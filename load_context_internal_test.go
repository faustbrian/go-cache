package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type contextValueKey string

func newContextLoadingCache(t *testing.T, ttl TTLPolicy, clock Clock, policy LoadPolicy) *Cache[string, string] {
	t.Helper()
	keys, err := NewKeySpace("test", "context", 1, StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(Config[string, string]{
		Backend:  &internalMemoryBackend{},
		Keys:     keys,
		Codec:    JSONCodec[string]{Version: 1},
		TTL:      ttl,
		Clock:    clock,
		MaxValue: 1024,
		Load:     policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

type observedLoadContext struct {
	leader, joiner any
	hasDeadline    bool
	errAfterCancel error
}

func TestLoadContextInheritsOnlyAdmittingCallerValues(t *testing.T) {
	t.Parallel()

	for name, inherit := range map[string]bool{"inherit": true, "default": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newContextLoadingCache(t, TTLPolicy{TTL: time.Minute}, SystemClock{}, LoadPolicy{InheritValues: inherit})
			key, err := store.keys.Key("key")
			if err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{})
			release := make(chan struct{})
			observed := make(chan observedLoadContext, 1)
			loader := func(ctx context.Context, _ string) (LoadResult[string], error) {
				close(started)
				<-release
				_, hasDeadline := ctx.Deadline()
				observed <- observedLoadContext{
					leader:         ctx.Value(contextValueKey("leader")),
					joiner:         ctx.Value(contextValueKey("joiner")),
					hasDeadline:    hasDeadline,
					errAfterCancel: ctx.Err(),
				}
				return LoadResult[string]{Value: "loaded", Found: true}, nil
			}

			leaderCtx, cancelLeader := context.WithTimeout(
				context.WithValue(t.Context(), contextValueKey("leader"), "leader-value"), time.Minute)
			leaderDone := make(chan error, 1)
			go func() {
				_, err := store.GetOrLoad(leaderCtx, "key", loader)
				leaderDone <- err
			}()
			waitInternalSignal(t, started)

			joinerCtx := context.WithValue(t.Context(), contextValueKey("joiner"), "joiner-value")
			joinerDone := make(chan Result[string], 1)
			go func() {
				result, err := store.GetOrLoad(joinerCtx, "key", func(context.Context, string) (LoadResult[string], error) {
					t.Error("joiner loader ran instead of joining the flight")
					return LoadResult[string]{}, nil
				})
				if err != nil {
					t.Errorf("joiner GetOrLoad() error = %v", err)
				}
				joinerDone <- result
			}()
			waitForInternalWaiters(t, store, key, 2)

			cancelLeader()
			if err := <-leaderDone; !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled leader returned %v, want context.Canceled", err)
			}
			close(release)
			got := <-observed
			if got.errAfterCancel != nil || got.hasDeadline {
				t.Fatalf("leader cancellation or deadline reached the load: err=%v deadline=%v", got.errAfterCancel, got.hasDeadline)
			}
			wantLeader := any(nil)
			if inherit {
				wantLeader = "leader-value"
			}
			if got.leader != wantLeader || got.joiner != nil {
				t.Fatalf("loader values leader=%v joiner=%v, want leader=%v joiner=<nil>", got.leader, got.joiner, wantLeader)
			}
			if result := <-joinerDone; result.State != Hit || result.Value != "loaded" {
				t.Fatalf("joiner result = %+v, want the leader's load", result)
			}
		})
	}
}

func TestLoadContextStaleRefreshInheritsReaderValues(t *testing.T) {
	t.Parallel()

	clock := &internalMutableClock{now: time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)}
	store := newContextLoadingCache(t, TTLPolicy{TTL: time.Minute, StaleFor: time.Hour}, clock,
		LoadPolicy{StaleWhileRevalidate: true, InheritValues: true})
	if err := store.Set(t.Context(), "key", "stale"); err != nil {
		t.Fatal(err)
	}
	clock.advance(2 * time.Minute)

	seen := make(chan any, 1)
	reader := context.WithValue(t.Context(), contextValueKey("leader"), "stale-reader")
	result, err := store.GetOrLoad(reader, "key", func(ctx context.Context, _ string) (LoadResult[string], error) {
		seen <- ctx.Value(contextValueKey("leader"))
		return LoadResult[string]{Value: "fresh", Found: true}, nil
	})
	if err != nil || result.State != Stale {
		t.Fatalf("stale read = %+v, %v", result, err)
	}
	select {
	case value := <-seen:
		if value != "stale-reader" {
			t.Fatalf("background refresh value = %v, want the stale reader's value", value)
		}
	case <-time.After(time.Second):
		t.Fatal("background refresh did not run")
	}
}

