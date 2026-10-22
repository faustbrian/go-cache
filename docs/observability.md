# Observability and cardinality

[Documentation index](README.md) · [Repository entry](../README.md)

Core events contain only operation, semantic outcome, duration, and size. They
have no key/value field by design. Custom observers must not add logical keys,
hashed backend keys, payloads, tenant IDs, or error strings as metric labels.
The bundled adapters emit no spans or trace attributes.

The OTel adapter records:

- `cache.operations` by operation and outcome;
- `cache.operation.duration` in milliseconds;
- `cache.value.size` for get/set payloads;
- `cache.memory.size` after memory eviction or expiration.

Only enumerated operation and outcome labels are accepted. Instrument creation
errors are returned from the constructor.
Import the adapter as `github.com/faustbrian/go-cache/v4/adapters/otel`.

The slog adapter records the same named fields, but unlike OTel it forwards
operation/outcome strings without enum validation. Direct callers must supply
safe bounded labels; configured logger/handler attributes can add data.
Configure its logger and level explicitly. Bundled cache events have no key/value/cause fields; that
is not a general sanitization guarantee for arbitrary labels or handlers.
Import the adapter as `github.com/faustbrian/go-cache/v4/adapters/slog`.

Observer failures and panics are ignored by cache notification, but synchronous
observers can delay completion/shutdown and have their own external effects.
Monitor exporter health separately. Alert on backend and loader error outcomes,
stale/negative ratios, waiter-limit rejections, latency, eviction rate, and
retained memory. A high hit rate alone does not prove freshness or correctness.

Compatible OTel observers may be constructed more than once from the same
meter; the SDK shares their instrument definitions and both contribute counts.
`BenchmarkObserver` reports the per-event allocation and latency cost. The
callback is synchronous, so custom exporters must keep their own work bounded.

Choose the next guide in the [documentation index](README.md)
or return to the [repository entry](../README.md) for installation and ownership.
