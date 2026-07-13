package analytics

import (
	"sync"
	"testing"
)

func TestRecordEmbeddingUsageKnownModelAccumulatesCost(t *testing.T) {
	tr := New()
	tr.RecordEmbeddingUsage("text-embedding-3-small", 1_000_000)
	tr.RecordEmbeddingUsage("text-embedding-3-small", 1_000_000)

	snap := tr.Snapshot()
	got, ok := snap.EmbeddingByModel["text-embedding-3-small"]
	if !ok {
		t.Fatalf("no usage recorded for text-embedding-3-small; snapshot: %+v", snap)
	}
	if got.Tokens != 2_000_000 {
		t.Fatalf("Tokens = %d, want 2,000,000", got.Tokens)
	}
	if !got.PricingKnown {
		t.Fatal("expected PricingKnown = true for a recognized model")
	}
	wantCost := 0.04 // 2 * $0.02/1M tokens
	if diff := got.CostUSD - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("CostUSD = %v, want %v", got.CostUSD, wantCost)
	}
}

func TestRecordEmbeddingUsageAcceptsProviderNameForm(t *testing.T) {
	tr := New()
	// Provider.Name() returns "openai:<model>"; RecordEmbeddingUsage must
	// normalize that form the same way as a bare model name.
	tr.RecordEmbeddingUsage("openai:text-embedding-3-large", 1_000_000)

	snap := tr.Snapshot()
	got, ok := snap.EmbeddingByModel["text-embedding-3-large"]
	if !ok {
		t.Fatalf("no usage recorded for text-embedding-3-large; snapshot: %+v", snap)
	}
	if got.Tokens != 1_000_000 {
		t.Fatalf("Tokens = %d, want 1,000,000", got.Tokens)
	}
	wantCost := 0.13
	if diff := got.CostUSD - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("CostUSD = %v, want %v", got.CostUSD, wantCost)
	}
}

func TestRecordEmbeddingUsageUnknownModelNoFabricatedCost(t *testing.T) {
	tr := New()
	tr.RecordEmbeddingUsage("some-future-model-nobody-has-heard-of", 500)

	snap := tr.Snapshot()
	got, ok := snap.EmbeddingByModel["some-future-model-nobody-has-heard-of"]
	if !ok {
		t.Fatalf("expected tokens to still be tracked for an unknown model; snapshot: %+v", snap)
	}
	if got.Tokens != 500 {
		t.Fatalf("Tokens = %d, want 500", got.Tokens)
	}
	if got.PricingKnown {
		t.Fatal("expected PricingKnown = false for an unrecognized model")
	}
	if got.CostUSD != 0 {
		t.Fatalf("CostUSD = %v, want 0 for an unrecognized model (no fabricated price)", got.CostUSD)
	}
}

func TestRecordEmbeddingUsageNonPositiveTokensNoOp(t *testing.T) {
	tr := New()
	tr.RecordEmbeddingUsage("text-embedding-3-small", 0)
	tr.RecordEmbeddingUsage("text-embedding-3-small", -5)

	snap := tr.Snapshot()
	if len(snap.EmbeddingByModel) != 0 {
		t.Fatalf("expected no recorded usage for non-positive token counts, got %+v", snap.EmbeddingByModel)
	}
}

func TestRecordCacheHitSavingsAccumulatesAndSkipsNonPositive(t *testing.T) {
	tr := New()
	tr.RecordCacheHitSavings("semantic", "what is kubernetes", 0.01)
	tr.RecordCacheHitSavings("semantic", "what is kubernetes", 0.01) // same entry hit again
	tr.RecordCacheHitSavings("prompt", "some template", 0.05)
	tr.RecordCacheHitSavings("semantic", "no cost reported", 0) // no-op
	tr.RecordCacheHitSavings("semantic", "negative cost", -1)   // no-op

	snap := tr.Snapshot()
	wantSaved := 0.01 + 0.01 + 0.05
	if diff := snap.MoneySavedTotalUSD - wantSaved; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("MoneySavedTotalUSD = %v, want %v", snap.MoneySavedTotalUSD, wantSaved)
	}

	// Repeated hits on the same (cacheType, prompt) should dedupe into a
	// single entry with an incremented Hits count, not a duplicate row.
	var k8sEntry *ExpensiveEntry
	for i := range snap.TopExpensiveEntries {
		if snap.TopExpensiveEntries[i].Prompt == "what is kubernetes" {
			k8sEntry = &snap.TopExpensiveEntries[i]
		}
	}
	if k8sEntry == nil {
		t.Fatal("expected an entry for 'what is kubernetes'")
	}
	if k8sEntry.Hits != 2 {
		t.Fatalf("Hits = %d, want 2", k8sEntry.Hits)
	}

	if len(snap.TopExpensiveEntries) != 2 {
		t.Fatalf("expected exactly 2 distinct entries (no zero/negative-cost entries), got %d: %+v", len(snap.TopExpensiveEntries), snap.TopExpensiveEntries)
	}
}

