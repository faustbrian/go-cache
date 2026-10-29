package cache

import (
	"cmp"
	"context"
	"errors"
	"math/rand/v2"
	"reflect"
	"slices"
	"sync"
	"time"
)

const (
	defaultMaxConcurrentLoaders = 64
	defaultMaxFlights           = 1024
	defaultMaxWaitersPerKey     = 1024
	defaultMaxBatch             = 1000
	defaultCloseTimeout         = 5 * time.Second
)

// MaxLoadFlights bounds configured loader concurrency and retained flight work.
const MaxLoadFlights = 1 << 16

// LoadResult is the value and existence result returned by a Loader.
type LoadResult[V any] struct {
	// Value is the loader result, shared without deep copying among waiters.
	Value V
	// Found distinguishes a present zero/nil value from authoritative absence.
	Found bool
	// Expiry optionally replaces the fresh TTL and jitter (found) or NegativeTTL
	// (absent) for this entry; zero keeps the cache policy. A non-zero Expiry on
	// an absent result is stored even when NegativeTTL is zero. A passed
	// deadline deletes the key; waiters still receive this result.
	Expiry Expiry
}

// Loader fetches a logical key from its source of truth. It receives a
// cache-owned context, not the first waiter's context/values/deadline. Honor its
// cancellation and bound external calls; returned mutable V is shared among
// flight waiters. Errors are protected; panic values are discarded as ErrLoaderPanic.
type Loader[K, V any] func(context.Context, K) (LoadResult[V], error)

// JitterSource chooses how much time to subtract from a loaded value's TTL.
type JitterSource interface {
	// Duration chooses TTL subtraction; custom results must be within the requested bound.
	Duration(time.Duration) time.Duration
}

// RandomJitter samples a non-cryptographic duration in [0, max).
type RandomJitter struct{}

// Duration returns a non-negative duration below max, or zero for max <= 0.
func (RandomJitter) Duration(upperBound time.Duration) time.Duration {
	if upperBound <= 0 {
		return 0
	}
	// Jitter distributes expirations; it is not used for a security decision.
	return time.Duration(rand.Int64N(int64(upperBound))) // #nosec G404 -- TTL jitter distributes expirations and never supplies credentials or security decisions.
}

// LoadPolicy bounds loading and enables optional negative and stale behavior.
type LoadPolicy struct {
	// MaxConcurrent bounds running loaders; zero selects 64.
	MaxConcurrent int
	// MaxFlights bounds distinct-key flights, including queued loads and
	// completed flights retained by an explicit mutation. Zero defaults to 1024.
	MaxFlights int
	// MaxWaitersPerKey bounds attached foreground callers per flight; zero selects 1024.
	MaxWaitersPerKey int
	// NegativeTTL is absence retention; zero disables storage of negative load
	// results unless the loader returns a non-zero LoadResult.Expiry.
	NegativeTTL time.Duration
	// StaleWhileRevalidate serves stale data and admits a shared background refresh.
	StaleWhileRevalidate bool
	// StaleIfError returns the originally read stale value with a foreground refresh error.
	StaleIfError bool
	// RefreshJitter is the maximum loaded-value TTL subtraction; it must be below TTL.
	RefreshJitter time.Duration
}

// Config contains all dependencies, limits, and policies for a Cache.
type Config[K, V any] struct {
	// Backend is a retained collaborator; Cache shutdown does not close it.
	Backend Backend
	// Keys must be built by NewKeySpace; New does not validate a zero key space.
	Keys KeySpace[K]
	// Codec is retained and may be called concurrently; callbacks are not panic-isolated.
	Codec Codec[V]
	// TTL sets positive-record freshness, hard staleness and optional sliding reads.
	TTL TTLPolicy
	// Clock is retained; its Now method must be safe for concurrent bounded calls.
	Clock Clock
	// MaxValue is the positive encoded payload limit, excluding a backend wire header.
	MaxValue int
	// MaxBatch bounds sequential bulk items; zero selects 1000.
	MaxBatch int
	// Load configures process-local flight, loader, stale and negative policy.
	Load LoadPolicy
	// Jitter optionally supplies concurrent TTL sampling; nil selects RandomJitter when needed.
	Jitter JitterSource
	// Observer optionally receives synchronous events; Cache owns no exporter lifecycle.
	Observer Observer
}