func TestLoadContextTimeoutBoundsAndClassifies(t *testing.T) {
	t.Parallel()

	errCarrier := errors.New("carrier unavailable")
	store := newContextLoadingCache(t, TTLPolicy{TTL: time.Minute}, SystemClock{},
		LoadPolicy{Timeout: 20 * time.Millisecond, InheritValues: true})
	caller, cancelCaller := context.WithTimeout(t.Context(), time.Minute)
	defer cancelCaller()
	callerDeadline, _ := caller.Deadline()

	var loaderDeadline time.Time
	_, err := store.GetOrLoad(caller, "slow", func(ctx context.Context, _ string) (LoadResult[string], error) {
		loaderDeadline, _ = ctx.Deadline()
		<-ctx.Done()
		return LoadResult[string]{}, errors.Join(errCarrier, ctx.Err())
	})
	for _, target := range []error{ErrLoader, ErrLoadTimeout, errCarrier, context.DeadlineExceeded} {
		if !errors.Is(err, target) {
			t.Fatalf("timed-out load error = %v, want match for %v", err, target)
		}
	}
	var classified *Error
	if !errors.As(err, &classified) || classified.Kind != LoaderError || classified.Operation != OperationLoad {
		t.Fatalf("timed-out load error = %#v, want a LoaderError load failure", err)
	}
	if loaderDeadline.IsZero() || !loaderDeadline.Before(callerDeadline) {
		t.Fatalf("loader deadline = %v, want the policy timeout, not caller deadline %v", loaderDeadline, callerDeadline)
	}

	_, err = store.GetOrLoad(t.Context(), "fast-failure", func(context.Context, string) (LoadResult[string], error) {
		return LoadResult[string]{}, errCarrier
	})
	if !errors.Is(err, ErrLoader) || !errors.Is(err, errCarrier) || errors.Is(err, ErrLoadTimeout) {
		t.Fatalf("failure before the timeout = %v, want a plain loader error", err)
	}

	result, err := store.GetOrLoad(t.Context(), "fast", func(ctx context.Context, _ string) (LoadResult[string], error) {
		return LoadResult[string]{Value: "loaded", Found: true}, ctx.Err()
	})
	if err != nil || result.State != Hit {
		t.Fatalf("load within the timeout = %+v, %v", result, err)
	}
}

func TestLoadContextShutdownIsNotATimeout(t *testing.T) {
	t.Parallel()

	store := newContextLoadingCache(t, TTLPolicy{TTL: time.Minute}, SystemClock{}, LoadPolicy{Timeout: time.Minute})
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "key", func(ctx context.Context, _ string) (LoadResult[string], error) {
			close(started)
			<-ctx.Done()
			return LoadResult[string]{}, ctx.Err()
		})
		done <- err
	}()
	waitInternalSignal(t, started)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) || errors.Is(err, ErrLoadTimeout) {
		t.Fatalf("load canceled by Close returned %v, want cancellation without ErrLoadTimeout", err)
	}
}

func TestLoadContextKeepsRecursionDetection(t *testing.T) {
	t.Parallel()

	store := newContextLoadingCache(t, TTLPolicy{TTL: time.Minute}, SystemClock{},
		LoadPolicy{Timeout: time.Second, InheritValues: true})
	other := newContextLoadingCache(t, TTLPolicy{TTL: time.Minute}, SystemClock{}, LoadPolicy{InheritValues: true})
	_, err := other.GetOrLoad(t.Context(), "outer", func(otherCtx context.Context, _ string) (LoadResult[string], error) {
		// otherCtx carries the other cache's marker into store's inherited values.
		_, err := store.GetOrLoad(otherCtx, "middle", func(ctx context.Context, _ string) (LoadResult[string], error) {
			_, err := store.GetOrLoad(ctx, "inner", func(context.Context, string) (LoadResult[string], error) {
				return LoadResult[string]{Value: "inner", Found: true}, nil
			})
			return LoadResult[string]{}, err
		})
		return LoadResult[string]{}, err
	})
	if !errors.Is(err, ErrRecursiveLoad) {
		t.Fatalf("recursive load error = %v, want ErrRecursiveLoad", err)
	}
}

func TestLoadContextRejectsNegativeTimeout(t *testing.T) {
	t.Parallel()

	keys, err := NewKeySpace("test", "context", 1, StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config[string, string]{
		Backend: &internalMemoryBackend{}, Keys: keys, Codec: JSONCodec[string]{Version: 1},
		TTL: TTLPolicy{TTL: time.Minute}, Clock: SystemClock{}, MaxValue: 1024,
		Load: LoadPolicy{Timeout: -time.Second},
	})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("New() with negative Timeout = %v, want ErrInvalidPolicy", err)
	}
}

type internalMutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *internalMutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *internalMutableClock) advance(step time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(step)
}

func TestLoadContextOwnDeadlineIsNotATimeout(t *testing.T) {
	t.Parallel()

	store := newContextLoadingCache(t, TTLPolicy{TTL: time.Minute}, SystemClock{}, LoadPolicy{Timeout: time.Minute})
	_, err := store.GetOrLoad(t.Context(), "key", func(ctx context.Context, _ string) (LoadResult[string], error) {
		own, cancel := context.WithTimeout(ctx, time.Millisecond)
		defer cancel()
		<-own.Done()
		return LoadResult[string]{}, own.Err()
	})
	if !errors.Is(err, ErrLoader) || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrLoadTimeout) {
		t.Fatalf("loader's own deadline = %v, want a loader error without ErrLoadTimeout", err)
	}
}

func TestLoadContextPublishesLateSuccess(t *testing.T) {
	t.Parallel()

	store := newContextLoadingCache(t, TTLPolicy{TTL: time.Minute}, SystemClock{}, LoadPolicy{Timeout: 5 * time.Millisecond})
	result, err := store.GetOrLoad(t.Context(), "key", func(ctx context.Context, _ string) (LoadResult[string], error) {
		<-ctx.Done()
		return LoadResult[string]{Value: "late", Found: true}, nil
	})
	if err != nil || result.State != Hit || result.Value != "late" {
		t.Fatalf("late success = %+v, %v, want a published hit", result, err)
	}
	stored, err := store.Get(t.Context(), "key")
	if err != nil || stored.State != Hit || stored.Value != "late" {
		t.Fatalf("stored late success = %+v, %v", stored, err)
	}
}
