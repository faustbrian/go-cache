package cacheredis_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	redisclient "github.com/redis/go-redis/v9"

	cache "github.com/faustbrian/go-cache/v2"
	cacheredis "github.com/faustbrian/go-cache/v2/adapters/redis"
)

func TestBackendRedactsClientDiagnostics(t *testing.T) {
	t.Parallel()

	const sensitive = "private-client-diagnostic-with-logical-key"
	cause := &sensitiveClientError{message: sensitive}
	client := redisclient.NewClient(&redisclient.Options{
		Addr:       "cache.invalid:6379",
		MaxRetries: -1,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, cause
		},
	})
	t.Cleanup(func() { _ = client.Close() })
	backend, err := cacheredis.New(cacheredis.Config{
		Client: client, Clock: cache.SystemClock{}, MaxRecordSize: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err = backend.Get(ctx, "hashed-key")
	if !errors.Is(err, cache.ErrBackend) || !errors.Is(err, cause) {
		t.Fatalf("Get error = %v, want backend and source identities", err)
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Fatal("Get exposed the client diagnostic")
	}
	var exposed *sensitiveClientError
	if errors.As(err, &exposed) {
		t.Fatal("errors.As exposed the client cause")
	}
}

func TestBackendSetRedactsClientDiagnostics(t *testing.T) {
	t.Parallel()

	const sensitive = "private-set-diagnostic-with-logical-key"
	cause := &sensitiveClientError{message: sensitive}
	client := redisclient.NewClient(&redisclient.Options{
		Addr:       "cache.invalid:6379",
		MaxRetries: -1,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, cause
		},
	})
	t.Cleanup(func() { _ = client.Close() })
	backend, err := cacheredis.New(cacheredis.Config{
		Client: client, Clock: cache.SystemClock{}, MaxRecordSize: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Round(0)
	_, err = backend.Set(t.Context(), "hashed-key", cache.Record{
		Payload: []byte("payload"), ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(time.Minute),
	}, cache.Unconditional)
	if !errors.Is(err, cache.ErrBackend) || !errors.Is(err, cause) {
		t.Fatalf("Set error = %v, want backend and source identities", err)
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Fatal("Set exposed the client diagnostic")
	}
	var exposed *sensitiveClientError
	if errors.As(err, &exposed) {
		t.Fatal("errors.As exposed the client cause")
	}
}

func TestBackendSetClassifiesInvalidConditionAsPolicy(t *testing.T) {
	t.Parallel()

	client := redisclient.NewClient(&redisclient.Options{Addr: "cache.invalid:6379"})
	t.Cleanup(func() { _ = client.Close() })
	backend, err := cacheredis.New(cacheredis.Config{
		Client: client, Clock: cache.SystemClock{}, MaxRecordSize: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Round(0)
	_, err = backend.Set(t.Context(), "hashed-key", cache.Record{
		Payload: []byte("payload"), ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(time.Minute),
	}, cache.Condition(255))
	if !errors.Is(err, cache.ErrInvalidPolicy) {
		t.Fatalf("Set error = %v, want ErrInvalidPolicy", err)
	}
	if errors.Is(err, cache.ErrBackend) {
		t.Fatalf("Set caller policy error matched ErrBackend: %v", err)
	}
	var classified *cache.Error
	if !errors.As(err, &classified) || classified.Kind != cache.PolicyError {
		t.Fatalf("Set error = %#v, want PolicyError", err)
	}
}

func TestNewRejectsTypedNilDependencies(t *testing.T) {
	t.Parallel()

	if _, err := cacheredis.New(cacheredis.Config{
		Client: (*redisclient.Client)(nil), Clock: cache.SystemClock{}, MaxRecordSize: 1024,
	}); !errors.Is(err, cache.ErrInvalidConfig) {
		t.Fatalf("typed nil client returned %v, want ErrInvalidConfig", err)
	}
	client := redisclient.NewClient(&redisclient.Options{Addr: "cache.invalid:6379"})
	t.Cleanup(func() { _ = client.Close() })
	if _, err := cacheredis.New(cacheredis.Config{
		Client: client, Clock: (*nilClock)(nil), MaxRecordSize: 1024,
	}); !errors.Is(err, cache.ErrInvalidConfig) {
		t.Fatalf("typed nil clock returned %v, want ErrInvalidConfig", err)
	}
}

type sensitiveClientError struct{ message string }

func (cause *sensitiveClientError) Error() string { return cause.message }

type nilClock struct{}

func (*nilClock) Now() time.Time { return time.Time{} }
