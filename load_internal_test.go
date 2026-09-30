package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaiterAccountingControlsAdmissionAndReleasesCapacity(t *testing.T) {
	t.Parallel()

	store := newInternalLoadingCache(t, &internalMemoryBackend{})
	key, err := store.keys.Key("key")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	loader := func(ctx context.Context, _ string) (LoadResult[string], error) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
			return LoadResult[string]{Value: "loaded", Found: true}, nil
		case <-ctx.Done():
			return LoadResult[string]{}, ctx.Err()
		}
	}

	leaderDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "key", loader)
		leaderDone <- err
	}()
	waitInternalSignal(t, started)

	followerCtx, cancelFollower := context.WithCancel(t.Context())
	followerDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(followerCtx, "key", loader)
		followerDone <- err
	}()
	waitForInternalWaiters(t, store, key, 2)

	if _, err := store.GetOrLoad(t.Context(), "key", loader); !errors.Is(err, ErrWaiterLimit) {
		t.Fatalf("overflow waiter returned %v, want ErrWaiterLimit", err)
	}

	cancelFollower()
	if err := <-followerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled follower returned %v", err)
	}
	waitForInternalWaiters(t, store, key, 1)

	replacementDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "key", loader)
		replacementDone <- err
	}()
	waitForInternalWaiters(t, store, key, 2)

	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader returned %v", err)
	}
	if err := <-replacementDone; err != nil {
		t.Fatalf("replacement returned %v", err)
	}
}