// Cache provides concurrent typed cache operations over a retained backend.
// Use New; a zero/nil Cache is not usable. Dependencies must support concurrent
// calls, non-nil contexts are required except that Shutdown rejects nil, and
// callers own mutation of generic keys/values and dependency cleanup.
type Cache[K, V any] struct {
	backend      Backend
	keys         KeySpace[K]
	codec        Codec[V]
	ttl          TTLPolicy
	clock        Clock
	maxValue     int
	maxBatch     int
	load         LoadPolicy
	jitter       JitterSource
	observer     Observer
	closeTimeout time.Duration
	loadSlots    chan struct{}
	loadCtx      context.Context
	cancel       context.CancelFunc
	loadMu       sync.Mutex
	flights      map[string]*loadFlight[V]
	activeLoads  int
	loadsDone    chan struct{}
	closed       bool
	// beforeLoadPublication is an internal synchronization seam for deterministic
	// publication-boundary concurrency tests. Production caches leave it nil.
	beforeLoadPublication func()
}

// loadFlight publishes result/err before closing done. loadMu owns accounting;
// mutation separately orders explicit writes with load publication, so a flight
// retained by a waiting mutation still consumes admission capacity.
type loadFlight[V any] struct {
	done          chan struct{}
	result        Result[V]
	err           error
	waiters       int
	mutation      sync.Mutex
	mutationUsers int
	finished      bool
	superseded    bool
}

type loadContextKey struct{}

type activeLoadContext struct{ owner any }

// New validates dependencies, TTL and load bounds and constructs an inert cache.
// Keys must already come from NewKeySpace; its zero value is not validated here.
// No goroutine is started until a flight is admitted. Config collaborators are
// retained by identity; construction does not clone or open backend resources.
func New[K, V any](config Config[K, V]) (*Cache[K, V], error) {
	if isNilDependency(config.Backend) || isNilDependency(config.Codec) || isNilDependency(config.Clock) ||
		isTypedNil(config.Jitter) || isTypedNil(config.Observer) || config.MaxValue <= 0 || config.MaxBatch < 0 {
		return nil, ErrInvalidConfig
	}
	if err := config.TTL.Validate(); err != nil {
		return nil, err
	}
	if config.Load.NegativeTTL < 0 || config.Load.MaxConcurrent < 0 || config.Load.MaxConcurrent > MaxLoadFlights || config.Load.MaxFlights < 0 || config.Load.MaxFlights > MaxLoadFlights || config.Load.MaxWaitersPerKey < 0 || config.Load.RefreshJitter < 0 {
		return nil, NewError(PolicyError, OperationLoad, ErrInvalidPolicy)
	}
	if config.Load.StaleWhileRevalidate && config.Load.StaleIfError {
		return nil, NewError(PolicyError, OperationLoad, ErrInvalidPolicy)
	}
	switch cmp.Compare(config.Load.RefreshJitter, 0) {
	case 1:
		if cmp.Compare(config.Load.RefreshJitter, config.TTL.TTL) != -1 {
			return nil, NewError(PolicyError, OperationLoad, ErrInvalidPolicy)
		}
		switch config.Jitter {
		case nil:
			config.Jitter = RandomJitter{}
		}
	}
	switch cmp.Compare(config.Load.MaxConcurrent, 0) {
	case 0:
		config.Load.MaxConcurrent = defaultMaxConcurrentLoaders
	}
	if config.Load.MaxFlights == 0 {
		config.Load.MaxFlights = defaultMaxFlights
	}
	if config.Load.MaxFlights < config.Load.MaxConcurrent {
		return nil, NewError(PolicyError, OperationLoad, ErrInvalidPolicy)
	}
	switch cmp.Compare(config.Load.MaxWaitersPerKey, 0) {
	case 0:
		config.Load.MaxWaitersPerKey = defaultMaxWaitersPerKey
	}
	switch cmp.Compare(config.MaxBatch, 0) {
	case 0:
		config.MaxBatch = defaultMaxBatch
	}
	// Close retains and invokes cancel after preventing new flights.
	loadCtx, cancel := context.WithCancel(context.Background()) // #nosec G118 -- Cache retains cancel and Shutdown invokes it after rejecting new flights; Close delegates to Shutdown.
	return &Cache[K, V]{
		backend:      config.Backend,
		keys:         config.Keys,
		codec:        config.Codec,
		ttl:          config.TTL,
		clock:        config.Clock,
		maxValue:     config.MaxValue,
		maxBatch:     config.MaxBatch,
		load:         config.Load,
		jitter:       config.Jitter,
		observer:     config.Observer,
		closeTimeout: defaultCloseTimeout,
		loadSlots:    make(chan struct{}, config.Load.MaxConcurrent),
		loadCtx:      loadCtx,
		cancel:       cancel,
		flights:      make(map[string]*loadFlight[V]),
		loadsDone:    make(chan struct{}),
	}, nil
}

