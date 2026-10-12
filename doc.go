// Package cache provides typed, backend-independent cache semantics.
//
// Cache distinguishes fresh Hit, absent/negative Miss, Stale and errors. Backend
// records own retained payload bytes; generic K and V are not deep copied.
// A published found load shares the loader's original V with its waiters, so mutable
// values require application ownership or an explicitly copying codec/loader.
//
// Construct with New and a validated NewKeySpace. Dependencies are retained,
// must be concurrency-safe, and must bound their own work. Only loader and
// observer notification panics are isolated; other callbacks are trusted.
// Observers run synchronously, and some publication callbacks run under a
// per-flight mutation lock. Do not re-enter that protected boundary.
//
// Caller cancellation detaches one load waiter. Load work uses a separate
// cache-owned context; Shutdown cancels and joins it within the caller's bound,
// without closing a backend/client or joining unrelated foreground calls.
// Incomplete shutdown is not proof that admitted external side effects stopped.
package cache
