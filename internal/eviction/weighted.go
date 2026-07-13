package eviction

// Weighted is a Phase 5 composite eviction policy: it combines multiple
// signals (recency, access frequency, caller-supplied cost/importance
// hints) into a single weighted eviction score, instead of LRU's
// recency-only view. This is explicitly a heuristic -- "smarter than LRU,"
// not "optimal" -- picked for being cheap to compute per entry and for
// degrading gracefully to something LRU-like when only recency is
// populated (the common case from internal/storage/memstore, which has no
// notion of CostHint/Importance).
type Weighted struct {
	// Weights maps a named signal to its contribution weight. Recognized
	// keys: "recency", "frequency", "cost", "importance". Missing keys are
	// treated as weight 0. A nil/empty map uses DefaultWeights.
	Weights map[string]float64
}

// DefaultWeights is used by Weighted.Score when Weights is nil/empty:
// recency dominates but frequency still matters; cost/importance default to
// 0 weight since most callers (internal/storage/memstore included) never
// populate those signals.
var DefaultWeights = map[string]float64{
	"recency":   0.6,
	"frequency": 0.4,
}

// NewWeighted constructs a Weighted policy with the given per-signal
// weights. A nil/empty map is fine -- Score falls back to DefaultWeights.
func NewWeighted(weights map[string]float64) *Weighted {
	return &Weighted{Weights: weights}
}

// Score implements Policy. Higher scores are evicted first, matching LRU's
// convention. The formula combines four independently-normalized, bounded
// ([0, 1)-ish) contributions so no single signal's raw units (seconds vs.
// a small integer count vs. an arbitrary cost/importance scale) can
// dominate the weighted sum just by having larger magnitude, and so the
// result never overflows to NaN/Inf even for zero-valued signals:
//
//   - recency:    ageSeconds / (1 + ageSeconds), ageSeconds = max(0, Now - LastAccess).
//     Saturates toward 1 as an entry gets older -- older is more evictable,
//     same direction as LRU.
//   - frequency:  1 / (1 + AccessCount).
//     1.0 when never accessed, shrinking toward 0 as AccessCount grows --
//     frequently-accessed entries contribute less to the evictable score.
//   - cost:       1 / (1 + CostHint).
//     Same shape as frequency: an entry hinted as expensive to recreate
//     contributes less (harder to evict).
//   - importance: 1 / (1 + Importance).
//     Same shape again: a higher caller-set importance contributes less.
//
// The final score is the weighted sum of those four contributions. All
// inputs are assumed non-negative (AccessCount, CostHint, Importance are
// documented as such by Signals); zero values for any of them are handled
// cleanly (frequency/cost/importance contribute their maximum of 1.0 when
// their underlying value is 0, recency contributes 0 when Now == LastAccess
// or LastAccess is zero-ish and Now is used as an anchor).
func (w *Weighted) Score(s Signals) float64 {
	weights := w.Weights
	if len(weights) == 0 {
		weights = DefaultWeights
	}

	age := s.Now.Sub(s.LastAccess).Seconds()
	if s.LastAccess.IsZero() {
		age = float64(s.Now.Unix())
	}
	if age < 0 {
		age = 0
	}
	recencyContrib := age / (1 + age)
	frequencyContrib := 1 / (1 + float64(s.AccessCount))
	costContrib := 1 / (1 + s.CostHint)
	importanceContrib := 1 / (1 + s.Importance)

	return weights["recency"]*recencyContrib +
		weights["frequency"]*frequencyContrib +
		weights["cost"]*costContrib +
		weights["importance"]*importanceContrib
}
