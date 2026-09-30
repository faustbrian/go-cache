# Changelog

All notable changes are documented here. The project follows Semantic
Versioning and keeps an Unreleased section at the top.

## [Unreleased]

## [2.0.0] - 2026-09-30

### Added

- Add canonical memory, Redis, Valkey, service-lifecycle, OpenTelemetry, and
  slog integrations under `adapters/*` without changing their runtime
  contracts.
- Add `Shutdown(ctx)` for caller-bounded loader cleanup and publish a versioned
  repository threat model and risk register.

### Fixed

- Reject excess distinct-key foreground and stale-refresh flights before
  allocation or goroutine creation. `LoadPolicy.MaxFlights` defaults to 1024,
  includes queued and mutation-retained work, and is bounded to 65536;
  excess new keys return `ErrFlightLimit` without disclosing the key.
- Redact recovered loader panic values while retaining `ErrLoaderPanic`
  classification.
- Redact backend, loader, key-encoder, and codec diagnostics from public error
  text and `errors.As` while retaining stable `errors.Is` identities. Code that
  inspected concrete dependency errors must move diagnostics to the trusted
  dependency boundary.
- Bound `Close` so a cancellation-ignoring loader cannot block shutdown
  indefinitely.
- Keep completed load flights attached while an explicit mutation holds their
  publication lock, so a replacement loader cannot overwrite that mutation.

### Deprecated

- Deprecate the `backend/*`, `cacheservice`, and `observability/*` integration
  paths in favor of their `adapters/*` successors. Compatibility facades keep
  existing source, named type, error, ownership, lifecycle, redaction, and
  backend behavior during the documented migration interval.

### Changed

- Bound `MaxConcurrent` to 65536 and require it not to exceed `MaxFlights`.
  Existing-key coalescing and per-key waiter limits remain independent.
- Move these intentional stable-contract changes to the
  `github.com/faustbrian/go-cache/v2` module path. After v2 is published,
  consumers must update all root and subpackage imports; v1 remains on its
  existing behavior.
- Reject typed-nil backend, codec, clock, jitter, observer, key-encoder, meter,
  Redis and Valkey dependencies during construction instead of allowing later
  panics. `Shutdown(nil)` now returns `ErrInvalidConfig`.
- Remove public `Error.Cause`; use `errors.Is` for protected source identity and
  capture full dependency diagnostics only at a trusted dependency boundary.
- Recheck shutdown cancellation before load-publication admission. Successful
  shutdown joins all active loads; an incomplete shutdown can be followed by
  late publication from an already-admitted, cancellation-ignoring backend.

- Adopt the checksum-verified `go-library-tools` v1.4.0 CLI, schema-v2
  cohesion contract, and local `make cohesion` gate without changing cache
  runtime behavior.
- Pin reusable CI to the immutable v1.4.0 W14-enforcement workflow and enforce
  cohesion metadata in the repository's required CI contract.
- Resolve owned dependencies through their canonical public v1.0.0 module
  identities instead of bootstrap-shadowed archives.

- Replace copied repository tooling with the pinned `go-library-tools` v1.0.13
  contract while retaining package-owned policy and verification evidence.

### Documentation

- Publish the cache family's ownership, package-selection, lifecycle, backend,
  and supported-environment metadata, and link the README to the immutable
  v1.4.0 ecosystem index and family guidance.
- Align the documented minimum Go toolchain with the module manifest.

- Remove the dated release audit and replace the archived monorepo link with
  package-owned documentation.

## [1.0.0] - 2026-08-25

### Changed

- Upgrade `moby/go-archive` and `golang.org/x/crypto` to their current
  security-fixed releases and reconcile the resulting indirect dependency
  graph.

- Exclude intentional nested modules from root local-proxy archives so local,
  bootstrap, CI, and public module checksums describe the same source
  boundary.

- Track the pinned documentation-tool lockfile so clean CI checkouts install
  the exact validated cspell dependency.

