package eviction

// LRU is the default eviction Policy: it scores entries purely by
// how long ago they were last accessed. The longer since last access, the
// higher (more evictable) the score.
type LRU struct{}

// NewLRU constructs the default least-recently-used policy.
func NewLRU() LRU { return LRU{} }

// Score implements Policy: the score is simply the age (in seconds) since
// Signals.LastAccess, so older (less recently used) entries sort as more
// eligible for eviction. Only LastAccess/Now are used; the other Signals
// fields are ignored.
func (LRU) Score(s Signals) float64 {
	if s.LastAccess.IsZero() {
		return float64(s.Now.Unix())
	}
	return s.Now.Sub(s.LastAccess).Seconds()
}
