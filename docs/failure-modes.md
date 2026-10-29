# Failure modes and fail-open decisions

[Documentation index](README.md) · [Repository entry](../README.md)

`cache` preserves failures. Applications choose whether to degrade; the
library does not silently turn corruption or backend loss into a miss.

## Fail closed

Fail the operation when stale or missing data could violate authorization,
pricing, balances, idempotency, or other correctness constraints. Plain
cache-aside naturally fails closed on backend and loader errors.

## Fail open explicitly

For reconstructible caches, an application may catch `ErrBackend`, call its
source directly, and decide whether to return that value without caching it.
Keep this policy at the use case boundary so it is visible and testable. Do not
treat `ErrDecode` or `ErrSchemaMismatch` as a miss without alerting and a clear
remediation plan.

Use stale-while-revalidate for explicitly bounded stale serving when callers do
not need refresh errors. Use stale-if-error when callers need both the stale
value and failure signal.

## Common failures

- Backend unavailable: matches `ErrBackend`; retry according to the native
  client's policy or bypass explicitly.
- Corrupt record: matches `ErrBackend` and `ErrInvalidRecord`, or a decode/schema
  sentinel after structural validation.
- Source unavailable: matches `ErrLoader` and preserves the source error's
  `errors.Is` identity without exposing its concrete diagnostic.
- Caller timeout: matches `context.DeadlineExceeded` directly.
- Load timeout: matches `ErrLoader` and `ErrLoadTimeout` after
  `LoadPolicy.Timeout`. It can also match `context.DeadlineExceeded` when the
  loader returns its context error, so check `ErrLoadTimeout` first.
- Capacity or size limit: reject before backend retention; encoding and trusted
  callback allocations can occur before a wire-size rejection.
- Waiter saturation: return `ErrWaiterLimit`; shed load or increase a measured
  bound.
- Shutdown: active loaders receive cancellation; new work returns `ErrClosed`;
  bounded cleanup expiration also matches `ErrShutdownIncomplete`.

Choose the next guide in the [documentation index](README.md)
or return to the [repository entry](../README.md) for installation and ownership.
