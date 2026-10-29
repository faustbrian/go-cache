package cache_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cache "github.com/faustbrian/go-cache/v4"
	cachememory "github.com/faustbrian/go-cache/v4/adapters/memory"
)

var neverDeadline = time.Unix(0, math.MaxInt64)

type atomicClock struct{ nanos atomic.Int64 }

func newAtomicClock(now time.Time) *atomicClock {
	clock := &atomicClock{}
	clock.nanos.Store(now.UnixNano())
	return clock
}

func (c *atomicClock) Now() time.Time             { return time.Unix(0, c.nanos.Load()) }
func (c *atomicClock) advance(step time.Duration) { c.nanos.Add(int64(step)) }

type countingLoader struct {
	calls  atomic.Int64
	expiry cache.Expiry
	found  bool
}

func (l *countingLoader) load(_ context.Context, key string) (cache.LoadResult[string], error) {
	l.calls.Add(1)
	return cache.LoadResult[string]{Value: "loaded:" + key, Found: l.found, Expiry: l.expiry}, nil
}

func newExpiryCache(
	t *testing.T,
	backend cache.Backend,
	clock cache.Clock,
	ttl cache.TTLPolicy,
	policy cache.LoadPolicy,
	jitter cache.JitterSource,
) *cache.Cache[string, string] {
	t.Helper()
	store, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     expiryKeySpace(t),
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      ttl,
		Clock:    clock,
		MaxValue: 1024,
		Load:     policy,
		Jitter:   jitter,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close cache: %v", err)
		}
	})
	return store
}

func expiryKeySpace(t *testing.T) cache.KeySpace[string] {
	t.Helper()
	space, err := cache.NewKeySpace("test", "strings", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	return space
}

func storedRecord(t *testing.T, backend *recordingBackend, logical string) (cache.Record, bool) {
	t.Helper()
	key, err := expiryKeySpace(t).Key(logical)
	if err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	record, found := backend.records[key]
	return record.Clone(), found
}

func requireState(t *testing.T, store *cache.Cache[string, string], logical string, want cache.State, negative bool) {
	t.Helper()
	result, err := store.Get(context.Background(), logical)
	if err != nil {
		t.Fatalf("get %q: %v", logical, err)
	}
	if result.State != want || result.Negative != negative {
		t.Fatalf("get %q = state %v negative %v, want state %v negative %v", logical, result.State, result.Negative, want, negative)
	}
}

func TestLoaderAbsoluteExpiryOverridesPolicyTTL(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	clock := newAtomicClock(start)
	backend := newRecordingBackend()
	deadline := start.Add(90 * time.Minute)
	loader := &countingLoader{found: true, expiry: cache.ExpireAt(deadline)}
	store := newExpiryCache(t, backend, clock, cache.TTLPolicy{TTL: time.Minute}, cache.LoadPolicy{}, nil)

	if _, err := store.GetOrLoad(context.Background(), "route", loader.load); err != nil {
		t.Fatal(err)
	}
	record, found := storedRecord(t, backend, "route")
	if !found || !record.ExpiresAt.Equal(deadline) || !record.StaleAt.Equal(deadline) {
		t.Fatalf("record = %+v found %v, want both deadlines at %v", record, found, deadline)
	}

	clock.advance(89*time.Minute + 59*time.Second)
	result, err := store.GetOrLoad(context.Background(), "route", loader.load)
	if err != nil || result.State != cache.Hit {
		t.Fatalf("before deadline = %+v, %v; want hit past the one-minute policy TTL", result, err)
	}
	if calls := loader.calls.Load(); calls != 1 {
		t.Fatalf("loader calls before deadline = %d, want 1", calls)
	}

	clock.advance(time.Second)
	requireState(t, store, "route", cache.Miss, false)
	if _, err := store.GetOrLoad(context.Background(), "route", loader.load); err != nil {
		t.Fatal(err)
	}
	if calls := loader.calls.Load(); calls != 2 {
		t.Fatalf("loader calls at deadline = %d, want reload", calls)
	}
}

func TestNeverExpireStaysFreshWithoutStaleWindow(t *testing.T) {
	t.Parallel()

	clock := newAtomicClock(time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC))
	backend := newRecordingBackend()
	loader := &countingLoader{found: true, expiry: cache.NeverExpire()}
	store := newExpiryCache(t, backend, clock,
		cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Hour}, cache.LoadPolicy{RefreshJitter: 30 * time.Second},
		fixedJitter{duration: 30 * time.Second})

	if _, err := store.GetOrLoad(context.Background(), "sender", loader.load); err != nil {
		t.Fatal(err)
	}
	record, _ := storedRecord(t, backend, "sender")
	if !record.ExpiresAt.Equal(neverDeadline) || !record.StaleAt.Equal(neverDeadline) {
		t.Fatalf("never record deadlines = %v/%v, want both %v", record.ExpiresAt, record.StaleAt, neverDeadline)
	}
	clock.advance(100 * 365 * 24 * time.Hour)
	requireState(t, store, "sender", cache.Hit, false)
	if calls := loader.calls.Load(); calls != 1 {
		t.Fatalf("loader calls = %d, want 1", calls)
	}

	if err := store.SetWithExpiry(context.Background(), "set", "value", cache.NeverExpire()); err != nil {
		t.Fatal(err)
	}
	record, _ = storedRecord(t, backend, "set")
	if !record.ExpiresAt.Equal(neverDeadline) || !record.StaleAt.Equal(neverDeadline) {
		t.Fatalf("SetWithExpiry never deadlines = %v/%v", record.ExpiresAt, record.StaleAt)
	}
}