// Get returns an explicit hit, negative/ordinary miss, or stale result.
// Freshness is Before(ExpiresAt); StaleAt is exclusive hard expiry. Hard expiry
// cleanup failures remain errors. Positive Decode ownership comes from Codec.
// Sliding fresh reads rewrite the record using IfPresent, not record-version
// compare-and-set, and can race a newer write. Stale/negative reads do not slide.
func (c *Cache[K, V]) Get(ctx context.Context, logical K) (result Result[V], err error) {
	start := c.clock.Now()
	size := 0
	defer func() {
		notify(ctx, c.observer, Event{
			Operation: OperationGet,
			Outcome:   resultOutcome(result, err),
			Duration:  elapsed(start, c.clock.Now()),
			Size:      size,
		})
	}()
	var zero Result[V]
	switch err := ctx.Err(); err {
	case nil:
	default:
		return zero, err
	}
	if c.isClosed() {
		return zero, ErrClosed
	}
	key, err := c.keys.Key(logical)
	switch err {
	case nil:
	default:
		return zero, NewError(InvalidKeyError, OperationGet, err)
	}
	record, found, err := c.backend.Get(ctx, key)
	switch err {
	case nil:
	default:
		return zero, operationError(OperationGet, err)
	}
	if !found {
		return Result[V]{State: Miss}, nil
	}
	if err := record.Validate(); err != nil {
		return zero, operationError(OperationGet, err)
	}
	size = len(record.Payload)

	now := c.clock.Now().Round(0)
	if !now.Before(record.StaleAt) {
		if _, err := c.backend.Delete(ctx, key); err != nil {
			return zero, operationError(OperationDelete, err)
		}
		return Result[V]{State: Miss}, nil
	}
	if record.Negative {
		return Result[V]{State: Miss, Negative: true}, nil
	}
	if cmp.Compare(len(record.Payload), c.maxValue) == 1 {
		return zero, NewError(LimitError, OperationGet, ErrValueTooLarge)
	}
	value, err := c.codec.Decode(record.Payload)
	if err != nil {
		return zero, codecError(OperationGet, err)
	}
	if !now.Before(record.ExpiresAt) {
		return Result[V]{State: Stale, Value: value}, nil
	}
	if c.ttl.Sliding {
		record.ExpiresAt = now.Add(c.ttl.TTL)
		record.StaleAt = record.ExpiresAt.Add(c.ttl.StaleFor)
		updated, err := c.backend.Set(ctx, key, record, IfPresent)
		if err != nil {
			return zero, operationError(OperationSet, err)
		}
		if !updated {
			return Result[V]{State: Miss}, nil
		}
	}
	return Result[V]{State: Hit, Value: value}, nil
}

