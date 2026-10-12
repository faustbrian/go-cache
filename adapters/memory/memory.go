package cachememory

import (
	"container/list"
	"context"
	"reflect"
	"sync"

	cache "github.com/faustbrian/go-cache/v3"
)

// Config defines hard entry and retained-byte limits for a Backend.
type Config struct {
	// MaxEntries bounds retained entries, not all allocations or initial map overhead.
	MaxEntries int
	// MaxBytes bounds retained key+payload bytes, excluding map/list/runtime overhead.
	MaxBytes int
	// Clock is retained and called under the backend mutex; it must not re-enter.
	Clock cache.Clock
	// Observer optionally receives removal events after unlocking; it may block the call.
	Observer cache.Observer
}

// Stats is a snapshot of retained state and cumulative removals.
type Stats struct {
	// Entries counts retained entries, including expired entries not yet touched.
	Entries int
	// Bytes counts retained key+payload bytes, not total process memory.
	Bytes int
	// Evictions counts capacity removals since construction, not explicit deletes.
	Evictions uint64
	// Expirations counts lazy hard-deadline removals since construction.
	Expirations uint64
}

// Backend is a bounded concurrency-safe LRU backend.
type Backend struct {
	mu          sync.Mutex
	maxEntries  int
	maxBytes    int
	clock       cache.Clock
	observer    cache.Observer
	items       map[string]*list.Element
	lru         list.List
	bytes       int
	evictions   uint64
	expirations uint64
	closed      bool
}

type entry struct {
	key    string
	record cache.Record
	size   int
}

// New validates config and constructs an empty memory backend.
func New(config Config) (*Backend, error) {
	if config.MaxEntries <= 0 || config.MaxBytes <= 0 || isNilDependency(config.Clock) ||
		(config.Observer != nil && isNilDependency(config.Observer)) {
		return nil, cache.ErrInvalidConfig
	}
	return &Backend{
		maxEntries: config.MaxEntries,
		maxBytes:   config.MaxBytes,
		clock:      config.Clock,
		observer:   config.Observer,
		items:      make(map[string]*list.Element, config.MaxEntries),
	}, nil
}

func isNilDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.String, reflect.Struct, reflect.UnsafePointer:
	}
	return false
}

// Get returns a cloned live record or an explicit miss.
func (b *Backend) Get(ctx context.Context, key string) (cache.Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return cache.Record{}, false, err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return cache.Record{}, false, cache.ErrClosed
	}
	element, found := b.items[key]
	if !found {
		b.mu.Unlock()
		return cache.Record{}, false, nil
	}
	item := element.Value.(*entry)
	if !item.record.StaleAt.IsZero() && !b.clock.Now().Round(0).Before(item.record.StaleAt) {
		b.remove(element)
		b.expirations++
		retained := b.bytes
		b.mu.Unlock()
		b.notify(ctx, cache.Event{Operation: cache.OperationExpire, Outcome: cache.OutcomeExpired, Size: retained})
		return cache.Record{}, false, nil
	}
	b.lru.MoveToFront(element)
	record := item.record.Clone()
	b.mu.Unlock()
	return record, true, nil
}

// Set atomically applies condition and stores a cloned record. A structurally
// valid past hard deadline is admitted and removed lazily on access. Size means
// key+payload bytes, not heap overhead. Eviction may remove multiple entries.
func (b *Backend) Set(
	ctx context.Context,
	key string,
	record cache.Record,
	condition cache.Condition,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := record.Validate(); err != nil {
		return false, err
	}
	if condition != cache.Unconditional && condition != cache.IfAbsent && condition != cache.IfPresent {
		return false, cache.ErrInvalidPolicy
	}
	size := len(key) + len(record.Payload)
	if size > b.maxBytes {
		return false, cache.ErrCapacity
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return false, cache.ErrClosed
	}
	existing, found, expired := b.liveElement(key)
	if condition == cache.IfAbsent && found || condition == cache.IfPresent && !found {
		retained := b.bytes
		b.mu.Unlock()
		if expired {
			b.notify(ctx, cache.Event{Operation: cache.OperationExpire, Outcome: cache.OutcomeExpired, Size: retained})
		}
		return false, nil
	}
	if found {
		b.remove(existing)
	}
	evicted := 0
	for len(b.items) >= b.maxEntries || b.bytes+size > b.maxBytes {
		oldest := b.lru.Back()
		if oldest == nil {
			b.mu.Unlock()
			return false, cache.ErrCapacity
		}
		b.remove(oldest)
		b.evictions++
		evicted++
	}
	item := &entry{key: key, record: record.Clone(), size: size}
	element := b.lru.PushFront(item)
	b.items[key] = element
	b.bytes += size
	retained := b.bytes
	b.mu.Unlock()
	if expired {
		b.notify(ctx, cache.Event{Operation: cache.OperationExpire, Outcome: cache.OutcomeExpired, Size: retained})
	}
	for range evicted {
		b.notify(ctx, cache.Event{Operation: cache.OperationEvict, Outcome: cache.OutcomeEvicted, Size: retained})
	}
	return true, nil
}

// Delete removes key and reports whether it was present.
func (b *Backend) Delete(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return false, cache.ErrClosed
	}
	element, found, expired := b.liveElement(key)
	if !found {
		retained := b.bytes
		b.mu.Unlock()
		if expired {
			b.notify(ctx, cache.Event{Operation: cache.OperationExpire, Outcome: cache.OutcomeExpired, Size: retained})
		}
		return false, nil
	}
	b.remove(element)
	b.mu.Unlock()
	return true, nil
}

// Stats returns a synchronized snapshot without sweeping expired entries.
// Removal counters survive Close, which clears retained entries/bytes.
func (b *Backend) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Stats{
		Entries:     len(b.items),
		Bytes:       b.bytes,
		Evictions:   b.evictions,
		Expirations: b.expirations,
	}
}

// Close releases retained entries and rejects subsequent operations.
func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	b.items = nil
	b.lru.Init()
	b.bytes = 0
	return nil
}

func (b *Backend) liveElement(key string) (*list.Element, bool, bool) {
	element, found := b.items[key]
	if !found {
		return nil, false, false
	}
	item := element.Value.(*entry)
	if !item.record.StaleAt.IsZero() && !b.clock.Now().Round(0).Before(item.record.StaleAt) {
		b.remove(element)
		b.expirations++
		return nil, false, true
	}
	return element, true, false
}

func (b *Backend) notify(ctx context.Context, event cache.Event) {
	if b.observer == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	_ = b.observer.Observe(ctx, event)
}

func (b *Backend) remove(element *list.Element) {
	item := element.Value.(*entry)
	delete(b.items, item.key)
	b.lru.Remove(element)
	b.bytes -= item.size
}