func TestMixedEntryExpiriesShareOneCache(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	clock := newAtomicClock(start)
	store := newExpiryCache(t, newRecordingBackend(), clock, cache.TTLPolicy{TTL: time.Minute}, cache.LoadPolicy{}, nil)
	ctx := context.Background()
	if err := store.Set(ctx, "policy", "p"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWithExpiry(ctx, "absolute", "a", cache.ExpireAt(start.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWithExpiry(ctx, "never", "n", cache.NeverExpire()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWithExpiry(ctx, "zero", "z", cache.Expiry{}); err != nil {
		t.Fatal(err)
	}

	clock.advance(2 * time.Minute)
	requireState(t, store, "policy", cache.Miss, false)
	requireState(t, store, "zero", cache.Miss, false)
	requireState(t, store, "absolute", cache.Hit, false)
	requireState(t, store, "never", cache.Hit, false)

	clock.advance(time.Hour)
	requireState(t, store, "absolute", cache.Miss, false)
	requireState(t, store, "never", cache.Hit, false)
}

func TestNegativeLoadWithExpiryIsStoredWithoutNegativeTTL(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	clock := newAtomicClock(start)
	backend := newRecordingBackend()
	deadline := start.Add(14 * time.Hour)
	loader := &countingLoader{found: false, expiry: cache.ExpireAt(deadline)}
	store := newExpiryCache(t, backend, clock, cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Hour}, cache.LoadPolicy{}, nil)

	result, err := store.GetOrLoad(context.Background(), "absent", loader.load)
	if err != nil || result.State != cache.Miss || !result.Negative {
		t.Fatalf("negative load = %+v, %v", result, err)
	}
	record, found := storedRecord(t, backend, "absent")
	if !found || !record.Negative || !record.ExpiresAt.Equal(deadline) || !record.StaleAt.Equal(deadline) {
		t.Fatalf("negative record = %+v found %v; StaleFor must not extend negative entries", record, found)
	}
	clock.advance(13 * time.Hour)
	if result, err := store.GetOrLoad(context.Background(), "absent", loader.load); err != nil || !result.Negative {
		t.Fatalf("cached negative = %+v, %v", result, err)
	}
	if calls := loader.calls.Load(); calls != 1 {
		t.Fatalf("loader calls while negative is cached = %d, want 1", calls)
	}
	clock.advance(time.Hour)
	if _, err := store.GetOrLoad(context.Background(), "absent", loader.load); err != nil {
		t.Fatal(err)
	}
	if calls := loader.calls.Load(); calls != 2 {
		t.Fatalf("loader calls after negative deadline = %d, want 2", calls)
	}

	never := &countingLoader{found: false, expiry: cache.NeverExpire()}
	if _, err := store.GetOrLoad(context.Background(), "never-absent", never.load); err != nil {
		t.Fatal(err)
	}
	if record, _ := storedRecord(t, backend, "never-absent"); !record.Negative || !record.StaleAt.Equal(neverDeadline) {
		t.Fatalf("never negative record = %+v", record)
	}
}

func TestPastExpiryDeletesInsteadOfStoring(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	clock := newAtomicClock(start)
	backend := newRecordingBackend()
	store := newExpiryCache(t, backend, clock,
		cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Hour}, cache.LoadPolicy{StaleIfError: true}, nil)
	ctx := context.Background()

	if err := store.Set(ctx, "set", "old"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWithExpiry(ctx, "set", "new", cache.ExpireAt(start)); err != nil {
		t.Fatalf("expired write: %v", err)
	}
	if _, found := storedRecord(t, backend, "set"); found {
		t.Fatal("expired SetWithExpiry kept the previous value")
	}

	for name, found := range map[string]bool{"found": true, "absent": false} {
		logical := "load-" + name
		if err := store.Set(ctx, logical, "stale"); err != nil {
			t.Fatal(err)
		}
		clock.advance(2 * time.Minute)
		now := clock.Now()
		loader := &countingLoader{found: found, expiry: cache.ExpireAt(now.Add(-time.Nanosecond))}
		result, err := store.GetOrLoad(ctx, logical, loader.load)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if found && (result.State != cache.Hit || result.Value != "loaded:"+logical) {
			t.Fatalf("%s: waiters must still receive the loaded value, got %+v", name, result)
		}
		if !found && !result.Negative {
			t.Fatalf("%s: waiters must still receive the negative result, got %+v", name, result)
		}
		if _, stored := storedRecord(t, backend, logical); stored {
			t.Fatalf("%s: passed loader expiry stored or kept a stale record", name)
		}
	}
}

func TestInvalidExpiriesAreRejected(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()
	cases := map[string]struct {
		ttl    cache.TTLPolicy
		expiry cache.Expiry
		want   error
		// negativeValid marks deadlines that only fail through StaleFor, which
		// negative entries do not use.
		negativeValid bool
	}{
		"zero deadline":          {ttl: cache.TTLPolicy{TTL: time.Minute}, expiry: cache.ExpireAt(time.Time{}), want: cache.ErrInvalidTTL},
		"unportable deadline":    {ttl: cache.TTLPolicy{TTL: time.Minute}, expiry: cache.ExpireAt(time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)), want: cache.ErrInvalidTTL},
		"stale window overflows": {ttl: cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Hour}, expiry: cache.ExpireAt(neverDeadline.Add(-time.Minute)), want: cache.ErrInvalidTTL, negativeValid: true},
		"absolute with sliding":  {ttl: cache.TTLPolicy{TTL: time.Minute, Sliding: true}, expiry: cache.ExpireAt(start.Add(time.Hour)), want: cache.ErrInvalidPolicy},
		"never with sliding":     {ttl: cache.TTLPolicy{TTL: time.Minute, Sliding: true}, expiry: cache.NeverExpire(), want: cache.ErrInvalidPolicy},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			backend := newRecordingBackend()
			store := newExpiryCache(t, backend, fixedClock{now: start}, testCase.ttl, cache.LoadPolicy{}, nil)
			err := store.SetWithExpiry(ctx, "set", "value", testCase.expiry)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("SetWithExpiry error = %v, want %v", err, testCase.want)
			}
			loader := &countingLoader{found: true, expiry: testCase.expiry}
			if _, err := store.GetOrLoad(ctx, "load-found", loader.load); !errors.Is(err, testCase.want) {
				t.Fatalf("found loader error = %v, want %v", err, testCase.want)
			}
			absent := &countingLoader{found: false, expiry: testCase.expiry}
			_, err = store.GetOrLoad(ctx, "load-absent", absent.load)
			if testCase.negativeValid {
				if err != nil {
					t.Fatalf("absent loader error = %v; negative entries ignore StaleFor", err)
				}
				return
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("absent loader error = %v, want %v", err, testCase.want)
			}
			backend.mu.Lock()
			defer backend.mu.Unlock()
			if len(backend.records) != 0 {
				t.Fatalf("invalid expiry stored %d records", len(backend.records))
			}
		})
	}

	sliding := newExpiryCache(t, newRecordingBackend(), fixedClock{now: start},
		cache.TTLPolicy{TTL: time.Minute, Sliding: true}, cache.LoadPolicy{}, nil)
	if err := sliding.SetWithExpiry(ctx, "zero", "value", cache.Expiry{}); err != nil {
		t.Fatalf("zero expiry must keep the sliding policy: %v", err)
	}
}