// GetOrLoad returns a cached value or coalesces source work by encoded key.
// The first admitted flight's loader/key arguments serve every follower. Caller
// cancellation only detaches that waiter, including the last waiter. All
// followers of a published found load share its original V, not deep copies.
// Double checks or superseding mutations can instead return current decoded state.
// Stale-while-revalidate returns admission errors with stale data; later refresh
// errors are observed. Stale-if-error returns the original stale data plus error.
// Recursive calls with the supplied loader context reject the same Cache even
// for a different key; discarding that context bypasses detection.
func (c *Cache[K, V]) GetOrLoad(ctx context.Context, logical K, loader Loader[K, V]) (Result[V], error) {
	active, ok := ctx.Value(loadContextKey{}).(activeLoadContext)
	switch ok {
	case true:
		switch active.owner {
		case c:
			return Result[V]{}, NewError(LoaderError, OperationLoad, ErrRecursiveLoad)
		}
	}
	result, err := c.Get(ctx, logical)
	switch err {
	case nil:
	default:
		return result, err
	}
	switch result.State {
	case Hit:
		return result, nil
	case Miss, Stale:
	}
	switch result.Negative {
	case true:
		return result, nil
	}
	switch loader {
	case nil:
		return Result[V]{}, NewError(LoaderError, OperationLoad, errors.New("nil loader"))
	}
	key, err := c.keys.Key(logical)
	switch err {
	case nil:
	default:
		return Result[V]{}, NewError(InvalidKeyError, OperationLoad, err)
	}
	switch result.State {
	case Stale:
		switch c.load.StaleWhileRevalidate {
		case true:
			if err := c.startBackgroundLoad(key, logical, loader); err != nil {
				return result, err
			}
			return result, nil
		}
	case Hit, Miss:
	}

	c.loadMu.Lock()
	if c.closed {
		c.loadMu.Unlock()
		return Result[V]{}, ErrClosed
	}
	flight, found := c.flights[key]
	if found {
		switch cmp.Compare(flight.waiters, c.load.MaxWaitersPerKey) {
		case 0, 1:
			c.loadMu.Unlock()
			return Result[V]{}, ErrWaiterLimit
		}
		flight.waiters++
	} else {
		flight, err = c.admitFlightLocked(key, logical, loader, 1)
		if err != nil {
			c.loadMu.Unlock()
			return Result[V]{}, err
		}
	}
	c.loadMu.Unlock()

	select {
	case <-ctx.Done():
		c.detachWaiter(flight)
		return Result[V]{}, ctx.Err()
	case <-flight.done:
		c.detachWaiter(flight)
		if flight.err != nil && result.State == Stale && c.load.StaleIfError {
			return result, flight.err
		}
		return flight.result, flight.err
	}
}

func (c *Cache[K, V]) startBackgroundLoad(key string, logical K, loader Loader[K, V]) error {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if _, found := c.flights[key]; found {
		return nil
	}
	_, err := c.admitFlightLocked(key, logical, loader, 0)
	return err
}

// Admission precedes allocation and spawning; loader slots alone do not bound
// distinct-key work waiting to enter the loader. Mutation-reserved completed
// flights remain in the map and must continue consuming this budget.
func (c *Cache[K, V]) admitFlightLocked(key string, logical K, loader Loader[K, V], waiters int) (*loadFlight[V], error) {
	if len(c.flights) >= c.load.MaxFlights || c.activeLoads >= c.load.MaxFlights {
		return nil, ErrFlightLimit
	}
	flight := &loadFlight[V]{done: make(chan struct{}), waiters: waiters}
	c.flights[key] = flight
	c.activeLoads++
	go c.runLoad(key, logical, loader, flight)
	return flight, nil
}

