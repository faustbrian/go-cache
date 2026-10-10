# Compatibility and SemVer policy

The module requires Go 1.27.0 or newer. CI uses the patched Go 1.27.2
toolchain; focused compatibility checks also cover the Go 1.27.0 minimum.

Before `v1.0.0`, minor releases may change public APIs with changelog and
migration notes. Starting with `v1`, SemVer applies to:

- exported interfaces, types, functions, methods, constants, and errors;
- `errors.Is` identities and hit/miss/stale semantics;
- key prefix, hashing, encoding, and version behavior;
- TTL, stale, sliding, negative, jitter, loading, and shutdown behavior;
- built-in codec and wire-envelope compatibility;
- backend conditional, expiration, size, and conformance behavior;
- ownership guard shape, protected-write errors, and atomic Valkey comparison;
- portable deadlines and their wall-clock interpretation;
- metric names, units, and label sets.

The immutable released-v1 and released-v2 API snapshots are retained at
`api/v1-baseline.txt` and `api/v2-baseline.txt`. The active v3 source is checked
against `api/v3-baseline.txt`; each major has an independent baseline because
its documented incompatible contracts have a distinct module identity.
Version 3 retains the Redis configuration's SDK `UniversalClient` type while
adopting its expanded v9.22 method set. This changes the accepted custom-client
contract and therefore requires a major version, not merely because the SDK
version changed. See the v2-to-v3 migration guide.

Adding a method to an exported interface is breaking. Changing a miss into an
error (or an error into a miss), changing key output, accepting previously
rejected ambiguous policy, or changing stored bytes incompatibly requires a
major release unless gated behind a new explicit API/version.

Supported backend integration versions for the initial release are Redis 7.2,
7.4, and 8.0, and Valkey 9.0. Older server versions may work but are not covered
by the release matrix.

The supported topology is standalone with optional password authentication and
verified TLS. Cluster, Sentinel, automatic failover, redirects, and replica
reads are outside the initial compatibility promise even when the supplied
native client exposes them. Adding a tested topology expands the compatibility
matrix and requires explicit changelog and operations guidance.
