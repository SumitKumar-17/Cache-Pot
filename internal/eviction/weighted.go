package eviction

import "time"

// Weighted is a Phase 5 skeleton stub: a composite eviction policy that will
// combine multiple signals (recency, access frequency, entry size, semantic
// relevance score, etc.) into a single weighted eviction score. Not
// implemented in Phase 1 — internal/eviction.LRU is the active default.
type Weighted struct {
	// Weights maps a named signal (e.g. "recency", "frequency", "size") to
	// its contribution weight. Left unpopulated/unused until Phase 5.
	Weights map[string]float64
}

// NewWeighted constructs an (currently inert) Weighted policy stub.
func NewWeighted(weights map[string]float64) *Weighted {
	return &Weighted{Weights: weights}
}

// Score satisfies Policy so Weighted type-checks as a drop-in replacement
// for LRU, but the composite scoring logic is a Phase 5 deliverable and is
// not implemented yet.
func (w *Weighted) Score(lastAccess time.Time, now time.Time) float64 {
	panic("eviction: Weighted.Score is a Phase 5 stub, not yet implemented")
}