func (c *Cache[K, V]) runLoad(key string, logical K, loader Loader[K, V], flight *loadFlight[V]) {
	var loadStarted time.Time
	loadOutcome := OutcomeSuccess
	didLoad := false
	defer c.finishLoad()
	defer func() {
		if didLoad {
			if flight.err != nil {
				loadOutcome = OutcomeError
			}
			notify(c.loadCtx, c.observer, Event{
				Operation: OperationLoad,
				Outcome:   loadOutcome,
				Duration:  elapsed(loadStarted, c.clock.Now()),
			})
		}
		c.loadMu.Lock()
		flight.finished = true
		close(flight.done)
		if flight.mutationUsers == 0 && c.flights[key] == flight {
			delete(c.flights, key)
		}
		c.loadMu.Unlock()
	}()

	current, err := c.Get(c.loadCtx, logical)
	switch err {
	case nil:
	default:
		flight.err = err
		return
	}
	switch current.State {
	case Hit:
		flight.result = current
		return
	case Miss, Stale:
	}
	switch current.Negative {
	case true:
		flight.result = current
		return
	}

	select {
	case c.loadSlots <- struct{}{}:
		defer func() { <-c.loadSlots }()
	case <-c.loadCtx.Done():
		flight.err = c.loadCtx.Err()
		return
	}

	didLoad = true
	loadStarted = c.clock.Now()
	loaderCtx := context.WithValue(c.loadCtx, loadContextKey{}, activeLoadContext{owner: c})
	loaded, err := callLoader(loaderCtx, logical, loader)
	if err != nil {
		flight.err = err
		return
	}
	if err := c.loadCtx.Err(); err != nil {
		flight.err = err
		return
	}
	if c.beforeLoadPublication != nil {
		c.beforeLoadPublication()
	}
	flight.mutation.Lock()
	if err := c.loadCtx.Err(); err != nil {
		flight.mutation.Unlock()
		flight.err = err
		return
	}
	if flight.superseded {
		flight.mutation.Unlock()
		current, err := c.Get(c.loadCtx, logical)
		flight.result = current
		flight.err = err
		return
	}
	defer flight.mutation.Unlock()
	if !loaded.Expiry.IsZero() {
		staleFor := time.Duration(0)
		if loaded.Found {
			staleFor = c.ttl.StaleFor
		}
		if _, _, _, err := loaded.Expiry.deadlines(OperationLoad, c.clock.Now().Round(0), staleFor, c.ttl.Sliding); err != nil {
			flight.err = err
			return
		}
	}
	if !loaded.Found {
		loadOutcome = OutcomeNegative
		flight.result = Result[V]{State: Miss, Negative: true}
		if err := c.setLoadedNegative(key, loaded.Expiry); err != nil {
			flight.err = err
		}
		return
	}
	if err := c.setLoaded(c.loadCtx, logical, loaded.Value, loaded.Expiry); err != nil {
		flight.err = err
		return
	}
	flight.result = Result[V]{State: Hit, Value: loaded.Value}
}

func callLoader[K, V any](ctx context.Context, logical K, loader Loader[K, V]) (
	loaded LoadResult[V],
	err error,
) {
	defer func() {
		if recover() != nil {
			err = NewError(LoaderError, OperationLoad, ErrLoaderPanic)
		}
	}()
	loaded, err = loader(ctx, logical)
	if err != nil {
		return LoadResult[V]{}, NewError(LoaderError, OperationLoad, err)
	}
	return loaded, nil
}

func (c *Cache[K, V]) finishLoad() {
	c.loadMu.Lock()
	c.activeLoads--
	if c.closed && c.activeLoads == 0 {
		close(c.loadsDone)
	}
	c.loadMu.Unlock()
}

func (c *Cache[K, V]) detachWaiter(flight *loadFlight[V]) {
	c.loadMu.Lock()
	flight.waiters--
	c.loadMu.Unlock()
}

// Close cancels active loads, rejects new loads, and waits up to five seconds
// for loader/notification cleanup. It does not close the backend or client.
func (c *Cache[K, V]) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.closeTimeout)
	defer cancel()

	return c.Shutdown(ctx)
}

// Shutdown cancels active loads, rejects new loads, and waits for loader
// cleanup within ctx. A nil error means all active loads have completed,
// including synchronous load observers, not all independent foreground calls.
// Supplied backends/clients remain caller-owned and are not closed.
// ErrShutdownIncomplete permits late completion of already-admitted backend
// work when trusted callbacks or backends ignore cancellation.
func (c *Cache[K, V]) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidConfig
	}
	c.loadMu.Lock()
	if !c.closed {
		c.closed = true
		c.cancel()
		if c.activeLoads == 0 {
			close(c.loadsDone)
		}
	}
	done := c.loadsDone
	c.loadMu.Unlock()

	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.Join(ErrShutdownIncomplete, ctx.Err())
	}
}