- Reconcile standalone dependency checksums against deterministic current
  module archives so CI, local verification, and release consumers resolve
  identical content.

- Harden standalone documentation validation with deterministic spelling and
  link checks, package-specific documentation gates, and repository-local
  contributor guidance.

### Documentation

- Replace obsolete standalone-repository links and workflow claims with
  monorepo-canonical targets and current release guidance.

- Link the package README to package-owned documentation.

### Changed

- Publish the module from its standalone `github.com/faustbrian/go-cache` identity while preserving its documented API and behavior.
- Replace obsolete owned-module pseudo-version pins with the monorepo's local
  `v0.0.0` source-proxy coordinates; release tooling continues to emit exact
  `v1.0.0` dependency versions.
- Guard schema-prefix allocation arithmetic against the platform integer limit
  after enforcing the configured payload boundary.
- Remove unused CLI-related indirect dependencies from canonical module
  metadata.
- Pin owned sibling modules to exact resolvable main pseudo-versions so
  standalone and clean external consumers use immutable dependency content.

- OpenTelemetry API and metric SDK dependencies now use 1.44.x consistently
  after adding the service lifecycle adapter.
- Wait for Redis and Valkey readiness logs as well as listening sockets before
  running backend conformance tests.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Added

- A `cacheservice` lifecycle adapter for explicit cache and Valkey resources,
  opt-in startup validation and readiness, and shared or transferred shutdown
  ownership.
- Atomic Valkey `SetIfOwned` publication guarded by an active lease owner and
  fencing token, with fail-closed ownership errors and typed cache support.
- Atomic `SetNegativeIfOwned` publication for authoritative absence under the
  same active lease and fencing-token guarantee.
- Typed cache API with explicit hit, miss, stale, and negative results.
- Bounded cache-aside loading, cancellation, panic cleanup, negative caching,
  stale policies, and refresh jitter.
- Versioned hashed key spaces and strict versioned JSON codec.
- Bounded memory, native go-redis/v9, and native valkey-go backends.
- Shared backend conformance suite and Testcontainers integration matrix.
- Redacted semantic events with OpenTelemetry and slog adapters.
- Exact production coverage, race, fuzz, leak, safety, benchmark, docs, and
  release automation.
- Authenticated and certificate-verified TLS integration coverage for every
  supported Redis and Valkey version.
- Operation-model backend fuzzer, minimized corpus, recovery tests, duplicate
  OTel construction test, and observer allocation benchmark.
- Semantic truth table, backend matrix, ownership/threat model, findings
  report, operations guide, and release verdict.

### Fixed

- Reject versioned JSON payloads whose schema-prefix allocation would overflow
  the platform integer size.
- Require Redis protocol readiness and an actual `NOAUTH` response before
  authenticated backend assertions begin.
- Run fuzz smoke campaigns for a deterministic execution count so the Go fuzz
  harness cannot report its own duration deadline as an application failure.
- Preserve successful same-instance `Set`, conditional mutation, and `Delete`
  precedence over foreground loads and stale background refreshes.
- Reject recursive same-cache loading with `ErrRecursiveLoad` instead of
  waiting on the active flight.
- Use relative server expiry so an injected clock is not confused with the
  Redis or Valkey server wall clock.
- Apply a portable 1 ms minimum server TTL instead of allowing Valkey `PX 0`.
- Strip process-local monotonic readings from portable deadlines so memory and
  serialized backends use the same wall-clock interpretation.
- Reject negative-cache deadline overflow before accessing the backend.
- Treat expired memory records as absent during deletion, matching Redis and
  Valkey.
- Require backend conformance to prove that read, write, and delete outages
  remain errors rather than misses or rejected mutations.
- Keep backend conformance failure messages compatible with standard Go error
  style so strict static analysis remains clean for downstream test suites.

[Unreleased]: https://github.com/faustbrian/go-cache/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/faustbrian/go-cache/releases/tag/v1.0.0
