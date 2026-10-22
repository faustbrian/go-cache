package cache_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cache "github.com/faustbrian/go-cache/v4"
)

type r17PanicEncoder struct{ value any }

func (p r17PanicEncoder) EncodeKey(string) ([]byte, error) { panic(p.value) }

type r17PanicCodec struct {
	cache.JSONCodec[string]
	value  any
	encode bool
	decode bool
}

func (p r17PanicCodec) Encode(v string) ([]byte, error) {
	if p.encode {
		panic(p.value)
	}
	return p.JSONCodec.Encode(v)
}
func (p r17PanicCodec) Decode(b []byte) (string, error) {
	if p.decode {
		panic(p.value)
	}
	return p.JSONCodec.Decode(b)
}

type r17PanicBackend struct {
	cache.Backend
	value any
}

func (p r17PanicBackend) Get(context.Context, string) (cache.Record, bool, error) { panic(p.value) }

// These callbacks deliberately violate trusted-provider obligations. The
// caller's recovery observes foreground propagation; it is not a new cache
// recovery guarantee or proof for a panic in an owned loader goroutine.
func TestR17ForegroundTrustedCallbacksAreNotPanicBoundaries(t *testing.T) {
	for _, boundary := range []string{"key", "backend", "encode", "decode"} {
		t.Run(boundary, func(t *testing.T) {
			marker := &struct{ name string }{name: boundary}
			var encoder cache.KeyEncoder[string] = cache.StringKeyEncoder{}
			var backend cache.Backend = newRecordingBackend()
			var codec cache.Codec[string] = cache.JSONCodec[string]{Version: 1}
			if boundary == "key" {
				encoder = r17PanicEncoder{value: marker}
			}
			if boundary == "backend" {
				backend = r17PanicBackend{Backend: backend, value: marker}
			}
			if boundary == "encode" || boundary == "decode" {
				codec = r17PanicCodec{JSONCodec: cache.JSONCodec[string]{Version: 1}, value: marker, encode: boundary == "encode", decode: boundary == "decode"}
			}
			keys, err := cache.NewKeySpace("r17", "callbacks", 1, encoder, 128)
			if err != nil {
				t.Fatal(err)
			}
			store, err := cache.New(cache.Config[string, string]{Backend: backend, Keys: keys, Codec: codec, TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: time.Now()}, MaxValue: 1024})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			defer func() {
				if err := store.Shutdown(ctx); err != nil {
					t.Fatal("foreground probe left cache lifecycle incomplete")
				}
			}()
			if boundary == "decode" {
				if err := store.Set(ctx, "key", "value"); err != nil {
					t.Fatal(err)
				}
			}
			var observed any
			func() {
				defer func() { observed = recover() }()
				if boundary == "encode" {
					_ = store.Set(ctx, "key", "value")
				} else {
					_, _ = store.Get(ctx, "key")
				}
			}()
			if observed != marker {
				t.Fatal("trusted foreground callback panic did not reach caller with its original identity")
			}
		})
	}
}

func TestR17LoaderPanicIsClassifiedRedactedAndAllowsReuse(t *testing.T) {
	store := newLoadingStringCache(t, newRecordingBackend(), fixedClock{now: time.Now()}, cache.LoadPolicy{MaxConcurrent: 1, MaxFlights: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	defer func() {
		if err := store.Shutdown(ctx); err != nil {
			t.Fatal("recovered loader did not join")
		}
	}()
	marker := errors.New("r17-private-loader-marker")
	_, err := store.GetOrLoad(ctx, "key", func(context.Context, string) (cache.LoadResult[string], error) { panic(marker) })
	if !errors.Is(err, cache.ErrLoaderPanic) || !errors.Is(err, cache.ErrLoader) || strings.Contains(err.Error(), marker.Error()) || errors.Is(err, marker) {
		t.Fatal("loader panic classification or payload discard changed")
	}
	var exposed *cache.Error
	if !errors.As(err, &exposed) || exposed.Kind != cache.LoaderError {
		t.Fatal("loader failure lost its public typed category")
	}
	result, err := store.GetOrLoad(ctx, "key", func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{Value: "recovered", Found: true}, nil
	})
	if err != nil || result.State != cache.Hit || result.Value != "recovered" {
		t.Fatal("recovered panic poisoned same-key flight reuse")
	}
}

func TestR17ObserverPanicDoesNotReplacePublicOperationOutcome(t *testing.T) {
	keys := mustStringKeySpace(t)
	marker := &struct{}{}
	observed := &recordingObserver{}
	store, err := cache.New(cache.Config[string, string]{Backend: newRecordingBackend(), Keys: keys, Codec: cache.JSONCodec[string]{Version: 1}, TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: time.Now()}, MaxValue: 1024, Observer: observerFunc(func(ctx context.Context, event cache.Event) error {
		_ = observed.Observe(ctx, event)
		panic(marker)
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer func() {
		if err := store.Shutdown(ctx); err != nil {
			t.Fatal("observer probe did not close")
		}
	}()
	if err := store.Set(ctx, "key", "stored"); err != nil {
		t.Fatal("observer replaced successful storage outcome")
	}
	result, err := store.Get(ctx, "key")
	if err != nil || result.State != cache.Hit || result.Value != "stored" {
		t.Fatal("observer replaced successful read outcome")
	}
	events := observed.Events()
	if len(events) != 2 || events[0].Operation != cache.OperationSet || events[0].Outcome != cache.OutcomeSuccess || events[1].Operation != cache.OperationGet || events[1].Outcome != cache.OutcomeHit {
		t.Fatal("panic-producing observer did not receive the successful public outcomes")
	}
}
