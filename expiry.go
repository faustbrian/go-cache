package cache

import (
	"math"
	"time"
)

// maxPortableDeadline is the latest instant a Record can carry: Record.Validate
// requires deadlines to round-trip through signed Unix nanoseconds.
var maxPortableDeadline = time.Unix(0, math.MaxInt64)

type expiryKind uint8

const (
	expiryPolicy expiryKind = iota
	expiryAt
	expiryNever
)

// Expiry selects the freshness deadline of one entry. The zero value defers to
// the cache's TTLPolicy, RefreshJitter and NegativeTTL, so existing callers keep
// their fixed-TTL behavior. A non-zero Expiry replaces the fresh TTL and jitter
// for that entry: ExpireAt is absolute and NeverExpire never becomes stale.
// Expiry is an immutable value and is safe to share between goroutines.
type Expiry struct {
	at   time.Time
	kind expiryKind
}

// ExpireAt returns an absolute freshness deadline. A positive entry remains
// fresh before deadline and stays usable as Stale for the configured StaleFor
// after it; a negative entry ends at deadline. A deadline at or before the
// cache clock behaves like an already expired write: the key is deleted and
// nothing is stored. A zero or non-portable deadline, or one whose stale window
// passes the latest portable instant, is rejected with ErrInvalidTTL.
func ExpireAt(deadline time.Time) Expiry {
	return Expiry{at: deadline, kind: expiryAt}
}

// NeverExpire returns an expiry that keeps an entry fresh until it is
// explicitly mutated or evicted by its backend. StaleFor does not apply.
// Remote backends store it with the longest server TTL the portable deadline
// allows, roughly two centuries from now, rather than a persistent key.
func NeverExpire() Expiry {
	return Expiry{kind: expiryNever}
}

// IsZero reports whether e defers to the cache's configured TTL policy.
func (e Expiry) IsZero() bool {
	return e.kind == expiryPolicy
}

// deadlines resolves a non-zero expiry against now. live is false when an
// absolute deadline has already passed. Sliding reads rewrite ExpiresAt from
// the policy TTL, which would silently discard an explicit deadline, so the
// combination is rejected instead.
func (e Expiry) deadlines(
	operation Operation,
	now time.Time,
	staleFor time.Duration,
	sliding bool,
) (expiresAt, staleAt time.Time, live bool, err error) {
	if sliding {
		return time.Time{}, time.Time{}, false, NewError(PolicyError, operation, ErrInvalidPolicy)
	}
	if e.kind == expiryNever {
		return maxPortableDeadline, maxPortableDeadline, true, nil
	}
	if e.at.IsZero() || !portableTime(e.at) {
		return time.Time{}, time.Time{}, false, NewError(PolicyError, operation, ErrInvalidTTL)
	}
	expiresAt = e.at.Round(0)
	if staleFor > maxPortableDeadline.Sub(expiresAt) {
		return time.Time{}, time.Time{}, false, NewError(PolicyError, operation, ErrInvalidTTL)
	}
	return expiresAt, expiresAt.Add(staleFor), now.Before(expiresAt), nil
}
