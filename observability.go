package cache

import (
	"context"
	"time"
)

// Outcome is a low-cardinality semantic operation result.
type Outcome string

const (
	// OutcomeSuccess identifies a successful mutation or load.
	OutcomeSuccess Outcome = "success"
	// OutcomeHit identifies a fresh read.
	OutcomeHit Outcome = "hit"
	// OutcomeMiss identifies an absent read.
	OutcomeMiss Outcome = "miss"
	// OutcomeStale identifies a stale read.
	OutcomeStale Outcome = "stale"
	// OutcomeNegative identifies a negative-cache result.
	OutcomeNegative Outcome = "negative"
	// OutcomeRejected identifies a failed conditional mutation.
	OutcomeRejected Outcome = "rejected"
	// OutcomeError identifies an operation failure.
	OutcomeError Outcome = "error"
	// OutcomeEvicted identifies capacity eviction.
	OutcomeEvicted Outcome = "evicted"
	// OutcomeExpired identifies deadline expiration.
	OutcomeExpired Outcome = "expired"
)

// Event is a redacted semantic observation with no key or value fields.
type Event struct {
	// Operation names the action; direct caller strings are not automatically redacted.
	Operation Operation
	// Outcome names the result; bundled cache producers use the defined constants.
	Outcome Outcome
	// Duration is non-negative cache-clock elapsed time for bundled producers.
	Duration time.Duration
	// Size is payload bytes for reads/writes, or retained key+payload bytes for removals.
	Size int
}

// Observer receives synchronous semantic events from cache operations.
// Notification discards returned errors and recovers observer panics, but cannot
// force a blocked callback to return. Implementations must bound work, support
// concurrent calls and avoid re-entering publication locks. Contexts can be
// caller-owned or cache-load-owned; event fields alone are not a redaction engine.
type Observer interface {
	// Observe receives an event synchronously; returned errors and panics are ignored by cache notification.
	Observe(context.Context, Event) error
}

// notify isolates the callback outcome, not its latency or external side effects.
func notify(ctx context.Context, observer Observer, event Event) {
	if observer == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	_ = observer.Observe(ctx, event)
}

func elapsed(start, end time.Time) time.Duration {
	return max(end.Sub(start), 0)
}

func resultOutcome[V any](result Result[V], err error) Outcome {
	if err != nil {
		return OutcomeError
	}
	if result.Negative {
		return OutcomeNegative
	}
	switch result.State {
	case Hit:
		return OutcomeHit
	case Miss:
		return OutcomeMiss
	case Stale:
		return OutcomeStale
	default:
		return OutcomeSuccess
	}
}
