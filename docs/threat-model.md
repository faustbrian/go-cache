# Threat model and risk register

Version: 2.0
Reviewed: 2026-09-13
Flight-admission review: 2026-09-30
Shutdown-publication and adapter validation review: 2026-09-30
Owner: go-cache maintainers

## Scope and objectives

This model covers the public cache API, key and record encoding, cache-aside
loaders, observers, memory/Redis/Valkey adapters, and the optional Valkey
ownership protocol. The cache stores reconstructible data and is not a
durability, authentication, authorization, encryption, or distributed-lock
boundary.

The security objectives are to preserve tenant/key separation, treat stored
bytes as untrusted, avoid disclosing keys or values through package-owned
errors and telemetry, bound package-controlled resource use, reject unsafe
ownership publication, and terminate lifecycle operations within an explicit
bound.

## Assets and sensitivity

- Logical keys and cached values may contain confidential application data.
- Namespace/name prefixes and backend credentials are deployment-sensitive;
  prefixes are visible and must not contain secrets or personal identifiers.
- Freshness and invalidation state may be integrity-sensitive. Authorization,
  revocation, balances, and pricing must not use stale-serving policies.
- Lease owner IDs and fencing tokens are integrity controls, not secrets, but
  disclosure can assist interference with a poorly isolated backend.
- Errors, logs, traces, and metrics cross observability boundaries and must not
  acquire keys, values, credentials, or callback panic payloads.

## Actors, assumptions, and trust boundaries

The application, its configured codecs/loaders/observers, and native backend
clients are trusted to follow their documented contracts. Backend contents,
network peers, caller-supplied logical keys, and decoded wire records are
untrusted. An attacker may write malformed backend records, trigger high
concurrency or cancellation, influence callback inputs, or observe application
errors and telemetry. Host compromise, Go runtime compromise, native-client
transport configuration, and source-of-truth authorization are outside this
package's control.

Trust boundaries:

1. **Caller to semantic cache:** logical keys, values, contexts, batch sizes,
   loaders, codecs, clocks, jitter sources, and observers enter package code.
2. **Cache to backend:** versioned hashed keys and encoded records cross to an
   untrusted memory or network store; raw responses return for strict parsing.
3. **Application to native client/network:** credentials, TLS, ACLs, topology,
   retries, and connection bounds remain application-owned.
4. **Loader to source of truth:** loader results and failures cross from
   application code; loaders must honor context and bound their own I/O.
5. **Cache to observability:** package events and errors may reach less-trusted
   operators, exporters, and log stores.
6. **Lease owner to Valkey:** owner/token/storage-key triples are checked in one
   server-side script before guarded cache publication.

## Risk register

Severity reflects package impact with documented deployment assumptions.
`Accepted` risks remain owned and have an explicit review trigger.