func TestAbsoluteExpiryKeepsStaleWindowAndSkipsJitter(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	clock := newAtomicClock(start)
	backend := newRecordingBackend()
	deadline := start.Add(10 * time.Minute)
	loader := &countingLoader{found: true, expiry: cache.ExpireAt(deadline)}
	store := newExpiryCache(t, backend, clock,
		cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Hour},
		cache.LoadPolicy{StaleWhileRevalidate: true, RefreshJitter: 30 * time.Second},
		fixedJitter{duration: 30 * time.Second})

	if _, err := store.GetOrLoad(context.Background(), "key", loader.load); err != nil {
		t.Fatal(err)
	}
	record, _ := storedRecord(t, backend, "key")
	if !record.ExpiresAt.Equal(deadline) || !record.StaleAt.Equal(deadline.Add(time.Hour)) {
		t.Fatalf("deadlines = %v/%v, want unjittered %v and stale %v", record.ExpiresAt, record.StaleAt, deadline, deadline.Add(time.Hour))
	}

	clock.advance(11 * time.Minute)
	refreshed := start.Add(2 * time.Hour)
	refresh := &countingLoader{found: true, expiry: cache.ExpireAt(refreshed)}
	backend.notifyAfterSets(2)
	result, err := store.GetOrLoad(context.Background(), "key", refresh.load)
	if err != nil || result.State != cache.Stale {
		t.Fatalf("stale-while-revalidate read = %+v, %v", result, err)
	}
	waitForSignal(t, backend.setsReached)
	if record, _ := storedRecord(t, backend, "key"); !record.ExpiresAt.Equal(refreshed) {
		t.Fatalf("background refresh deadline = %v, want %v", record.ExpiresAt, refreshed)
	}
}

