package cachevalkey_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"

	cache "github.com/faustbrian/go-cache/v2"
	cachevalkey "github.com/faustbrian/go-cache/v2/adapters/valkey"
)

func TestBackendRedactsClientDiagnostics(t *testing.T) {
	t.Parallel()

	const sensitive = "private-client-diagnostic-with-logical-key"
	cause := &sensitiveClientError{message: sensitive}
	client := valkeymock.NewClient(gomock.NewController(t))
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(cause))
	backend, err := cachevalkey.New(cachevalkey.Config{
		Client: client, Clock: cache.SystemClock{}, MaxRecordSize: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = backend.Get(t.Context(), "hashed-key")
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

func TestNewRejectsTypedNilDependencies(t *testing.T) {
	t.Parallel()

	if _, err := cachevalkey.New(cachevalkey.Config{
		Client: (*valkeymock.Client)(nil), Clock: cache.SystemClock{}, MaxRecordSize: 1024,
	}); !errors.Is(err, cache.ErrInvalidConfig) {
		t.Fatalf("typed nil client returned %v, want ErrInvalidConfig", err)
	}
	client := valkeymock.NewClient(gomock.NewController(t))
	if _, err := cachevalkey.New(cachevalkey.Config{
		Client: client, Clock: (*nilClock)(nil), MaxRecordSize: 1024,
	}); !errors.Is(err, cache.ErrInvalidConfig) {
		t.Fatalf("typed nil clock returned %v, want ErrInvalidConfig", err)
	}
}

type sensitiveClientError struct{ message string }

func (cause *sensitiveClientError) Error() string { return cause.message }

type nilClock struct{}

func (*nilClock) Now() time.Time { return time.Time{} }
