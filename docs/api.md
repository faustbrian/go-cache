# API reference

[Documentation index](README.md) · [Repository entry](../README.md)

The Go package documentation is the source of truth for signatures. This guide
defines the behavioral contract that those signatures carry.

## Construction

`New(Config[K, V])` requires a backend, codec, clock, positive value limit and
valid positive TTL. Supply Keys from NewKeySpace: New does not validate a
zero-value key space, which has no usable encoder. Zero loader and batch limits
select bounded defaults. Negative limits, contradictory stale policies, and jitter at or above
the positive TTL are rejected.

Construction errors match `ErrInvalidConfig`, `ErrInvalidTTL`, or
`ErrInvalidPolicy`. Required interface dependencies and configured optional
interfaces reject both nil and typed-nil values.

## Results

`Result[V]` contains:

- `State`: `Hit`, `Miss`, or `Stale`;
- `Value`: decoded value for hits and stale results;
- `Negative`: true when a miss came from negative caching.

`Get` returns `Miss, nil` for absence. It never converts backend, record,
schema, or decode failures into misses. A record past `ExpiresAt` but before
`StaleAt` returns `Stale`. A record at or beyond `StaleAt` is deleted and
returns `Miss` unless cleanup fails.

Stored zero values and nil-like values remain hits. Empty raw bytes are not a
tombstone and fail schema validation. Negative records are the only built-in
absence marker.

## Mutations

- `Set` writes unconditionally.
- `SetWithExpiry` writes unconditionally with a per-entry `Expiry`; see
  [per-entry expiry](#per-entry-expiry).
- `SetIfOwned` atomically writes only while an `OwnershipGuard` matches an
  active owner in an `OwnershipBackend`.
- `SetNegativeIfOwned` atomically writes an explicit negative record under the
  same ownership check and requires a positive configured `NegativeTTL`.
- `Add` atomically writes only if no live record exists.
- `Replace` atomically writes only if a live record exists.
- `Delete` is idempotent at the semantic API.

Conditional false results are not errors. Backends must implement the
conditions atomically. Live refers to provider retention (including stale
records), with server expiry precision/clock differences described in the
backend guide. Sliding updates use IfPresent only, not record-version CAS.

Protected publication fails with `ErrOwnershipUnsupported` when the configured
backend lacks the optional capability. A missing, expired, or superseded guard
fails with `ErrOwnershipLost` and is also classified as `ErrBackend` by the
semantic cache. A successful protected write has the same observation and
same-instance load-precedence behavior as `Set`; positive records use the
normal TTL policy and negative records use `NegativeTTL`. The built-in
implementation is the Valkey adapter; the cache package does not acquire,
renew, or release leases.

## Cache-aside loading

`GetOrLoad` first performs `Get`, then joins or creates one in-process flight
per backend key. Flights receive a cache-owned Background-derived context,
never a caller's cancellation or deadline. `LoadPolicy.InheritValues` exposes
the admitting caller's context values to the loader (joiners' values are not
visible), and `LoadPolicy.Timeout` bounds each loader call; a loader error after
that bound matches `ErrLoader` and `ErrLoadTimeout`. See
[concurrency](concurrency.md) for refresh, shutdown and recursion details. A published found load shares the
loader's original V among waiters without deep copies. Double checks or
superseding mutations can instead return the current decoded state. The loader
returns `LoadResult[V]{Found:false}` for an authoritative absence. A loader error is not absence and matches `ErrLoader`.

Flights are bounded globally by `MaxFlights` (default 1024, maximum 65536);
`MaxConcurrent` separately bounds executing loaders and cannot exceed that
flight budget. New distinct-key work beyond the budget returns `ErrFlightLimit`
before allocation or spawning. Existing-key followers are bounded by
`MaxWaitersPerKey`. Caller cancellation detaches that caller; it does not cancel
a load still needed by other callers. `Close` cancels the shared load context,
waits up to five seconds for flight cleanup, and makes subsequent operations
return `ErrClosed`. `Shutdown(ctx)` uses the caller's bound. Either returns
`ErrShutdownIncomplete` if loader cleanup remains when its bound ends.
An incomplete shutdown may be followed by late backend publication from
already-admitted work if trusted callbacks or the backend ignore cancellation.
A successful `Shutdown` joins all active loads and their publication.
Passing nil to `Shutdown` returns `ErrInvalidConfig` without changing cache
state.

Successful same-instance mutations supersede an active load for that key, so a
load cannot overwrite a `Set` or resurrect a `Delete`. Loaders must use their
supplied context; recursive same-cache calls with that context return
`ErrRecursiveLoad`. These guarantees are process-local.

## Per-entry expiry

`Expiry` lets one entry carry its own freshness deadline. The zero value keeps
the cache policy, so fixed-TTL caches behave exactly as before. Loaders return
it in `LoadResult.Expiry`, and `SetWithExpiry` accepts it directly.

| Expiry | Positive entry | Negative entry |
| --- | --- | --- |
| zero | `TTL` minus any `RefreshJitter`, then `StaleFor` | stored only when `NegativeTTL` is positive |
| `ExpireAt(t)` | fresh before `t`, then `StaleFor`; no jitter | ends at `t`, even when `NegativeTTL` is zero |
| `NeverExpire()` | never stale; `StaleFor` ignored | never expires |

