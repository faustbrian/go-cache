//lint:file-ignore SA1019 Compatibility coverage requires deprecated imports.

package cache_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	cache "github.com/faustbrian/go-cache"
	cachememory "github.com/faustbrian/go-cache/adapters/memory"
	cacheotel "github.com/faustbrian/go-cache/adapters/otel"
	cacheredis "github.com/faustbrian/go-cache/adapters/redis"
	cacheservice "github.com/faustbrian/go-cache/adapters/service"
	cacheslog "github.com/faustbrian/go-cache/adapters/slog"
	cachevalkey "github.com/faustbrian/go-cache/adapters/valkey"
	legacymemory "github.com/faustbrian/go-cache/backend/memory"   //nolint:staticcheck // Compatibility coverage requires the deprecated path.
	legacyredis "github.com/faustbrian/go-cache/backend/redis"     //nolint:staticcheck // Compatibility coverage requires the deprecated path.
	legacyvalkey "github.com/faustbrian/go-cache/backend/valkey"   //nolint:staticcheck // Compatibility coverage requires the deprecated path.
	legacyservice "github.com/faustbrian/go-cache/cacheservice"    //nolint:staticcheck // Compatibility coverage requires the deprecated path.
	legacyotel "github.com/faustbrian/go-cache/observability/otel" //nolint:staticcheck // Compatibility coverage requires the deprecated path.
	legacyslog "github.com/faustbrian/go-cache/observability/slog" //nolint:staticcheck // Compatibility coverage requires the deprecated path.
	"go.opentelemetry.io/otel/metric/noop"
)

