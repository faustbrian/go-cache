# Source documentation audit

## Acceptance and owned boundary

This Tier A pass owns comments and directly contradicted maintained prose at
main `079a0aa90dac06a51e9c7727e67e8f7c3cdcfaa3`. Preserve runtime syntax,
literals, directives, dependencies, service fixtures and API baselines. Cache
consumers include typed reads/loaders, provider clients, observers, test
harnesses and service composition. Named risks are misleading ownership,
cancellation, callback, expiration and resource guarantees, not a request to
repair runtime behavior or requalify unchanged services.

## Reproducible before-write inventory

`git ls-files` and `golib source-docs inventory --json` identify one v3 root
module, 76 Go files (37 production/support and 39 tests), and 16 source
directories: root; six canonical adapters; three legacy backend facades; two
legacy observability facades; cacheservice; cachetest; internal/wire; and
internal/integrationtest. The initial inventory reports 106 missing public
field/interface/error-method comments. All maintained files, fuzz/benchmark/
Example helpers and integration variants are included in the source audit,
not just build-selected packages. Five files carry the integration build tag;
there are no generated Go, generator directives, embedded templates, vendored
Go, or platform-specific source variants. Existing Lua strings and security
suppression directives are executable or analyzer inputs and remain exact.

The native API mode is apidiff with api/v3-baseline.txt; v1/v2 baselines are
historical. No full-prose snapshot is configured. Native docs in
verification/package.mk run Examples and package documentation. Service-backed
Make checks, targeted race assertions and full campaigns belong to their actual
runtime/release boundaries and are not this comment-only pass. The repository
has a policy decision guide but no typed standards decision register.

## Contract tracing

- Key encoding hashes borrowed logical bytes after a callback; the final key
  limit does not bound logical input or callback allocation. Prefixes are
  visible, not secret. Config copies value settings but retains collaborators.
  New requires a preconstructed KeySpace without validating a zero Keys field.
- Backends own retained wire payloads, not generic application V. A published
  found load returns the loader's original V to every waiter; pointers/maps/slices may
  be shared. A double-check hit/negative result or superseding mutation may
  instead return decoded current state. Codec/key/jitter/clock dependencies must
  support concurrent calls.
- Load flights are keyed by encoded backend key and use a cache-owned
  Background-derived context, not the first waiter's context. All canceled
  waiters can leave a running flight. Budgets count queued and retained work;
  loader slots are separate from flight admission. Supplied loader-context
  recursion detection rejects the same cache even for another key.
- Load/observer panics are isolated in their own wrappers; codec/key/clock/
  jitter/backend/guard callbacks are not. Notification is synchronous and may
  block publication/flight completion. Memory calls Clock under its mutex;
  load publication calls collaborators under a per-flight mutation mutex.
  Reentrant callbacks must not acquire those same protected boundaries.
- Shutdown cancels/joins loads only, not every already-started foreground
  operation or external client. Cancellation-ignoring admitted publication can
  complete after incomplete shutdown. A successful join includes observer work.
- Freshness is strict Before(ExpiresAt); hard expiry is StaleAt. Conditions
  consider stale retained records live. Sliding writes test presence, not
  record-version equality. Negative records bypass codec/refresh until hard
  expiry. Stale-if-error returns both the old result and refresh error.
- Memory counts key+payload bytes, not heap overhead; expiration is lazy and
  Stats does not sweep. A structurally valid already-expired Set can be stored
  then disappear on lookup; network Set rejects non-positive remaining TTL.
  Network scripts/conditions use server existence and relative millisecond TTL;
  wall-clock skew and native-client retries/latency remain caller/provider risks.
  Only Valkey supplies atomic lease-hash publication; no adapter acquires a lease.
- JSON Encode allocates through json.Marshal before size rejection. Decode
  compares its configured version without independently rejecting zero and
  uses encoding/json field/null/custom-method semantics. Neither a byte cap
  nor trusted callbacks creates a total heap/CPU deadline guarantee.
- Observer event fields exclude keys/values, but extensible strings are not a
  redaction engine. OTel validates enums; slog forwards labels. Service callbacks
  use supplied contexts without retries/panic recovery; shutdown ownership is
  opt-in and attempted once even after error, with causes exposed by Unwrap.

## Dated review caveat

The Version 2 threat model retains its 2026-09-13/09-30 review identity and
verdict. GC-SEC-001 broadly mentions backend/key/codec panic mitigation, whereas
source recovery is limited to callLoader and notify. GC-SEC-010 already treats
arbitrary callbacks as trusted and unsandboxed. This pass does not fabricate a
new security review or alter that historical verdict; current source/concurrency
guidance states the narrower proven panic boundary.

## Smallest sufficient proof

Format owned Go files; compare AST/significant tokens/literals/directives across
all 76 tracked files; render every source package including the tagged helper;
run the native Example/doc target without services. Use current structural,
local-doc and offline specification applicability checks, pinned changed/staged
secret scan and one ordinary complete-diff usefulness/truth inspection. Small
disposable source-contract observations may establish the named zero-KeySpace,
zero codec-version or past-memory-deadline limits. No service, race/leak, active
fuzz, performance, release or broad unchanged-runtime campaign is justified.
Keep exact task-owned Go caches/resources disposable, distinguish executed and
reused evidence, and leave remote main/CI delivery to the coordinator.

## Outcomes and retained boundaries

The pinned Go 1.27.2 pass preserved AST, significant tokens (including implicit
semicolons), literals and directives across all 76 Go files. All 16 package
renders were inspected: 15 native full GoDoc renders and stdlib go/doc extraction
of the integration-only TLS helper. Native go doc did not select that helper
through GOFLAGS build tags, so its source render is not an integration execution.
The native docs target passed both existing Examples and ordinary package docs;
its normal root test harness includes its existing goroutine cleanup check.
No additional leak, service, race, fuzz or performance campaign was run.

Three bounded observations established current limits without changing tests:
New accepted a zero Keys configuration, version-zero JSON Decode accepted its
zero prefix while Encode rejected it, and a past-hard-deadline memory Set
succeeded followed by a miss. Structural source checks now report no findings;
local docs links pass and the native specification check has zero applicable
module/decision records. Native apidiff baselines, dependency files, Lua scripts,
corpus and dated review inputs remain unchanged. Earlier renders and observations
were reused only while their source, dependencies and toolchain remained exact.

All task-owned Go caches, observation helpers and render output were removed.
The complete comment/prose diff receives ordinary Tier A inspection and a pinned
changed-content secret scan before the detached commit. Remote delivery and
exact-source hosted CI are coordinator-owned, not established by local proof.
