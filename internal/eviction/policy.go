// Package eviction defines eviction-scoring policies used to decide which
// keys to reclaim under memory pressure. Entry-level last-access tracking
// (internal/storage/memstore.Entry.LastAccess) feeds a default LRU policy
// and a maxmemory-style bounded trigger (internal/storage/memstore.Store's
// maxEntries option) that calls into this package, plus a real composite
// Weighted policy alongside LRU.
package eviction

import "time"

// Signals is everything a Policy can use to score one entry's eviction
// eligibility. Not every field is populated by every caller -- e.g.
// internal/storage/memstore has no notion of a semantic-cache "cost" or a
// user-set importance, so CostHint/Importance are always zero from there.
// Policies must treat zero as "no signal," not "explicitly low," and
// implementations that don't use a given signal should simply ignore it.
type Signals struct {
	LastAccess time.Time
	Now        time.Time

	// AccessCount is the number of times this entry has been read/written;
	// 0 if never tracked (or genuinely never accessed beyond creation).
	AccessCount int64

	// CostHint is a caller-supplied "expensive to recreate" hint; 0 if
	// unknown.
	CostHint float64

	// Importance is a caller-supplied priority/importance value; 0 if
	// unset.
	Importance float64
}

// Policy scores an entry for eviction eligibility. Higher scores are
// evicted first. Implementations deliberately take a primitive Signals
// value (not a concrete storage.Entry) to avoid a dependency between
// internal/eviction and internal/storage/memstore.
type Policy interface {
	// Score returns the eviction score for one entry. Higher scores are
	// evicted first.
	Score(Signals) float64
}
