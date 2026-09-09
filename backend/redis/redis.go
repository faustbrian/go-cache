package redis

import (
	"context"

	redisclient "github.com/redis/go-redis/v9"

	cache "github.com/faustbrian/go-cache"
	cacheredis "github.com/faustbrian/go-cache/adapters/redis"
)

// Config supplies the native client, clock, and maximum wire record size.
type Config struct {
	Client        redisclient.UniversalClient
	Clock         cache.Clock
	MaxRecordSize int
}

// Backend preserves the legacy Redis adapter type identity.
type Backend struct{ inner *cacheredis.Backend }

// New validates config and constructs a Redis backend.
func New(config Config) (*Backend, error) {
	inner, err := cacheredis.New(cacheredis.Config(config))
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

// Delete removes key and reports whether Redis deleted it.
func (backend *Backend) Delete(ctx context.Context, key string) (bool, error) {
	return backend.inner.Delete(ctx, key)
}