func TestFixedTTLCachesKeepPolicyDeadlines(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	backend := newRecordingBackend()
	store := newExpiryCache(t, backend, fixedClock{now: start},
		cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Hour},
		cache.LoadPolicy{RefreshJitter: 10 * time.Second}, fixedJitter{duration: 10 * time.Second})
	ctx := context.Background()

	if err := store.Set(ctx, "set", "v"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWithExpiry(ctx, "zero", "v", cache.Expiry{}); err != nil {
		t.Fatal(err)
	}
	found := &countingLoader{found: true}
	if _, err := store.GetOrLoad(ctx, "loaded", found.load); err != nil {
		t.Fatal(err)
	}
	absent := &countingLoader{found: false}
	if _, err := store.GetOrLoad(ctx, "absent", absent.load); err != nil {
		t.Fatal(err)
	}

	for logical, wantFresh := range map[string]time.Duration{"set": time.Minute, "zero": time.Minute, "loaded": 50 * time.Second} {
		record, stored := storedRecord(t, backend, logical)
		if !stored || !record.ExpiresAt.Equal(start.Add(wantFresh)) || !record.StaleAt.Equal(start.Add(wantFresh+time.Hour)) {
			t.Fatalf("%s record = %+v stored %v, want policy deadlines", logical, record, stored)
		}
	}
	if _, stored := storedRecord(t, backend, "absent"); stored {
		t.Fatal("negative result without expiry was stored with NegativeTTL zero")
	}
}

