# Migration guide

## From v2 to v3

Version 3 adopts go-redis v9.22.0. The root and every cache subpackage move
from `github.com/faustbrian/go-cache/v2` to
`github.com/faustbrian/go-cache/v3`. Wait until the v3 tag resolves from the
public module proxy before changing production dependencies; no local
`replace` directive is needed after publication.

Both `adapters/redis.Config.Client` and `backend/redis.Config.Client` retain
`redis.UniversalClient`. Its upstream method set now includes `AutoPipeline`,
`AutoPipelineWithOptions`, `AsyncAutoPipeline`, and
`AsyncAutoPipelineWithOptions`, as well as additional command methods.
Native v9.22 clients satisfy the new contract. Custom clients must implement
all methods in the v9.22 interface; compiling against an older SDK is not
sufficient. Values read from either configuration field remain assignable
back to the SDK interface. The deprecated Redis facade remains supported.

Review application-owned client options: default read/write timeouts change
from three to five seconds, derived pool timeout from four to six seconds,
and retry delays from 8–512 milliseconds to 10 milliseconds–one second.
Review the SDK's TCP keepalive defaults as well. Set explicit options when
an existing deployment needs its earlier timeout or retry policy. Cache
wire records, key spaces, ownership, error classification, and supported
standalone Redis topology are unchanged. Cache never closes borrowed clients.

The published v2 module and its immutable API baseline remain available;
v3 does not silently replace that input contract in a v2 release. Separate
cache majors do not isolate their shared Redis SDK: Go's minimal version
selection chooses one `github.com/redis/go-redis/v9` version for the whole
build. Adding v3 can therefore select v9.22 for retained v2 imports too.
Check and update custom Redis providers used by both cache majors before
using them together; leaving a provider's cache imports on v2 does not protect
it from the expanded SDK method requirements.

Owned consumers such as `go-authorization` still expose v2 cache type
identities. A v3 backend uses v3 `Record` and other named types and is not an
automatic substitute for their v2 backend contract. Migrate those consumers
under their own compatibility policy, or keep their existing cache major;
this cache release does not change their public APIs.


## From v1 to v2

Version 2 is published at `v2.0.0`. Its historical migration is described
below; new v3 adoption must also follow the v2-to-v3 guidance above.

Change the module and every package import from
`github.com/faustbrian/go-cache` to `github.com/faustbrian/go-cache/v2`.
Version 2 intentionally changes stable v1 contracts: `Close` is bounded to
five seconds, and dependency causes are protected from public formatting,
`Error` fields, and `errors.As` traversal.

Replace direct `Error.Cause` access and concrete-cause `errors.As` branching
with `errors.Is` against cache sentinels or a known source error identity.
Capture detailed dependency diagnostics at a trusted backend, codec, encoder,
or loader boundary before returning the cause. Use `NewError` for custom
classified errors. Prefer `Shutdown(ctx)` when the application owns the
shutdown deadline, and never pass a nil context; nil is `ErrInvalidConfig`.

Constructors now reject typed-nil interface dependencies. Validate dependency
assembly during migration instead of relying on a later operation to panic.

Set `MaxFlights` for the total distinct-key loading budget (default 1024,
maximum 65536) and keep `MaxConcurrent` no greater than it. Handle
`ErrFlightLimit` as local saturation, not a source failure; stale refreshes
return the existing stale result together with this error.

Known direct consumers requiring import migration are `go-authorization` (`authcache` and `adapters/cache`) and
`go-service/integration/adoption`; `go-tenancy` also carries v1 imports in
analyzer fixtures. This repository does not edit those separately owned
consumers.

## From map or ad-hoc memory caches

Define a typed key encoder and value type, choose explicit byte/entry bounds,
and replace `(value, ok)` handling with `Result.State`. Preserve source errors
instead of treating every false result as a miss.

## From direct Redis/Valkey JSON

Keep native client creation in the application. Introduce a versioned key space
and codec, then deploy readers before writers if old and new formats must
coexist. Prefer a new key-space version for an incompatible cutover so no key
scan is required.

## From singleflight wrappers

Replace unbounded `singleflight.Group` use with `GetOrLoad`. Set measured
`MaxConcurrent`, `MaxFlights`, and `MaxWaitersPerKey` values, make loaders honor
their supplied context, and call `Close` during shutdown.

## From concrete dependency error inspection

Cache operations and network adapters no longer expose backend, loader,
key-encoder, or codec error text or concrete types through `errors.As`. Replace
concrete-cause branching with `errors.Is` against stable cache sentinels or a
known source error identity. If full dependency diagnostics are operationally
required, record them explicitly at that trusted dependency boundary before
returning the error; do not log credentials, logical keys, or payloads. Use
`NewError` when a custom cache component creates a classified protected error.

Service shutdown should prefer `Shutdown(ctx)` with the service deadline.
`Close` uses a five-second default and may return `ErrShutdownIncomplete` when
application loader, callback or backend code ignores cancellation. It does not
guarantee that already-admitted backend side effects have stopped; successful
`Shutdown` joins all active loads and their publication.

## To canonical adapter paths

New integrations use the canonical `adapters/*` paths. Existing integrations
can migrate imports independently; configuration, exported method behavior,
error classification, resource ownership, lifecycle order, redaction, and
backend wire semantics are unchanged.

| Deprecated path | Canonical replacement |
| --- | --- |
| `backend/memory` | `adapters/memory` |
| `backend/redis` | `adapters/redis` |
| `backend/valkey` | `adapters/valkey` |
| `cacheservice` | `adapters/service` |
| `observability/otel` | `adapters/otel` |
| `observability/slog` | `adapters/slog` |

The deprecated packages retain their own named Go type identities and delegate
to the canonical implementations. They remain supported for the longer of 180
days after the canonical successor release and two subsequently published
stable root-module minor releases.

## Release upgrades

Read every version in `CHANGELOG.md`. For changes to keys, codecs, TTLs, error
semantics, interfaces, or adapters, follow the compatibility note and deploy in
the stated order. Run your backend through `cachetest.RunBackendConformance`
with a deterministic outage hook after upgrading.