- A non-zero `Expiry` replaces the policy TTL, jitter and `NegativeTTL` for
  that entry. `StaleFor` still follows an absolute deadline, so
  `StaleWhileRevalidate` and `StaleIfError` apply after `t`.
- A deadline at or before the cache clock behaves like an already expired
  write: the key is deleted and nothing is stored. A loader's waiters still
  receive the loaded result.
- A zero or non-portable `ExpireAt` time, or one whose stale window passes the
  latest portable instant, fails with `ErrInvalidTTL`.
- A non-zero `Expiry` fails with `ErrInvalidPolicy` when `TTLPolicy.Sliding` is
  set, because a sliding hit would replace the explicit deadline.
- `NeverExpire` stores the latest portable deadline. Remote backends receive
  a server TTL of roughly two centuries, not a persistent key. Memory-backend
  capacity eviction still applies to these entries.
- Only loaders and `SetWithExpiry` accept an `Expiry`. `Add`, `Replace`,
  `SetMany`, `SetIfOwned` and `SetNegativeIfOwned` keep the policy deadlines.
- The cache compares a deadline with its own clock. A remote adapter that
  computes the server TTL from a later clock read can still reject a deadline
  that passes in between with `ErrInvalidTTL`, classified as `ErrBackend`;
  the previous value then remains until its own deadline.

## Bulk operations

`GetMany`, `SetMany`, and `DeleteMany` preserve input order. A request beyond
`MaxBatch` fails before backend access with `ErrBatchTooLarge`. Individual
operation failures are stored beside each key and do not stop the remaining
items.

## Keys and codecs

`NewKeySpace` accepts lowercase ASCII namespace/name parts containing digits,
hyphen, or underscore, a nonzero version, a deterministic encoder, and a
positive backend-key limit. Backend keys contain the versioned prefix and a
SHA-256 digest, never raw logical bytes.

`JSONCodec` prefixes strict JSON with a nonzero one-byte version. It rejects
unknown fields, malformed or trailing JSON, wrong versions, and size excesses.
Custom codecs implement `Codec[V]` and must provide the same deterministic,
bounded behavior expected by the application.

## Errors

Use `errors.Is`, not string matching. Important sentinels are:

- `ErrBackend`, `ErrDecode`, and `ErrSchemaMismatch`;
- `ErrInvalidKey`, `ErrKeyTooLarge`, and `ErrValueTooLarge`;
- `ErrInvalidTTL`, `ErrInvalidPolicy`, `ErrInvalidConfig`, and
  `ErrInvalidRecord`;
- `ErrLoader`, `ErrLoaderPanic`, `ErrLoadTimeout`, `ErrRecursiveLoad`,
  `ErrWaiterLimit`, and `ErrFlightLimit`;
- `ErrCapacity`, `ErrBatchTooLarge`, `ErrClosed`, and
  `ErrShutdownIncomplete`;
- `ErrOwnershipLost` and `ErrOwnershipUnsupported`.

`Error` exposes `Kind` and `Operation`; its protected source cause is not a
public field.
Public formatting and `errors.As` do not expose concrete backend, loader,
key-encoder, or codec diagnostics. `errors.Is` still recognizes their identity
and the stable cache sentinel. Context cancellation and deadlines are returned
directly. Construct classified errors with `NewError` so dependency causes use
the same protection.

## Observers

`Observer.Observe` receives redacted `Event` values with operation, outcome,
duration, and size only. Observer errors and panics do not replace operation
results, but synchronous callbacks can block calls and loader cleanup.
Implementations must keep labels bounded and must not infer or attach
keys or values.

## Backend conformance

Third-party backends should invoke:

```go
cachetest.RunBackendConformance(t, cachetest.BackendHarness{
    Backend: backend,
    MakeUnavailable: func(t *testing.T) {
        // Deterministically stop or disconnect the backend.
    },
})
```

The suite checks misses, cloned payloads, positive and negative round trips,
atomic conditions, idempotent deletion, cancellation, invalid records and
conditions, deadline portability, and that outages remain errors for reads,
writes, and deletes. The outage hook runs last. Backends may expose additional
native operations outside the semantic API. The optional `OwnershipBackend`
contract is adapter-specific and is verified separately because ordinary cache
backends are not required to provide distributed ownership.

## Service lifecycle adapter

`adapters/service.New[R]` retains the exact concrete cache or Valkey resource
while adapting explicit startup, readiness, and shutdown callbacks to
`service`.
Readiness is opt-in and re-evaluates the resource on every probe, allowing
recovery after transient failures.

A nil `Shutdown` means the resource is shared and is never closed by the
adapter. A configured `Shutdown` transfers ownership, runs after
later-declared components drain, cleans up failed startup, and executes once
across repeated shutdown calls. Callers and service contexts bound operations;
the adapter adds no retry or cache policy and preserves existing error
classification.

Choose the next guide in the [documentation index](README.md)
or return to the [repository entry](../README.md) for installation and ownership.
