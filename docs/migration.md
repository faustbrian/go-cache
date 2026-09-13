# Migration guide

## From v1 to v2

Version 2 is planned but unpublished. Do not change production dependencies
until a `v2.0.0` tag resolves from the public module proxy without a local
`replace` directive.

Change the module and every package import from
`github.com/faustbrian/go-cache` to `github.com/faustbrian/go-cache/v2`.
Version 2 intentionally changes two stable v1 contracts: `Close` is bounded to
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

Release remains blocked until public v2 resolution and clean consumer checks
succeed without local replacements. Known direct consumers requiring import
migration are `go-authorization` (`authcache` and `adapters/cache`) and
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
`MaxConcurrent` and `MaxWaitersPerKey` values, make loaders honor their supplied
context, and call `Close` during shutdown.

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
application loader code ignores cancellation.

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
