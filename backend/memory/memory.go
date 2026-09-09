package memory

import (
	"context"

	cache "github.com/faustbrian/go-cache"
	cachememory "github.com/faustbrian/go-cache/adapters/memory"
)

// Config defines hard entry and retained-byte limits for a Backend.
type Config struct {
	MaxEntries int
	MaxBytes   int
	Clock      cache.Clock
	Observer   cache.Observer
}

// Stats is a snapshot of retained state and cumulative removals.
type Stats struct {
	Entries     int
	Bytes       int
	Evictions   uint64
	Expirations uint64
}

// Backend preserves the legacy memory adapter type identity.
type Backend struct{ inner *cachememory.Backend }

// New validates config and constructs an empty memory backend.
func New(config Config) (*Backend, error) {
	inner, err := cachememory.New(cachememory.Config(config))
	if err != nil {
		return nil, err
	}

	return &Backend{inner: inner}, nil
}

// Get returns a cloned live record or an explicit miss.
func (backend *Backend) Get(ctx context.Context, key string) (cache.Record, bool, error) {
	return backend.inner.Get(ctx, key)
}

// Set atomically applies condition and stores a cloned record.
func (backend *Backend) Set(
	ctx context.Context,
	key string,
	record cache.Record,
	condition cache.Condition,
) (bool, error) {
	return backend.inner.Set(ctx, key, record, condition)
}

// Delete removes key and reports whether it was present.
func (backend *Backend) Delete(ctx context.Context, key string) (bool, error) {
	return backend.inner.Delete(ctx, key)
}

// Stats returns a synchronized snapshot of backend counters.
func (backend *Backend) Stats() Stats { return Stats(backend.inner.Stats()) }

// Close releases retained entries and rejects subsequent operations.
func (backend *Backend) Close() error { return backend.inner.Close() }
