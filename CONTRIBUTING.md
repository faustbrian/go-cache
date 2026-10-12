# Contributing

## Before Editing

1. Read the README, [`AGENTS.md`](AGENTS.md) and the affected API contract.
2. Start with `go test ./...` or a focused package/test selector.
3. Inspect direct consumers when changing a public or composition contract.
4. Preserve unrelated work and generated/corpus provenance.

An ordinary fix stays in this repository. It does not require sibling checkouts,
Tools edits, ecosystem catalog updates or agent-goal ledgers. Change manifests
only when their owned facts change, not merely because source changed.

## Changes

Keep commits focused and conventional. Update changelogs for material public
behavior, compatibility or adoption changes, not CI-only administration.
Public API changes require compatibility
evidence and documentation. Specification behavior requires a decision record,
fixture coverage, and interoperability evidence.

New direct dependencies and dependency updates must follow the
[dependency governance policy](AGENTS.md#dependencies-and-supply-chain). Package-local
update bots are forbidden; the root policy owns every module and action update.

Specification-backed changes must follow the
[specification governance contract](AGENTS.md#design), update
the affected stable decision entries, and complete the Specification Decisions
section of the pull request template. An unresolved interpretation or stale
source pin is release-blocking; peer behavior cannot silently select policy.

Mutation is optional developer-local or selected scheduled assurance, not a PR
prerequisite. Investigate viable survivors and add focused regressions for real
defects; moving a campaign off PRs does not waive a confirmed correctness or
security defect. Equivalent/invalid mutants retain reviewed narrow dispositions.

Do not add package-local workflows, permanent replacements, machine-specific
paths, bypass flags, broad mutation exclusions, or aggregate quality metrics
that hide a failing package.

## Verification

Use the checksum-pinned public Tools release declared in `.golib.yaml`.
Download its released `golib` binary rather than building the verifier from source.

The repository contract is pinned in [`.golib.yaml`](.golib.yaml). The root
Makefile is intentionally a thin local wrapper around the released `golib`
CLI; package-specific checks belong in typed configuration operations or
`verification/package.mk`, not copied `.golib` tooling.

For ordinary source work, untagged `go test ./...` runs deterministic tests,
fuzz seeds and saved corpus regressions without enabling the service integration
tests. With the qualified verifier selected by repository policy, use:

```bash
make inventory
make check
make ci
```

`check` and `ci` use bounded local checks, including the named race assertions
in `.golib.yaml` for loader coalescing, caller-cancellation isolation, queued-load
cancellation, caller-bounded shutdown and prevention of late publication.
The focused race operation runs once with a two-minute timeout, not a broad
stress campaign. CI consumes the checksum-pinned public Tools binary without
an ordinary verifier source build.

The root manifest still declares Redis and Valkey services and `integration`
build/test tags. Make/verifier gates therefore remain service-backed and require
the corresponding Docker prerequisites; changing their assurance route does not
remove that integration contract. Untagged direct Go tests and a focused root
race selector are distinct from those manifest routes. See
[backend integration guidance](docs/backends.md#supported-integration-matrix)
for the supported service versions and backend assertions.

Documentation changes need relevant structure, links and examples, not unrelated
runtime work. Public API, concurrency and backend/lease boundaries retain their
affected compatibility, race or real-service integration checks.

For active exploration, select one fuzz target and a small budget; long campaigns
belong in scheduled assurance. Optional local mutation selects one package with
the qualified verifier. These examples are not all-change requirements; the
mutation command retains manifest-selected service prerequisites:

```bash
GOMAXPROCS=2 go test -run '^$' -fuzz '^FuzzKeySpace$' -fuzztime=2s -parallel=1 -timeout=2m .
golib mutation --module . --package .
```

`make check-full` retains explicit full assurance and its service contracts;
relevant release qualification uses `golib release dry-run --module .`.
Select these for their material risk or release boundary, not every commit.
Report unavailable/failed checks honestly; a fast local pass is not
public-release or hosted-CI qualification.

## Adding A Module

Follow [repository structure policy](AGENTS.md#repository-structure). New modules
require an explicit purpose, ownership boundary, dependency review, package
catalog entry, full quality gates, documentation, changelog, license, security
policy, compatibility plan, and release dry-run.