func TestRecursiveLoadOwnershipIsScopedToOneCache(t *testing.T) {
	t.Parallel()

	first := newInternalLoadingCache(t, &internalMemoryBackend{})
	second := newInternalLoadingCache(t, &internalMemoryBackend{})

	result, err := first.GetOrLoad(t.Context(), "outer", func(ctx context.Context, _ string) (LoadResult[string], error) {
		nested, err := second.GetOrLoad(ctx, "inner", func(context.Context, string) (LoadResult[string], error) {
			return LoadResult[string]{Value: "nested", Found: true}, nil
		})
		return LoadResult[string]{Value: nested.Value, Found: nested.State == Hit}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != Hit || result.Value != "nested" {
		t.Fatalf("cross-cache load returned %#v", result)
	}
}

func newInternalLoadingCache(t *testing.T, backend Backend) *Cache[string, string] {
	t.Helper()
	keys, err := NewKeySpace("test", "internal", 1, StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(Config[string, string]{
		Backend:  backend,
		Keys:     keys,
		Codec:    JSONCodec[string]{Version: 1},
		TTL:      TTLPolicy{TTL: time.Minute},
		Clock:    SystemClock{},
		MaxValue: 1024,
		Load: LoadPolicy{
			MaxConcurrent:    1,
			MaxWaitersPerKey: 2,
		},
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

func waitForInternalWaiters(
	t *testing.T,
	store *Cache[string, string],
	key string,
	want int,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		store.loadMu.Lock()
		flight := store.flights[key]
		got := 0
		if flight != nil {
			got = flight.waiters
		}
		store.loadMu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiters = %d, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitInternalSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for loader")
	}
}

type internalMemoryBackend struct {
	mu      sync.Mutex
	records map[string]Record
}

func (b *internalMemoryBackend) Get(ctx context.Context, key string) (Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record, found := b.records[key]
	return record.Clone(), found, nil
}

func (b *internalMemoryBackend) Set(
	ctx context.Context,
	key string,
	record Record,
	condition Condition,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.records == nil {
		b.records = make(map[string]Record)
	}
	_, found := b.records[key]
	if condition == IfAbsent && found || condition == IfPresent && !found {
		return false, nil
	}
	b.records[key] = record.Clone()
	return true, nil
}

func (b *internalMemoryBackend) Delete(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, found := b.records[key]
	delete(b.records, key)
	return found, nil
}

func TestShutdownDuringPublicationWaitPreventsLateWrite(t *testing.T) {
	tests := map[string]LoadResult[string]{
		"positive": {Value: "late", Found: true},
		"negative": {Found: false},
	}
	for name, loaded := range tests {
		t.Run(name, func(t *testing.T) {
			backend := &publicationBackend{}
			space, err := NewKeySpace("test", "publication", 1, StringKeyEncoder{}, 128)
			if err != nil {
				t.Fatal(err)
			}
			store, err := New(Config[string, string]{
				Backend: backend, Keys: space, Codec: JSONCodec[string]{Version: 1},
				TTL: TTLPolicy{TTL: time.Minute}, Clock: SystemClock{}, MaxValue: 1024,
				Load: LoadPolicy{NegativeTTL: time.Minute},
			})
			if err != nil {
				t.Fatal(err)
			}

			loaderReady := make(chan struct{})
			releaseLoader := make(chan struct{})
			loadDone := make(chan error, 1)
			go func() {
				_, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (LoadResult[string], error) {
					close(loaderReady)
					<-releaseLoader
					return loaded, nil
				})
				loadDone <- err
			}()
			<-loaderReady

			key, err := space.Key("key")
			if err != nil {
				t.Fatal(err)
			}
			store.loadMu.Lock()
			flight := store.flights[key]
			if flight == nil {
				store.loadMu.Unlock()
				t.Fatal("active flight not registered")
			}
			flight.mutation.Lock()
			store.loadMu.Unlock()

			publicationReached := make(chan struct{})
			publicationRelease := make(chan struct{})
			store.beforeLoadPublication = func() {
				close(publicationReached)
				<-publicationRelease
			}
			close(releaseLoader)
			<-publicationReached
			close(publicationRelease)

			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if err := store.Shutdown(ctx); !errors.Is(err, ErrShutdownIncomplete) {
				flight.mutation.Unlock()
				t.Fatalf("Shutdown() error = %v, want ErrShutdownIncomplete", err)
			}
			flight.mutation.Unlock()
			if err := <-loadDone; !errors.Is(err, context.Canceled) {
				t.Fatalf("GetOrLoad() error = %v, want context cancellation", err)
			}
			if backend.writes() != 0 {
				t.Fatalf("backend writes after shutdown = %d, want 0", backend.writes())
			}
		})
	}
}

type publicationBackend struct {
	mu       sync.Mutex
	setCount int
}

// These barriers pause trusted callbacks after runLoad's cancellation check,
// before the actual backend call, without adding another production seam.
type publicationCodec struct {
	JSONCodec[string]
	reached chan struct{}
	release chan struct{}
}

func (codec publicationCodec) Encode(value string) ([]byte, error) {
	close(codec.reached)
	<-codec.release
	return codec.JSONCodec.Encode(value)
}

type publicationClock struct {
	armed   atomic.Bool
	reached chan struct{}
	release chan struct{}
}

func (clock *publicationClock) Now() time.Time {
	if clock.armed.Swap(false) {
		close(clock.reached)
		<-clock.release
	}
	return time.Now()
}

func TestIncompleteShutdownPermitsAdmittedBackendPublication(t *testing.T) {
	for _, found := range []bool{true, false} {
		name := "negative"
		if found {
			name = "positive"
		}
		t.Run(name, func(t *testing.T) {
			backend := &publicationBackend{} // Deliberately ignores context.
			store := newInternalLoadingCache(t, backend)
			store.load.NegativeTTL = time.Minute
			reached, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			if found {
				store.codec = publicationCodec{JSONCodec: JSONCodec[string]{Version: 1}, reached: reached, release: release}
			} else {
				clock := &publicationClock{reached: reached, release: release}
				store.clock = clock
				store.beforeLoadPublication = func() { clock.armed.Store(true) }
			}
			done := make(chan error, 1)
			go func() {
				_, err := store.GetOrLoad(t.Context(), "key", func(context.Context, string) (LoadResult[string], error) {
					return LoadResult[string]{Value: "late", Found: found}, nil
				})
				done <- err
			}()
			waitInternalSignal(t, reached)
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
			defer cancel()
			if err := store.Shutdown(ctx); !errors.Is(err, ErrShutdownIncomplete) || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("blocked publication shutdown = %v, want incomplete deadline", err)
			}
			if backend.writes() != 0 {
				t.Fatal("backend wrote before callback release")
			}
			releaseOnce.Do(func() { close(release) })
			if err := <-done; err != nil {
				t.Fatalf("admitted publication = %v", err)
			}
			if backend.writes() != 1 {
				t.Fatalf("late writes = %d, want 1", backend.writes())
			}
			if err := store.Shutdown(t.Context()); err != nil {
				t.Fatalf("completed shutdown = %v", err)
			}
		})
	}
}

func (*publicationBackend) Get(context.Context, string) (Record, bool, error) {
	return Record{}, false, nil
}

func (backend *publicationBackend) Set(context.Context, string, Record, Condition) (bool, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.setCount++
	return true, nil
}

func (*publicationBackend) Delete(context.Context, string) (bool, error) { return false, nil }

func (backend *publicationBackend) writes() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.setCount
}

func TestFinishedFlightRemainsAttachedDuringExplicitMutation(t *testing.T) {
	backend := newSupersessionBackend()
	store := newInternalLoadingCache(t, backend)
	store.load.MaxFlights = 1

	firstLoadDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "key", func(context.Context, string) (LoadResult[string], error) {
			return LoadResult[string]{Value: "old", Found: true}, nil
		})
		firstLoadDone <- err
	}()
	waitInternalSignal(t, backend.firstSetReached)

	explicitDone := make(chan error, 1)
	go func() { explicitDone <- store.Set(t.Context(), "key", "explicit") }()
	waitForInternalMutationUsers(t, store, "key", 1)
	close(backend.releaseFirstSet)
	waitInternalSignal(t, backend.explicitSetReached)
	if err := <-firstLoadDone; err != nil {
		close(backend.releaseExplicitSet)
		t.Fatalf("first load returned %v", err)
	}
	if _, err := store.GetOrLoad(t.Context(), "other-key", func(context.Context, string) (LoadResult[string], error) {
		t.Error("mutation-reserved flight allowed a distinct loader")
		return LoadResult[string]{}, nil
	}); !errors.Is(err, ErrFlightLimit) {
		close(backend.releaseExplicitSet)
		t.Fatalf("mutation-reserved budget: %v", err)
	}

	var secondLoaderCalls int
	secondLoadDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "key", func(context.Context, string) (LoadResult[string], error) {
			secondLoaderCalls++
			return LoadResult[string]{Value: "late", Found: true}, nil
		})
		secondLoadDone <- err
	}()
	if err := <-secondLoadDone; err != nil {
		close(backend.releaseExplicitSet)
		t.Fatalf("second load returned %v", err)
	}
	if secondLoaderCalls != 0 {
		close(backend.releaseExplicitSet)
		t.Fatalf("new loader admitted while explicit mutation held finished flight: calls=%d", secondLoaderCalls)
	}
	if backend.count() != 2 {
		close(backend.releaseExplicitSet)
		t.Fatalf("writes before explicit mutation release = %d, want 2", backend.count())
	}

	close(backend.releaseExplicitSet)
	if err := <-explicitDone; err != nil {
		t.Fatalf("explicit Set returned %v", err)
	}
}