func isNilDependency(dependency any) bool {
	return dependency == nil || isTypedNil(dependency)
}

func isTypedNil(dependency any) bool {
	if dependency == nil {
		return false
	}
	value := reflect.ValueOf(dependency)
	kind := value.Kind()
	if kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice {
		return value.IsNil()
	}
	return false
}

func (c *Cache[K, V]) isClosed() bool {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()
	return c.closed
}

// Set writes a value without an existence precondition.
func (c *Cache[K, V]) Set(ctx context.Context, logical K, value V) error {
	_, err := c.set(ctx, logical, value, Unconditional, c.ttl.TTL, Expiry{}, true)
	return err
}

// SetWithExpiry writes a value without an existence precondition using a
// per-entry expiry. A zero Expiry behaves exactly like Set. An absolute
// deadline replaces the policy TTL while the configured StaleFor still follows
// it; NeverExpire ignores both. A deadline at or before the cache clock deletes
// the key instead of storing an expired value and still supersedes an active
// load. Non-zero expiries are rejected with ErrInvalidPolicy when the TTL
// policy is Sliding, because a sliding hit would replace the explicit deadline.
func (c *Cache[K, V]) SetWithExpiry(ctx context.Context, logical K, value V, expiry Expiry) error {
	_, err := c.set(ctx, logical, value, Unconditional, c.ttl.TTL, expiry, true)
	return err
}

// SetIfOwned atomically writes a value only while guard identifies the active
// backend owner. Ownership loss is reported as ErrOwnershipLost.
func (c *Cache[K, V]) SetIfOwned(
	ctx context.Context,
	logical K,
	value V,
	guard OwnershipGuard,
) error {
	return c.setIfOwned(ctx, logical, guard, func() (Record, int, error) {
		payload, err := c.codec.Encode(value)
		if err != nil {
			return Record{}, 0, codecError(OperationSet, err)
		}
		if cmp.Compare(len(payload), c.maxValue) == 1 {
			return Record{}, 0, NewError(LimitError, OperationSet, ErrValueTooLarge)
		}
		now := c.clock.Now().Round(0)
		return Record{
			Payload:   payload,
			ExpiresAt: now.Add(c.ttl.TTL),
			StaleAt:   now.Add(c.ttl.TTL).Add(c.ttl.StaleFor),
		}, len(payload), nil
	})
}

// SetNegativeIfOwned atomically writes an explicit negative record only while
// guard identifies the active backend owner. The configured NegativeTTL must
// be positive. Ownership loss is reported as ErrOwnershipLost.
func (c *Cache[K, V]) SetNegativeIfOwned(
	ctx context.Context,
	logical K,
	guard OwnershipGuard,
) error {
	return c.setIfOwned(ctx, logical, guard, func() (Record, int, error) {
		if c.load.NegativeTTL <= 0 {
			return Record{}, 0, NewError(PolicyError, OperationSet, ErrInvalidPolicy)
		}
		now := c.clock.Now().Round(0)
		return Record{
			ExpiresAt: now.Add(c.load.NegativeTTL),
			StaleAt:   now.Add(c.load.NegativeTTL),
			Negative:  true,
		}, 0, nil
	})
}