func TestMemoryBoundEvictsLeastRecentlyUsedRegardlessOfExpiry(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	clock := newAtomicClock(start)
	backend, err := cachememory.New(cachememory.Config{MaxEntries: 3, MaxBytes: 1 << 20, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	store := newExpiryCache(t, backend, clock, cache.TTLPolicy{TTL: time.Minute}, cache.LoadPolicy{}, nil)
	ctx := context.Background()
	writes := []struct {
		key    string
		expiry cache.Expiry
	}{
		{key: "never", expiry: cache.NeverExpire()},
		{key: "absolute", expiry: cache.ExpireAt(start.Add(time.Hour))},
		{key: "policy"},
	}
	for _, write := range writes {
		if err := store.SetWithExpiry(ctx, write.key, "v", write.expiry); err != nil {
			t.Fatal(err)
		}
	}

	// A read promotes "never", so the bound evicts "absolute": order is by
	// recency, not insertion or remaining lifetime.
	requireState(t, store, "never", cache.Hit, false)
	if err := store.SetWithExpiry(ctx, "fourth", "v", cache.NeverExpire()); err != nil {
		t.Fatal(err)
	}
	requireState(t, store, "absolute", cache.Miss, false)
	requireState(t, store, "never", cache.Hit, false)
	requireState(t, store, "policy", cache.Hit, false)
	requireState(t, store, "fourth", cache.Hit, false)

	// The checks above read "never" first, so it is now least recently used and
	// is evicted although it never expires.
	if err := store.SetWithExpiry(ctx, "fifth", "v", cache.ExpireAt(start.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	requireState(t, store, "never", cache.Miss, false)
	if stats := backend.Stats(); stats.Entries != 3 || stats.Evictions != 2 {
		t.Fatalf("stats = %+v, want 3 entries and 2 evictions", stats)
	}
}

func TestExpiryRaceWrites(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	clock := newAtomicClock(start)
	backend, err := cachememory.New(cachememory.Config{MaxEntries: 16, MaxBytes: 1 << 20, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	store := newExpiryCache(t, backend, clock, cache.TTLPolicy{TTL: time.Minute}, cache.LoadPolicy{}, nil)
	expiries := []cache.Expiry{{}, cache.NeverExpire(), cache.ExpireAt(start.Add(time.Hour))}

	var group sync.WaitGroup
	errs := make(chan error, 64*4)
	for worker := range 64 {
		group.Go(func() {
			ctx := context.Background()
			logical := fmt.Sprint("key-", worker%8)
			expiry := expiries[worker%len(expiries)]
			loader := func(context.Context, string) (cache.LoadResult[string], error) {
				return cache.LoadResult[string]{Value: logical, Found: worker%5 != 0, Expiry: expiry}, nil
			}
			for range 4 {
				if _, err := store.GetOrLoad(ctx, logical, loader); err != nil {
					errs <- err
				}
				if err := store.SetWithExpiry(ctx, logical, logical, expiry); err != nil {
					errs <- err
				}
				clock.advance(time.Second)
			}
		})
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for key := range 8 {
		result, err := store.Get(context.Background(), fmt.Sprint("key-", key))
		if err != nil || result.State == cache.Hit && result.Value != fmt.Sprint("key-", key) {
			t.Fatalf("key-%d final = %+v, %v", key, result, err)
		}
	}
}

func TestExpiryRaceSupersede(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		expiry    cache.Expiry
		wantState cache.State
		wantStale time.Time
	}{
		"passed deadline deletes": {expiry: cache.ExpireAt(start), wantState: cache.Miss},
		"never is kept":           {expiry: cache.NeverExpire(), wantState: cache.Hit, wantStale: neverDeadline},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			backend := newRecordingBackend()
			store := newExpiryCache(t, backend, fixedClock{now: start}, cache.TTLPolicy{TTL: time.Minute}, cache.LoadPolicy{}, nil)
			started := make(chan struct{})
			release := make(chan struct{})
			loaded := make(chan cache.Result[string], 1)
			go func() {
				result, err := store.GetOrLoad(t.Context(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
					close(started)
					<-release
					return cache.LoadResult[string]{Value: "obsolete", Found: true}, nil
				})
				if err != nil {
					t.Errorf("GetOrLoad() error = %v", err)
				}
				loaded <- result
			}()
			waitForSignal(t, started)
			if err := store.SetWithExpiry(t.Context(), "key", "fresh", testCase.expiry); err != nil {
				t.Fatalf("SetWithExpiry() error = %v", err)
			}
			close(release)
			var result cache.Result[string]
			select {
			case result = <-loaded:
			case <-time.After(time.Second):
				t.Fatal("GetOrLoad() did not finish after the superseding write")
			}
			if result.State != testCase.wantState || testCase.wantState == cache.Hit && result.Value != "fresh" {
				t.Fatalf("superseded load result = %+v, want state %v with the explicit write", result, testCase.wantState)
			}
			record, stored := storedRecord(t, backend, "key")
			if stored != !testCase.wantStale.IsZero() || stored && (!record.StaleAt.Equal(testCase.wantStale) || string(record.Payload) == "") {
				t.Fatalf("stored record = %+v stored %v, want stale deadline %v", record, stored, testCase.wantStale)
			}
			requireState(t, store, "key", testCase.wantState, false)
		})
	}
}

func TestExpiredWriteObservesNoPayloadSize(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	observer := &expiryObserver{}
	store, err := cache.New(cache.Config[string, string]{
		Backend:  newRecordingBackend(),
		Keys:     expiryKeySpace(t),
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: start},
		MaxValue: 1024,
		Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetWithExpiry(t.Context(), "key", "value", cache.ExpireAt(start)); err != nil {
		t.Fatal(err)
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.events) != 1 || observer.events[0].Operation != cache.OperationSet || observer.events[0].Size != 0 {
		t.Fatalf("events = %+v, want one set event without payload size", observer.events)
	}
}

type expiryObserver struct {
	mu     sync.Mutex
	events []cache.Event
}

func (o *expiryObserver) Observe(_ context.Context, event cache.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
	return nil
}