func waitForInternalMutationUsers(t *testing.T, store *Cache[string, string], logical string, want int) {
	t.Helper()
	key, err := store.keys.Key(logical)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		store.loadMu.Lock()
		flight := store.flights[key]
		got := 0
		if flight != nil {
			got = flight.mutationUsers
		}
		store.loadMu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("flight mutation users = %d, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

type supersessionBackend struct {
	mu                 sync.Mutex
	setCount           int
	firstSetReached    chan struct{}
	releaseFirstSet    chan struct{}
	explicitSetReached chan struct{}
	releaseExplicitSet chan struct{}
}

func newSupersessionBackend() *supersessionBackend {
	return &supersessionBackend{
		firstSetReached: make(chan struct{}), releaseFirstSet: make(chan struct{}),
		explicitSetReached: make(chan struct{}), releaseExplicitSet: make(chan struct{}),
	}
}

func (*supersessionBackend) Get(context.Context, string) (Record, bool, error) {
	return Record{}, false, nil
}

func (backend *supersessionBackend) Set(context.Context, string, Record, Condition) (bool, error) {
	backend.mu.Lock()
	backend.setCount++
	count := backend.setCount
	backend.mu.Unlock()
	switch count {
	case 1:
		close(backend.firstSetReached)
		<-backend.releaseFirstSet
	case 2:
		close(backend.explicitSetReached)
		<-backend.releaseExplicitSet
	}
	return true, nil
}

func (*supersessionBackend) Delete(context.Context, string) (bool, error) { return false, nil }

func (backend *supersessionBackend) count() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.setCount
}
