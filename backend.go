package cache

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// Condition controls the atomic precondition applied by Backend.Set.
type Condition uint8

const (
	// Unconditional always writes the record.
	Unconditional Condition = iota
	// IfAbsent writes only when the key does not hold a live record.
	IfAbsent
	// IfPresent writes only when the key holds a live record.
	IfPresent
)

// Record is the portable value and expiration envelope stored by a Backend.
type Record struct {
	// Payload is caller-owned mutable encoded data; Clone isolates its bytes.
	Payload []byte
	// ExpiresAt is the first instant the value is no longer fresh.
	ExpiresAt time.Time
	// StaleAt is the hard usability deadline, at or after ExpiresAt.
	StaleAt time.Time
	// Negative marks authoritative absence and requires an empty payload.
	Negative bool
}

// Clone returns a portable record whose payload does not alias the receiver.
func (r Record) Clone() Record {
	clone := r
	clone.Payload = append([]byte(nil), r.Payload...)
	clone.ExpiresAt = r.ExpiresAt.Round(0)
	clone.StaleAt = r.StaleAt.Round(0)
	return clone
}

// Validate checks deadline portability and negative-record invariants.
// It does not compare deadlines with a clock or require a future hard expiry.
// Portable time must round-trip through signed nanoseconds since the Unix epoch.
func (r Record) Validate() error {
	if slices.Contains([]bool{
		r.ExpiresAt.IsZero(),
		r.StaleAt.IsZero(),
		r.StaleAt.Before(r.ExpiresAt),
		!portableTime(r.ExpiresAt),
		!portableTime(r.StaleAt),
		r.Negative && len(r.Payload) != 0,
	}, true) {
		return fmt.Errorf("%w: deadlines or negative payload", ErrInvalidRecord)
	}
	return nil
}

func portableTime(value time.Time) bool {
	return time.Unix(0, value.UnixNano()).Equal(value)
}

// Backend is the concurrent atomic storage contract implemented by adapters.
// Presence denotes provider retention, not freshness; Cache.Get separately
// enforces StaleAt using its clock. Server expiry may differ from that clock.
// Implementations must honor contexts and copy retained/returned payload bytes.
// Native clients, retries and shutdown remain provider/caller responsibilities;
// memory lazy expiration and server TTL precision are not identical.
type Backend interface {
	// Get returns a payload copy and explicit presence; failures are not misses.
	Get(context.Context, string) (Record, bool, error)
	// Set atomically applies condition and owns stored payload bytes on success.
	Set(context.Context, string, Record, Condition) (bool, error)
	// Delete atomically removes a key and reports presence, not value contents.
	Delete(context.Context, string) (bool, error)
}

// OwnershipGuard identifies backend-authenticated ownership for one protected
// write. Methods must supply stable values during a call and must not panic.
// Implementations must return opaque storage coordinates, not logical
// keys or credentials.
type OwnershipGuard interface {
	// StorageKey returns the external lease storage coordinate; it may be sensitive.
	StorageKey() string
	// Owner returns the opaque identity to compare with the active owner.
	Owner() string
	// Token returns the opaque fencing identity to compare, not a numeric order.
	Token() string
}

// OwnershipBackend atomically validates ownership and writes one record.
type OwnershipBackend interface {
	Backend
	// SetIfOwned compares ownership and publishes atomically, or reports loss/failure.
	SetIfOwned(context.Context, string, Record, OwnershipGuard) error
}

// Clock supplies time for wall-clock expiration behavior. Implementations must
// be concurrency-safe, bounded and panic-free. Memory invokes Now under its
// mutex, so clock callbacks must not re-enter that backend.
type Clock interface {
	// Now returns the time used for wall-clock deadlines; calls must be bounded.
	Now() time.Time
}

// SystemClock uses the system wall clock.
type SystemClock struct{}

// Now returns the current system time.
func (SystemClock) Now() time.Time { return time.Now() }