| ID | Threat and boundary | Severity | Status | Mitigation or acceptance | Owner and review trigger |
| --- | --- | --- | --- | --- | --- |
| GC-SEC-001 | A backend, loader, key encoder, or codec returns or panics with a secret-bearing diagnostic which crosses the error/observability boundary. | Medium | Mitigated | Public error text and `errors.As` expose no concrete dependency cause; identity remains available through `errors.Is`, and loader panic recovery exposes only `ErrLoaderPanic`. Focused tests cover every callback class and panic redaction. | Maintainers; review error construction, callback, adapter, or panic handling changes. |
| GC-SEC-002 | A loader ignores cancellation and blocks lifecycle completion indefinitely. | Medium | Mitigated | `Close` uses a five-second bound; `Shutdown(ctx)` accepts the service deadline and returns `ErrShutdownIncomplete`. The cache is closed before waiting. `TestCloseReturnsWhenLoaderIgnoresCancellation` covers the bound. | Maintainers; review loader lifecycle, flight tracking, or close semantics. |
| GC-SEC-003 | Malformed or oversized backend bytes cause unsafe decoding or memory growth. | High | Mitigated | Backends and codecs enforce wire/value bounds, versions, strict decoding, and typed schema errors; conformance and codec tests cover malformed records. | Maintainers; review record format, codec, or allocation changes. |
| GC-SEC-004 | Raw logical keys disclose tenant data in backend keys or telemetry. | High | Mitigated | Key spaces hash encoded logical keys with SHA-256; built-in events contain no key/value fields. Prefixes must be non-sensitive. | Maintainers and integrators; review key format or event fields. |
| GC-SEC-005 | Cross-tenant collisions or namespace reuse expose another tenant's value. | High | Mitigated | Applications must use unique tenant/semantic logical keys and versioned namespace/name prefixes; strict codecs reject incompatible records. | Integrators; review tenant model, encoder, or namespace changes. |
| GC-SEC-006 | Stale security-sensitive state authorizes an operation after revocation. | High | Accepted | Stale serving is prohibited for authorization, revocation, balance, and pricing decisions. The source of truth remains authoritative. | Integrators; review when cached data begins influencing security or money decisions. |
| GC-SEC-007 | Backend interception, credential theft, or excessive permissions expose or mutate cache records. | High | Accepted | Applications own verified TLS, authentication, network policy, native-client timeouts, and least-privilege command/key ACLs documented in `SECURITY.md`. | Integrators; review backend topology, credentials, or ACL changes. |
| GC-SEC-008 | A stale lease owner publishes after ownership transfer. | High | Mitigated | Valkey guarded publication atomically validates storage key, owner, and fencing token. It protects cache publication only, not durable side effects. | Maintainers; review Lua scripts or ownership record formats. |
| GC-SEC-009 | Distinct-key request floods retain unbounded queued goroutines even when executing loaders are limited. | High | Mitigated | `MaxFlights` rejects new foreground/background flights before allocation or spawning, including canceled-caller and mutation-reserved work. `MaxConcurrent` separately bounds loader execution; both have hard ceilings. Focused tests cover cancellation churn, stale refresh, replacement admission and mutation reservations. Native-client pool/retry bounds remain deployment-owned. | Maintainers and integrators; review new concurrency or allocation paths and production limits. |
| GC-SEC-010 | A malicious custom codec, loader, observer, clock, or jitter source violates confidentiality or availability. | Medium | Accepted | Callbacks are application-trusted. Dependency errors and loader panics are redacted, observer panics/errors are isolated, and loaders receive cancellation. Arbitrary callback code can still retain data, block, or mutate application-owned state and cannot be sandboxed by this library. | Integrators; review callback provenance or third-party implementations. |
| GC-SEC-011 | An uncooperative loader continues application-owned I/O after bounded shutdown returns. | Medium | Accepted | The cache cancels first, rejects new work, and reports `ErrShutdownIncomplete`; the application must make loaders context-aware and separately bound I/O. Killing arbitrary goroutines is unsafe in Go. | Integrators; review incomplete-shutdown signals or loader dependencies. |
| GC-SEC-012 | Vulnerable dependencies or unsafe runtime escape weaken package isolation. | Medium | Mitigated | Checks cover vulnerability scanning and GO-SAFETY-1 (`unsafe`, cgo, and `go:linkname` absent); immutable module checksums bind dependencies. | Maintainers; review dependency or toolchain updates and vulnerability reports. |
| GC-SEC-013 | Same-process load cleanup detaches a flight while an explicit mutation is still serialized against it, allowing a replacement loader to overwrite the mutation. | High | Mitigated | Explicit mutators reserve the mapped flight before waiting on its publication lock; completed flights remain attached until all reservations release. A deterministic concurrency regression covers cleanup and replacement-load admission. | Maintainers; review flight cleanup, mutation locking, or supersession behavior. |
| GC-SEC-014 | An admitted load publication calls or completes a cancellation-ignoring backend after incomplete shutdown returns. | Medium | Accepted | Cancellation is checked before publication, not atomically with an arbitrary backend commit. Waiting on trusted backend I/O under the shutdown lock would defeat the shutdown deadline. Cache data is reconstructible and cannot authorize security-sensitive decisions; therefore this conditional trusted-dependency residual is Medium. Successful shutdown joins all active loads. Integrators must use context-aware, separately bounded backend I/O and treat `ErrShutdownIncomplete` as ongoing work. Deterministic positive/negative post-check callback barriers prove the late-write residual and bounded incomplete shutdown. | Maintainers own truthful lifecycle contracts; integrators own backend I/O and incomplete-shutdown handling. Review on backend/callback/lifecycle changes or any late-publication incident; reassess before each major release. |

There are no open Critical or High package-owned risks in this version. Any new
Critical or High finding blocks release until mitigated or explicitly accepted
by maintainers with rationale, compensating controls, and a dated review
trigger. Medium findings must likewise be mitigated or recorded above as an
owned residual risk.

## Verification and maintenance

Security-relevant changes must update this document when they alter an asset,
boundary, assumption, mitigation, owner, or residual risk. Review it before a
major release, after a security incident, when adding a backend or callback
boundary, and when transport, ownership, keying, decoding, telemetry, or
lifecycle contracts change. Private reports follow `SECURITY.md`.