func (c *Cache[K, V]) setIfOwned(
	ctx context.Context,
	logical K,
	guard OwnershipGuard,
	buildRecord func() (Record, int, error),
) (err error) {
	start := c.clock.Now()
	size := 0
	defer func() {
		outcome := OutcomeSuccess
		switch err {
		case nil:
		default:
			outcome = OutcomeError
		}
		notify(ctx, c.observer, Event{
			Operation: OperationSet,
			Outcome:   outcome,
			Duration:  elapsed(start, c.clock.Now()),
			Size:      size,
		})
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.isClosed() {
		return ErrClosed
	}
	switch guard {
	case nil:
		return NewError(PolicyError, OperationSet, ErrInvalidPolicy)
	}
	if slices.Contains([]string{guard.StorageKey(), guard.Owner(), guard.Token()}, "") {
		return NewError(PolicyError, OperationSet, ErrInvalidPolicy)
	}
	backend, supported := c.backend.(OwnershipBackend)
	if !supported {
		return ErrOwnershipUnsupported
	}
	key, err := c.keys.Key(logical)
	if err != nil {
		return NewError(InvalidKeyError, OperationSet, err)
	}
	flight := c.lockFlightMutation(key, true)
	if flight != nil {
		defer c.unlockFlightMutation(key, flight)
	}
	record, size, err := buildRecord()
	if err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if err := backend.SetIfOwned(ctx, key, record, guard); err != nil {
		return operationError(OperationSet, err)
	}
	if flight != nil {
		flight.superseded = true
	}
	return nil
}

// Add writes a value only when the key has no live record.
func (c *Cache[K, V]) Add(ctx context.Context, logical K, value V) (bool, error) {
	return c.set(ctx, logical, value, IfAbsent, c.ttl.TTL, Expiry{}, true)
}

// Replace writes a value only when the key has a live record.
func (c *Cache[K, V]) Replace(ctx context.Context, logical K, value V) (bool, error) {
	return c.set(ctx, logical, value, IfPresent, c.ttl.TTL, Expiry{}, true)
}

func (c *Cache[K, V]) setLoaded(ctx context.Context, logical K, value V, expiry Expiry) error {
	ttl := c.ttl.TTL
	if !expiry.IsZero() {
		_, err := c.set(ctx, logical, value, Unconditional, ttl, expiry, false)
		return err
	}
	if c.load.RefreshJitter > 0 {
		jitter := c.jitter.Duration(c.load.RefreshJitter)
		if slices.Contains([]bool{
			cmp.Compare(jitter, 0) == -1,
			cmp.Compare(jitter, c.load.RefreshJitter) == 1,
			cmp.Compare(jitter, ttl) != -1,
		}, true) {
			return NewError(PolicyError, OperationLoad, ErrInvalidPolicy)
		}
		ttl -= jitter
	}
	_, err := c.set(ctx, logical, value, Unconditional, ttl, Expiry{}, false)
	return err
}

// setLoadedNegative runs under the flight's mutation lock, like setLoaded.
func (c *Cache[K, V]) setLoadedNegative(key string, expiry Expiry) error {
	now := c.clock.Now().Round(0)
	record := Record{Negative: true}
	switch {
	case !expiry.IsZero():
		expiresAt, _, live, err := expiry.deadlines(OperationLoad, now, 0, c.ttl.Sliding)
		if err != nil {
			return err
		}
		if !live {
			if _, err := c.backend.Delete(c.loadCtx, key); err != nil {
				return operationError(OperationDelete, err)
			}
			return nil
		}
		record.ExpiresAt, record.StaleAt = expiresAt, expiresAt
	case c.load.NegativeTTL > 0:
		record.ExpiresAt = now.Add(c.load.NegativeTTL)
		record.StaleAt = record.ExpiresAt
	default:
		return nil
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if _, err := c.backend.Set(c.loadCtx, key, record, Unconditional); err != nil {
		return operationError(OperationSet, err)
	}
	return nil
}

func (c *Cache[K, V]) set(
	ctx context.Context,
	logical K,
	value V,
	condition Condition,
	ttl time.Duration,
	expiry Expiry,
	supersede bool,
) (written bool, err error) {
	start := c.clock.Now()
	size := 0
	defer func() {
		outcome := OutcomeSuccess
		if err != nil {
			outcome = OutcomeError
		} else if !written {
			outcome = OutcomeRejected
		}
		notify(ctx, c.observer, Event{
			Operation: OperationSet,
			Outcome:   outcome,
			Duration:  elapsed(start, c.clock.Now()),
			Size:      size,
		})
	}()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c.isClosed() {
		return false, ErrClosed
	}
	key, err := c.keys.Key(logical)
	if err != nil {
		return false, NewError(InvalidKeyError, OperationSet, err)
	}
	flight := c.lockFlightMutation(key, supersede)
	if flight != nil {
		defer c.unlockFlightMutation(key, flight)
	}
	payload, err := c.codec.Encode(value)
	if err != nil {
		return false, codecError(OperationSet, err)
	}
	if cmp.Compare(len(payload), c.maxValue) == 1 {
		return false, NewError(LimitError, OperationSet, ErrValueTooLarge)
	}
	size = len(payload)
	now := c.clock.Now().Round(0)
	record := Record{
		Payload:   payload,
		ExpiresAt: now.Add(ttl),
		StaleAt:   now.Add(ttl).Add(c.ttl.StaleFor),
	}
	if !expiry.IsZero() {
		expiresAt, staleAt, live, err := expiry.deadlines(OperationSet, now, c.ttl.StaleFor, c.ttl.Sliding)
		if err != nil {
			return false, err
		}
		if !live {
			// Only unconditional writes carry an expiry; an already expired write
			// must still remove the previous value, as a provider-side past
			// deadline would. Nothing is stored, so no payload size is observed.
			size = 0
			if _, err := c.backend.Delete(ctx, key); err != nil {
				return false, operationError(OperationDelete, err)
			}
			if flight != nil {
				flight.superseded = true
			}
			return true, nil
		}
		record.ExpiresAt, record.StaleAt = expiresAt, staleAt
	}
	if err := record.Validate(); err != nil {
		return false, err
	}
	written, err = c.backend.Set(ctx, key, record, condition)
	if err != nil {
		return false, operationError(OperationSet, err)
	}
	if written && flight != nil {
		flight.superseded = true
	}
	return written, nil
}

// Delete removes a logical key. Deleting an absent key succeeds.
func (c *Cache[K, V]) Delete(ctx context.Context, logical K) (err error) {
	start := c.clock.Now()
	defer func() {
		outcome := OutcomeSuccess
		switch err {
		case nil:
		default:
			outcome = OutcomeError
		}
		notify(ctx, c.observer, Event{
			Operation: OperationDelete,
			Outcome:   outcome,
			Duration:  elapsed(start, c.clock.Now()),
		})
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.isClosed() {
		return ErrClosed
	}
	key, err := c.keys.Key(logical)
	if err != nil {
		return NewError(InvalidKeyError, OperationDelete, err)
	}
	flight := c.lockFlightMutation(key, true)
	if flight != nil {
		defer c.unlockFlightMutation(key, flight)
	}
	if _, err := c.backend.Delete(ctx, key); err != nil {
		return operationError(OperationDelete, err)
	}
	if flight != nil {
		flight.superseded = true
	}
	return nil
}

// lockFlightMutation reserves a reference before waiting: finishing the loader
// must not remove/recreate the key flight while an explicit mutation still owns it.
func (c *Cache[K, V]) lockFlightMutation(key string, lock bool) *loadFlight[V] {
	if !lock {
		return nil
	}
	c.loadMu.Lock()
	flight := c.flights[key]
	if flight != nil {
		flight.mutationUsers++
	}
	c.loadMu.Unlock()
	if flight != nil {
		flight.mutation.Lock()
	}
	return flight
}

func (c *Cache[K, V]) unlockFlightMutation(key string, flight *loadFlight[V]) {
	flight.mutation.Unlock()
	c.loadMu.Lock()
	flight.mutationUsers--
	if flight.finished && flight.mutationUsers == 0 && c.flights[key] == flight {
		delete(c.flights, key)
	}
	c.loadMu.Unlock()
}

func operationError(operation Operation, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return NewError(BackendError, operation, err)
}

func codecError(operation Operation, err error) error {
	kind := DecodeError
	switch {
	case errors.Is(err, ErrSchemaMismatch):
		kind = SchemaMismatchError
	case errors.Is(err, ErrValueTooLarge):
		kind = LimitError
	}
	return NewError(kind, operation, err)
}
