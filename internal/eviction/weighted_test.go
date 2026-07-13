package eviction

import (
	"math"
	"testing"
	"time"
)

func TestWeightedScoreZeroSignalsNoPanicNoNaN(t *testing.T) {
	w := NewWeighted(nil)
	now := time.Now()

	score := w.Score(Signals{LastAccess: now, Now: now})
	if math.IsNaN(score) || math.IsInf(score, 0) {
		t.Fatalf("expected a finite score for all-zero signals, got %v", score)
	}
}

func TestWeightedScoreNilWeightsUsesDefault(t *testing.T) {
	w := NewWeighted(nil)
	now := time.Now()
	old := now.Add(-1 * time.Hour)

	got := w.Score(Signals{LastAccess: old, Now: now})

	want := &Weighted{Weights: DefaultWeights}
	wantScore := want.Score(Signals{LastAccess: old, Now: now})

	if got != wantScore {
		t.Fatalf("nil Weights should behave like DefaultWeights: got %v want %v", got, wantScore)
	}
}

func TestWeightedScoreHigherAccessCountLowersScore(t *testing.T) {
	w := NewWeighted(map[string]float64{"recency": 0.6, "frequency": 0.4})
	now := time.Now()
	lastAccess := now.Add(-10 * time.Minute)

	base := w.Score(Signals{LastAccess: lastAccess, Now: now, AccessCount: 0})
	frequent := w.Score(Signals{LastAccess: lastAccess, Now: now, AccessCount: 1000})

	if !(frequent < base) {
		t.Fatalf("expected higher AccessCount to strictly lower the score: base=%v frequent=%v", base, frequent)
	}
}

func TestWeightedScoreHigherCostHintLowersScore(t *testing.T) {
	w := NewWeighted(map[string]float64{"recency": 0.5, "cost": 0.5})
	now := time.Now()
	lastAccess := now.Add(-10 * time.Minute)

	base := w.Score(Signals{LastAccess: lastAccess, Now: now, CostHint: 0})
	expensive := w.Score(Signals{LastAccess: lastAccess, Now: now, CostHint: 1000})

	if !(expensive < base) {
		t.Fatalf("expected higher CostHint to strictly lower the score: base=%v expensive=%v", base, expensive)
	}
}

func TestWeightedScoreHigherImportanceLowersScore(t *testing.T) {
	w := NewWeighted(map[string]float64{"recency": 0.5, "importance": 0.5})
	now := time.Now()
	lastAccess := now.Add(-10 * time.Minute)

	base := w.Score(Signals{LastAccess: lastAccess, Now: now, Importance: 0})
	important := w.Score(Signals{LastAccess: lastAccess, Now: now, Importance: 1000})

	if !(important < base) {
		t.Fatalf("expected higher Importance to strictly lower the score: base=%v important=%v", base, important)
	}
}

func TestWeightedScoreEmptyWeightsMapUsesDefault(t *testing.T) {
	w := NewWeighted(map[string]float64{})
	now := time.Now()
	old := now.Add(-1 * time.Hour)

	got := w.Score(Signals{LastAccess: old, Now: now})

	want := &Weighted{Weights: DefaultWeights}
	wantScore := want.Score(Signals{LastAccess: old, Now: now})

	if got != wantScore {
		t.Fatalf("empty Weights should behave like DefaultWeights: got %v want %v", got, wantScore)
	}
}
