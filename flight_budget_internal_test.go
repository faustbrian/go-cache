package cache

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDistinctKeyFlightsRemainBoundedAcrossCanceledAndStaleCallers(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprintf("stale=%t", stale), func(t *testing.T) {
			backend := &internalMemoryBackend{}
			store := newInternalLoadingCache(t, backend)
			store.load.StaleWhileRevalidate = stale
			const desiredFlightBudget = 8
			store.load.MaxFlights = desiredFlightBudget
			release := make(chan struct{})
			loader := func(ctx context.Context, _ string) (LoadResult[string], error) {
				select {
				case <-ctx.Done():
					return LoadResult[string]{}, ctx.Err()
				case <-release:
					return LoadResult[string]{Value: "loaded", Found: true}, nil
				}
			}
			for i := range desiredFlightBudget + 1 {
				logical := fmt.Sprintf("hostile-%d", i)
				key, err := store.keys.Key(logical)
				if err != nil {
					t.Fatal(err)
				}
				if stale {
					payload, err := store.codec.Encode("stale")
					if err != nil {
						t.Fatal(err)
					}
					_, err = backend.Set(t.Context(), key, Record{Payload: payload,
						ExpiresAt: time.Now().Add(-time.Second), StaleAt: time.Now().Add(time.Minute)}, Unconditional)
					if err != nil {
						t.Fatal(err)
					}
					result, err := store.GetOrLoad(t.Context(), logical, loader)
					if i == desiredFlightBudget {
						if !errors.Is(err, ErrFlightLimit) || result.State != Stale || result.Value != "stale" {
							t.Fatalf("excess stale refresh: result=%+v err=%v", result, err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
				} else {
					ctx, cancel := context.WithCancel(t.Context())
					done := make(chan error, 1)
					go func() { _, err := store.GetOrLoad(ctx, logical, loader); done <- err }()
					if i == desiredFlightBudget {
						cancel()
						// Cancellation must not be allowed to mask a synchronous
						// excess-work rejection; use a live context for the proof.
						<-done
						if _, err := store.GetOrLoad(t.Context(), logical, loader); !errors.Is(err, ErrFlightLimit) {
							t.Fatalf("excess foreground flight: %v", err)
						}
						continue
					}
					waitForInternalWaiters(t, store, key, 1)
					cancel()
					if err := <-done; !errors.Is(err, context.Canceled) {
						t.Fatalf("canceled request: %v", err)
					}
				}
			}
			store.loadMu.Lock()
			retained, active := len(store.flights), store.activeLoads
			store.loadMu.Unlock()
			if retained > desiredFlightBudget || active > desiredFlightBudget {
				t.Fatalf("distinct-key work exceeds budget %d: retained=%d active=%d (MaxConcurrent=%d)",
					desiredFlightBudget, retained, active, store.load.MaxConcurrent)
			}
			// A follower can still join a retained key when distinct-key
			// capacity is exhausted, then detach without freeing live work.
			store.load.StaleWhileRevalidate = false
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { _, err := store.GetOrLoad(ctx, "hostile-0", loader); done <- err }()
			key, err := store.keys.Key("hostile-0")
			if err != nil {
				t.Fatal(err)
			}
			waitForInternalWaiters(t, store, key, 1)
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("existing-key follower: %v", err)
			}
			close(release)
			deadline := time.Now().Add(time.Second)
			for {
				store.loadMu.Lock()
				finished := len(store.flights) == 0 && store.activeLoads == 0
				store.loadMu.Unlock()
				if finished {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("finished work retained flight budget")
				}
				time.Sleep(time.Millisecond)
			}
			if result, err := store.GetOrLoad(t.Context(), "replacement", loader); err != nil || result.Value != "loaded" {
				t.Fatalf("released flight budget: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestFlightBudgetConfigurationBounds(t *testing.T) {
	base := newInternalLoadingCache(t, &internalMemoryBackend{})
	for _, policy := range []LoadPolicy{
		{MaxFlights: -1}, {MaxFlights: MaxLoadFlights + 1},
		{MaxConcurrent: MaxLoadFlights + 1}, {MaxConcurrent: 2, MaxFlights: 1},
	} {
		_, err := New(Config[string, string]{Backend: base.backend, Keys: base.keys, Codec: base.codec,
			TTL: base.ttl, Clock: base.clock, MaxValue: base.maxValue, Load: policy})
		if !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("policy %+v: %v", policy, err)
		}
	}
	for _, policy := range []LoadPolicy{{}, {MaxConcurrent: 1, MaxFlights: 1}, {MaxConcurrent: MaxLoadFlights, MaxFlights: MaxLoadFlights}} {
		store, err := New(Config[string, string]{Backend: base.backend, Keys: base.keys, Codec: base.codec,
			TTL: base.ttl, Clock: base.clock, MaxValue: base.maxValue, Load: policy})
		if err != nil {
			t.Fatalf("policy %+v: %v", policy, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
