package eviction

import "time"

// LRU is the Phase 1 default eviction Policy: it scores entries purely by
// how long ago they were last accessed. The longer since last access, the
// higher (more evictable) the score.
type LRU struct{}

// NewLRU constructs the default least-recently-used policy.
func NewLRU() LRU { return LRU{} }

// Score implements Policy: the score is simply the age (in seconds) since
// lastAccess, so older (less recently used) entries sort as more eligible
// for eviction.
func (LRU) Score(lastAccess time.Time, now time.Time) float64 {
	if lastAccess.IsZero() {
		return float64(now.Unix())
	}
	return now.Sub(lastAccess).Seconds()
}
