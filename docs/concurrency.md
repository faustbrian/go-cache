# Stampede control and concurrency model

[Documentation index](README.md) · [Repository entry](../README.md)

`GetOrLoad` coalesces work by the fully encoded backend key. Coalescing is
process-local; it is not a distributed lock and does not promise exactly-once
loading across processes.

The first miss creates a flight. Before entering the loader, the flight reads
the cache again so a concurrent writer can satisfy it. Independent flights
share a global semaphore capped by `MaxConcurrent`. Callers joining one flight
are capped by `MaxWaitersPerKey`.

Distinct-key flight admission is separately bounded by `MaxFlights` (default
1024, maximum 65536). The budget includes queued loader goroutines and finished
flights retained by explicit mutations. Excess new keys return `ErrFlightLimit`
before flight allocation or spawning; existing-key followers can still join
within `MaxWaitersPerKey`. `MaxConcurrent` cannot exceed `MaxFlights` or 65536.
Caller cancellation detaches a waiter but does not release a live flight's
budget; actual flight completion and mutation cleanup do. Stale refreshes use
the same budget and return the existing stale value with the rejection error.

Each caller waits with its own context. Cancellation detaches only that waiter.
The loader receives a cache-owned context so one impatient caller cannot cancel
work needed by others. Even the last waiter's cancellation does not stop the
flight. Caller context values and deadlines are not copied into load context.
`Close` cancels that shared context, prevents new
flights, waits up to five seconds for active goroutines, and is idempotent.
`Shutdown` accepts a caller-owned context when another deadline is required.

Loader panics are recovered, classified as `ErrLoaderPanic`, and cannot poison
the flight map. Source errors match `ErrLoader`; they are never converted into
negative entries. A `Found:false` result may create a bounded negative record.
Only the loader wrapper and observer notification recover panics; clocks, codecs,
key encoders, jitter, guards and backend callbacks remain trusted.

The dated Version 2 `GC-SEC-001` risk-register assessment is not a blanket
panic-containment guarantee for every dependency. Returned key/codec errors and
non-cancellation backend errors pass through the protected error boundary.
Cancellation/deadline-shaped backend errors retain the documented direct-return
contract; see [API errors](api.md#errors). A panic outside the two recovery
wrappers does not become a protected error. Use bounded, concurrency-safe,
panic-free implementations outside those wrappers. A foreground callback panic
can propagate to its caller; one escaping a cache-owned load goroutine can
terminate the process. Caller-side recovery is not containment of that worker.
This is the trusted-callback boundary already recorded by `GC-SEC-010`, not a
new review verdict or a sandbox for arbitrary callback code. Historical review
dates and the original risk-register verdict remain unchanged.

Published found-load results share the loader's original V; double checks and
superseding mutations may instead return decoded current state. Mutable maps,
slices and pointers require application synchronization/immutability. The
codec controls ownership of separately decoded reads; the generic cache does
not deep-copy arbitrary values. Publication holds a per-flight mutation mutex
while encoding, writing and notifying. Such callbacks must not re-enter that
same-key mutation boundary. The separate superseded-load reread releases that
mutex before its observer callbacks, as covered by the reentrancy assertion.

A successful `Set`, `SetIfOwned`, `SetNegativeIfOwned`, `Add`, `Replace`, or
`Delete` on the same cache instance wins over a concurrent foreground load or
background refresh. Failed writes and rejected conditions do not suppress a
valid loader result. Ordinary mutations do not fence another process or
another `Cache` instance.

For a distributed refresh using the Valkey backend, `SetIfOwned` and
`SetNegativeIfOwned` compare the exact active lease storage key, owner, and
fencing token and write the cache record in one server-side script. Expired,
released, missing, and successor leases reject the publish with
`ErrOwnershipLost`, even when the previous cache record has expired or was
deleted. This protects reconstructible cache publication; it is not a
distributed lock API, exactly-once loading promise, or substitute for
resource-side fencing of durable business effects.

Stale-while-revalidate uses the same flight map and global bound. Repeated stale
reads therefore start one background refresh per key. Stale-if-error waits on
the same foreground flight and returns the stale value plus the refresh error.

Applications must make loaders context-aware and must bound their own network
clients. A loader that ignores cancellation may outlive shutdown, but the cache
rejects new loads after closure. `Close` returns `ErrShutdownIncomplete` after
five seconds; `Shutdown` returns it when the supplied context ends.

Cancellation is checked before load publication, but is not an atomic storage
commit barrier. A publication already admitted before shutdown may call or
complete a cancellation-ignoring backend after `ErrShutdownIncomplete` returns.
Successful `Shutdown` waits for all active loads, including publication and
synchronous load notifications. It does not join independent foreground
operations already underway or close backend/client/exporter resources.
Use context-aware backend I/O and separately bounded native-client operations;
do not interpret incomplete shutdown as proof that side effects have stopped.

The global loader semaphore does not promise FIFO fairness. Independent keys
make progress subject to Go scheduler and channel scheduling. A loader must not
recursively call the same cache with its supplied context; such calls return
`ErrRecursiveLoad`. If it discards that context, recursion cannot be detected
and may deadlock, which is another reason context propagation is mandatory.

Choose the next guide in the [documentation index](README.md)
or return to the [repository entry](../README.md) for installation and ownership.
