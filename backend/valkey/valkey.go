package valkey

import (
	"context"

	valkeyclient "github.com/valkey-io/valkey-go"

	cache "github.com/faustbrian/go-cache"
	cachevalkey "github.com/faustbrian/go-cache/adapters/valkey"
)

// Config supplies the native client, clock, and maximum wire record size.
type Config struct {
	Client        valkeyclient.CommandClient
	Clock         cache.Clock
	MaxRecordSize int
}

// Backend preserves the legacy Valkey adapter type identity.
type Backend struct{ inner *cachevalkey.Backend }

// New validates config and constructs a Valkey backend.
func New(config Config) (*Backend, error) {
	inner, err := cachevalkey.New(cachevalkey.Config(config))
	if err != nil {
		return nil, err
	}

	return &Backend{inner: inner}, nil
}

// Get reads and validates one bounded wire record.
func (backend *Backend) Get(ctx context.Context, key string) (cache.Record, bool, error) {
	return backend.inner.Get(ctx, key)
}

// Set atomically writes a record with its stale deadline as server expiry.
func (backend *Backend) Set(
	ctx context.Context,
	key string,
	record cache.Record,
	condition cache.Condition,
) (bool, error) {
	return backend.inner.Set(ctx, key, record, condition)
}

// SetIfOwned atomically compares an active Valkey lease and publishes a record.
func (backend *Backend) SetIfOwned(
	ctx context.Context,
	key string,
	record cache.Record,
	guard cache.OwnershipGuard,
) error {
	return backend.inner.SetIfOwned(ctx, key, record, guard)
}

// Delete removes key and reports whether Valkey deleted it.
func (backend *Backend) Delete(ctx context.Context, key string) (bool, error) {
	return backend.inner.Delete(ctx, key)
}
