package cache

import "time"

// TTLPolicy defines the fresh and stale lifetime of positive records.
type TTLPolicy struct {
	// TTL is the strictly positive fresh duration.
	TTL time.Duration
	// StaleFor is additional non-negative hard-retention time after freshness.
	StaleFor time.Duration
	// Sliding rewrites fresh records on hit with IfPresent, not record-version CAS.
	Sliding bool
}

// Validate reports whether the TTL and stale window are well formed.
func (p TTLPolicy) Validate() error {
	if p.TTL <= 0 || p.StaleFor < 0 {
		return ErrInvalidTTL
	}
	return nil
}