func TestCanonicalAdaptersPreserveLegacyContracts(t *testing.T) {
	t.Parallel()

	canonicalMemory, err := cachememory.New(cachememory.Config{
		MaxEntries: 1,
		MaxBytes:   128,
		Clock:      cache.SystemClock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyMemory, err := legacymemory.New(legacymemory.Config{
		MaxEntries: 1,
		MaxBytes:   128,
		Clock:      cache.SystemClock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := canonicalMemory.Close(); closeErr != nil {
			t.Errorf("canonical Close() error = %v", closeErr)
		}
	}()
	defer func() {
		if closeErr := legacyMemory.Close(); closeErr != nil {
			t.Errorf("legacy Close() error = %v", closeErr)
		}
	}()

	now := time.Now()
	record := cache.Record{
		Payload:   []byte("value"),
		ExpiresAt: now.Add(time.Minute),
		StaleAt:   now.Add(2 * time.Minute),
	}
	for _, backend := range []cache.Backend{canonicalMemory, legacyMemory} {
		if written, setErr := backend.Set(context.Background(), "key", record, cache.Unconditional); setErr != nil || !written {
			t.Fatalf("Set() = %v, %v", written, setErr)
		}
		got, found, getErr := backend.Get(context.Background(), "key")
		if getErr != nil || !found || string(got.Payload) != "value" {
			t.Fatalf("Get() = %#v, %v, %v", got, found, getErr)
		}
	}

	for _, identity := range []struct {
		name string
		got  reflect.Type
		want string
	}{
		{"legacy memory Config", reflect.TypeOf(legacymemory.Config{}), "github.com/faustbrian/go-cache/backend/memory"},
		{"legacy memory Stats", reflect.TypeOf(legacymemory.Stats{}), "github.com/faustbrian/go-cache/backend/memory"},
		{"legacy memory Backend", reflect.TypeOf(legacymemory.Backend{}), "github.com/faustbrian/go-cache/backend/memory"},
		{"canonical memory Config", reflect.TypeOf(cachememory.Config{}), "github.com/faustbrian/go-cache/adapters/memory"},
		{"canonical memory Stats", reflect.TypeOf(cachememory.Stats{}), "github.com/faustbrian/go-cache/adapters/memory"},
		{"canonical memory Backend", reflect.TypeOf(cachememory.Backend{}), "github.com/faustbrian/go-cache/adapters/memory"},
		{"legacy Redis Config", reflect.TypeOf(legacyredis.Config{}), "github.com/faustbrian/go-cache/backend/redis"},
		{"legacy Redis Backend", reflect.TypeOf(legacyredis.Backend{}), "github.com/faustbrian/go-cache/backend/redis"},
		{"canonical Redis Config", reflect.TypeOf(cacheredis.Config{}), "github.com/faustbrian/go-cache/adapters/redis"},
		{"canonical Redis Backend", reflect.TypeOf(cacheredis.Backend{}), "github.com/faustbrian/go-cache/adapters/redis"},
		{"legacy Valkey Config", reflect.TypeOf(legacyvalkey.Config{}), "github.com/faustbrian/go-cache/backend/valkey"},
		{"legacy Valkey Backend", reflect.TypeOf(legacyvalkey.Backend{}), "github.com/faustbrian/go-cache/backend/valkey"},
		{"canonical Valkey Config", reflect.TypeOf(cachevalkey.Config{}), "github.com/faustbrian/go-cache/adapters/valkey"},
		{"canonical Valkey Backend", reflect.TypeOf(cachevalkey.Backend{}), "github.com/faustbrian/go-cache/adapters/valkey"},
		{"legacy service Startup", reflect.TypeOf((legacyservice.Startup[int])(nil)), "github.com/faustbrian/go-cache/cacheservice"},
		{"legacy service Check", reflect.TypeOf((legacyservice.Check[int])(nil)), "github.com/faustbrian/go-cache/cacheservice"},
		{"legacy service Shutdown", reflect.TypeOf((legacyservice.Shutdown[int])(nil)), "github.com/faustbrian/go-cache/cacheservice"},
		{"legacy service Options", reflect.TypeOf(legacyservice.Options[int]{}), "github.com/faustbrian/go-cache/cacheservice"},
		{"legacy service OptionsError", reflect.TypeOf(legacyservice.OptionsError{}), "github.com/faustbrian/go-cache/cacheservice"},
		{"legacy service StartupError", reflect.TypeOf(legacyservice.StartupError{}), "github.com/faustbrian/go-cache/cacheservice"},
		{"legacy service Adapter", reflect.TypeOf(legacyservice.Adapter[int]{}), "github.com/faustbrian/go-cache/cacheservice"},
		{"canonical service Startup", reflect.TypeOf((cacheservice.Startup[int])(nil)), "github.com/faustbrian/go-cache/adapters/service"},
		{"canonical service Check", reflect.TypeOf((cacheservice.Check[int])(nil)), "github.com/faustbrian/go-cache/adapters/service"},
		{"canonical service Shutdown", reflect.TypeOf((cacheservice.Shutdown[int])(nil)), "github.com/faustbrian/go-cache/adapters/service"},
		{"canonical service Options", reflect.TypeOf(cacheservice.Options[int]{}), "github.com/faustbrian/go-cache/adapters/service"},
		{"canonical service OptionsError", reflect.TypeOf(cacheservice.OptionsError{}), "github.com/faustbrian/go-cache/adapters/service"},
		{"canonical service StartupError", reflect.TypeOf(cacheservice.StartupError{}), "github.com/faustbrian/go-cache/adapters/service"},
		{"canonical service Adapter", reflect.TypeOf(cacheservice.Adapter[int]{}), "github.com/faustbrian/go-cache/adapters/service"},
		{"legacy OTel Observer", reflect.TypeOf(legacyotel.Observer{}), "github.com/faustbrian/go-cache/observability/otel"},
		{"canonical OTel Observer", reflect.TypeOf(cacheotel.Observer{}), "github.com/faustbrian/go-cache/adapters/otel"},
		{"legacy slog Config", reflect.TypeOf(legacyslog.Config{}), "github.com/faustbrian/go-cache/observability/slog"},
		{"legacy slog Observer", reflect.TypeOf(legacyslog.Observer{}), "github.com/faustbrian/go-cache/observability/slog"},
		{"canonical slog Config", reflect.TypeOf(cacheslog.Config{}), "github.com/faustbrian/go-cache/adapters/slog"},
		{"canonical slog Observer", reflect.TypeOf(cacheslog.Observer{}), "github.com/faustbrian/go-cache/adapters/slog"},
	} {
		if got := identity.got.PkgPath(); got != identity.want {
			t.Errorf("%s package = %q, want %q", identity.name, got, identity.want)
		}
	}
	if !errors.Is(legacyservice.ErrInvalidOptions, cacheservice.ErrInvalidOptions) ||
		!errors.Is(legacyservice.ErrUnavailable, cacheservice.ErrUnavailable) {
		t.Fatal("legacy and canonical service sentinels differ")
	}
	_, err = cacheservice.New(cacheservice.Options[int]{})
	if err == nil || !errors.Is(err, cacheservice.ErrInvalidOptions) || err.Error() == "" {
		t.Fatalf("canonical invalid options error = %v", err)
	}
	validationErr := errors.New("validation marker")
	cleanupErr := errors.New("cleanup marker")
	canonicalService, err := cacheservice.New(cacheservice.Options[int]{
		Name:     "cache",
		Resource: 1,
		Startup: func(context.Context, int) error {
			return validationErr
		},
		Shutdown: func(context.Context, int) error {
			return cleanupErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	startupErr := canonicalService.Component().Start(context.Background())
	if startupErr == nil || !errors.Is(startupErr, validationErr) ||
		!errors.Is(startupErr, cleanupErr) ||
		startupErr.Error() != "cache service startup validation and cleanup failed" {
		t.Fatalf("canonical startup error = %v", startupErr)
	}
	validationOnly := &cacheservice.StartupError{Validation: validationErr}
	if !errors.Is(validationOnly, validationErr) ||
		validationOnly.Error() != "cache service startup validation failed" {
		t.Fatalf("canonical validation-only startup error = %v", validationOnly)
	}

	meter := noop.NewMeterProvider().Meter("cache-successors")
	canonicalOTel, err := cacheotel.New(meter)
	if err != nil {
		t.Fatal(err)
	}
	legacyOTel, err := legacyotel.New(meter)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	canonicalSlog, err := cacheslog.New(cacheslog.Config{Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	legacySlog, err := legacyslog.New(legacyslog.Config{Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	for _, observer := range []cache.Observer{canonicalOTel, legacyOTel, canonicalSlog, legacySlog} {
		if observeErr := observer.Observe(context.Background(), cache.Event{
			Operation: cache.OperationGet,
			Outcome:   cache.OutcomeHit,
		}); observeErr != nil {
			t.Fatalf("Observe() error = %v", observeErr)
		}
	}

}

type testWriter struct{ t *testing.T }

func (writer testWriter) Write(payload []byte) (int, error) {
	writer.t.Helper()
	return len(payload), nil
}