func TestSnapshotTopExpensiveEntriesSortedDescending(t *testing.T) {
	tr := New()
	tr.RecordCacheHitSavings("semantic", "cheap", 0.01)
	tr.RecordCacheHitSavings("semantic", "expensive", 5.00)
	tr.RecordCacheHitSavings("semantic", "medium", 1.00)

	snap := tr.Snapshot()
	if len(snap.TopExpensiveEntries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap.TopExpensiveEntries))
	}
	for i := 1; i < len(snap.TopExpensiveEntries); i++ {
		if snap.TopExpensiveEntries[i-1].Cost < snap.TopExpensiveEntries[i].Cost {
			t.Fatalf("TopExpensiveEntries not sorted descending: %+v", snap.TopExpensiveEntries)
		}
	}
	if snap.TopExpensiveEntries[0].Prompt != "expensive" {
		t.Fatalf("most expensive entry = %q, want %q", snap.TopExpensiveEntries[0].Prompt, "expensive")
	}
}

func TestSnapshotTopExpensiveEntriesBounded(t *testing.T) {
	tr := New()
	// Record more than maxTopEntries unique, increasingly expensive
	// entries; only the maxTopEntries most expensive should survive.
	for i := 0; i < maxTopEntries+10; i++ {
		tr.RecordCacheHitSavings("semantic", string(rune('a'+i)), float64(i)+1)
	}

	snap := tr.Snapshot()
	if len(snap.TopExpensiveEntries) != maxTopEntries {
		t.Fatalf("len(TopExpensiveEntries) = %d, want %d", len(snap.TopExpensiveEntries), maxTopEntries)
	}
	// The cheapest entries recorded (cost 1..10) should have been evicted;
	// the survivor with the smallest cost should be the 11th recorded
	// (cost 11).
	minCost := snap.TopExpensiveEntries[len(snap.TopExpensiveEntries)-1].Cost
	if minCost != 11 {
		t.Fatalf("smallest surviving cost = %v, want 11", minCost)
	}
}

func TestSnapshotIsIndependentCopy(t *testing.T) {
	tr := New()
	tr.RecordEmbeddingUsage("text-embedding-3-small", 100)
	tr.RecordCacheHitSavings("semantic", "p", 1.0)

	snap := tr.Snapshot()
	snap.EmbeddingByModel["text-embedding-3-small"] = ModelUsage{Tokens: 999999}
	snap.TopExpensiveEntries[0].Cost = 999999

	fresh := tr.Snapshot()
	if fresh.EmbeddingByModel["text-embedding-3-small"].Tokens != 100 {
		t.Fatal("mutating a returned Snapshot's map affected Tracker's internal state")
	}
	if fresh.TopExpensiveEntries[0].Cost != 1.0 {
		t.Fatal("mutating a returned Snapshot's slice affected Tracker's internal state")
	}
}

func TestTrackerConcurrentUse(t *testing.T) {
	tr := New()
	var wg sync.WaitGroup
	const n = 200
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.RecordEmbeddingUsage("text-embedding-3-small", 10)
			tr.RecordCacheHitSavings("semantic", "shared prompt", 0.02)
		}()
	}
	wg.Wait()

	snap := tr.Snapshot()
	if snap.EmbeddingByModel["text-embedding-3-small"].Tokens != n*10 {
		t.Fatalf("Tokens = %d, want %d", snap.EmbeddingByModel["text-embedding-3-small"].Tokens, n*10)
	}
	wantSaved := float64(n) * 0.02
	if diff := snap.MoneySavedTotalUSD - wantSaved; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("MoneySavedTotalUSD = %v, want %v", snap.MoneySavedTotalUSD, wantSaved)
	}
}
