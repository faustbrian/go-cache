# Package map

[Documentation index](README.md) · [Repository entry](../README.md)

This map follows the current source modules, not a synchronized release.
Use the repository README for published installation versions; an unpublished
source major is not installable merely because its package appears here.
Internal packages and non-releasable fixtures are intentionally excluded.

## github.com/faustbrian/go-cache/v4

`cache` is a typed Go cache library with explicit hit, miss, stale, decode, and backend-failure semantics. It provides bounded cache-aside loading, versioned and hashed keys, strict codecs, a bounded memory backend, and native Redis and Valkey adapters. Valkey-backed caches can also publish a refresh only while a compatible distributed lease is still owned.

| Import path | Responsibility |
| --- | --- |
| [`github.com/faustbrian/go-cache/v4`](../doc.go) | Use for typed hit, miss, stale, decode, and backend-failure semantics with bounded cache-aside loading. |
| [`github.com/faustbrian/go-cache/v4/adapters/memory`](../adapters/memory/doc.go) | Use for a bounded process-local backend with explicit entry and byte limits. |
| [`github.com/faustbrian/go-cache/v4/adapters/otel`](../adapters/otel/doc.go) | Use for bounded OpenTelemetry cache observations with a caller-owned meter. |
| [`github.com/faustbrian/go-cache/v4/adapters/redis`](../adapters/redis/doc.go) | Use to adapt a caller-owned go-redis client to the cache backend contract. |
| [`github.com/faustbrian/go-cache/v4/adapters/service`](../adapters/service/service.go) | Use to compose explicit cache readiness and optional transferred shutdown with go-service. |
| [`github.com/faustbrian/go-cache/v4/adapters/slog`](../adapters/slog/doc.go) | Use for bounded structured cache observations with a caller-owned slog logger. |
| [`github.com/faustbrian/go-cache/v4/adapters/valkey`](../adapters/valkey/doc.go) | Use to adapt a caller-owned valkey-go client, including lease-guarded publication. |
| [`github.com/faustbrian/go-cache/v4/cachetest`](../cachetest/doc.go) | Use to verify custom backends against the shared cache conformance contract. |

Choose the next guide in the [documentation index](README.md)
or return to the [repository entry](../README.md) for installation and ownership.
