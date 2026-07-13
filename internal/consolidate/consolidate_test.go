package consolidate

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/embed"
	"github.com/SumitKumar-17/cache-pot/internal/llm"
	"github.com/SumitKumar-17/cache-pot/internal/memory"
)

func newTestConsolidator() (*Consolidator, *memory.Store) {
	store := memory.New(embed.NewMock(8))
	return New(store, llm.NewMock()), store
}

// TestConsolidateDedupsNearDuplicatesNonDestructively puts three
// near-identical episodic memories (same words, different
// case/whitespace -- internal/embed's mock provider's documented
// near-duplicate-closeness behavior, see internal/embed/mock.go) plus one
// clearly-distinct memory, then confirms Consolidate's dedup pass collapsed
// the three near-duplicates into one representative for summarization,
// while the underlying store still holds every one of the four original
// memories untouched.
func TestConsolidateDedupsNearDuplicatesNonDestructively(t *testing.T) {
	c, store := newTestConsolidator()
	ctx := context.Background()

	put := func(id, content string) {
		if _, err := store.Put(ctx, memory.Memory{
			ID:          id,
			AgentID:     "agent-1",
			WorkspaceID: "default",
			Kind:        memory.Episodic,
			Content:     content,
		}, 0); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}

	put("dup-1", "user completed the onboarding flow")
	put("dup-2", "User completed the onboarding flow")
	put("dup-3", "USER COMPLETED THE ONBOARDING FLOW")
	put("distinct-1", "the weather in paris is nice today")

	result, err := c.Consolidate(ctx, "default", "agent-1", memory.Episodic, 0)
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	if result.SourceCount != 4 {
		t.Fatalf("SourceCount = %d, want 4", result.SourceCount)
	}
	if result.DedupedCount != 2 {
		t.Fatalf("DedupedCount = %d, want 2 (the 3 near-duplicates collapsed to 1, plus the 1 distinct memory)", result.DedupedCount)
	}
	if result.SummaryID == "" {
		t.Fatal("expected a non-empty SummaryID")
	}

	// Non-destructiveness: every one of the 4 original source memories
	// must still be present in the store, completely unchanged.
	for _, id := range []string{"dup-1", "dup-2", "dup-3", "distinct-1"} {
		if _, found, err := store.Get(ctx, "default", id); err != nil || !found {
			t.Fatalf("Get(%s) after Consolidate: found=%v err=%v, want the source memory to still exist untouched", id, found, err)
		}
	}

	// The new summary itself must also be fetchable and correctly shaped.
	summary, found, err := store.Get(ctx, "default", result.SummaryID)
	if err != nil || !found {
		t.Fatalf("Get(summary) after Consolidate: found=%v err=%v", found, err)
	}
	if summary.Kind != memory.LongTerm {
		t.Fatalf("summary Kind = %v, want LongTerm", summary.Kind)
	}
	if summary.AgentID != "agent-1" {
		t.Fatalf("summary AgentID = %q, want agent-1", summary.AgentID)
	}
	if !strings.HasPrefix(summary.Content, "[mock completion, no real generation] ") {
		t.Fatalf("summary Content = %q, want the mock CompletionProvider's marked output", summary.Content)
	}
	if summary.Metadata["consolidated_from_kind"] != "episodic" {
		t.Errorf("summary Metadata[consolidated_from_kind] = %q, want episodic", summary.Metadata["consolidated_from_kind"])
	}
	if summary.Metadata["source_count"] != "4" {
		t.Errorf("summary Metadata[source_count] = %q, want 4", summary.Metadata["source_count"])
	}
	if summary.Metadata["deduped_count"] != "2" {
		t.Errorf("summary Metadata[deduped_count] = %q, want 2", summary.Metadata["deduped_count"])
	}
}

// TestConsolidateKeepsMostRecentRepresentative confirms that, within a
// dedup cluster, the representative fed into summarization is the most
// recently created member, per dedupe's documented rule. It calls dedupe
// directly with explicit CreatedAt timestamps (memory.Store.Put always
// stamps CreatedAt with the real wall clock and offers no way to override
// it from outside the memory package, so this constructs memory.Memory
// values directly rather than going through the store).
func TestConsolidateKeepsMostRecentRepresentative(t *testing.T) {
	ctx := context.Background()
	provider := embed.NewMock(8)
	base := time.Now()

	vecOlder, err := provider.Embed(ctx, "user completed the onboarding flow")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	vecNewer, err := provider.Embed(ctx, "User completed the onboarding flow")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	older := memory.Memory{ID: "older", Content: "user completed the onboarding flow", Embedding: vecOlder, CreatedAt: base}
	newer := memory.Memory{ID: "newer", Content: "User completed the onboarding flow", Embedding: vecNewer, CreatedAt: base.Add(time.Hour)}

	reps := dedupe([]memory.Memory{older, newer}, DefaultDedupThreshold)
	if len(reps) != 1 {
		t.Fatalf("dedupe returned %d representatives, want 1", len(reps))
	}
	if reps[0].ID != "newer" {
		t.Fatalf("dedupe representative = %q, want %q (the most recently created)", reps[0].ID, "newer")
	}
}

// TestConsolidateNothingToSummarize confirms a zero-memory input is a
// non-error "nothing to summarize" outcome, not a caller mistake.
func TestConsolidateNothingToSummarize(t *testing.T) {
	c, _ := newTestConsolidator()
	ctx := context.Background()

	result, err := c.Consolidate(ctx, "default", "agent-with-no-memories", memory.Episodic, 0)
	if err != nil {
		t.Fatalf("Consolidate (no memories): unexpected error %v", err)
	}
	if result.SummaryID != "" {
		t.Fatalf("SummaryID = %q, want empty for nothing-to-summarize", result.SummaryID)
	}
	if result.SourceCount != 0 || result.DedupedCount != 0 {
		t.Fatalf("Result = %+v, want SourceCount=0 DedupedCount=0", result)
	}
}

// TestConsolidateScopedByAgentAndKind confirms Consolidate only summarizes
// the requested agent's memories of the requested kind, leaving other
// agents' and other kinds' memories out of the source set entirely.
func TestConsolidateScopedByAgentAndKind(t *testing.T) {
	c, store := newTestConsolidator()
	ctx := context.Background()

	put := func(id, agentID string, kind memory.Kind, content string) {
		if _, err := store.Put(ctx, memory.Memory{
			ID:          id,
			AgentID:     agentID,
			WorkspaceID: "default",
			Kind:        kind,
			Content:     content,
		}, 0); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}

	put("a-epi", "agent-a", memory.Episodic, "agent-a's episodic memory")
	put("a-long", "agent-a", memory.LongTerm, "agent-a's long-term memory")
	put("b-epi", "agent-b", memory.Episodic, "agent-b's episodic memory")

	result, err := c.Consolidate(ctx, "default", "agent-a", memory.Episodic, 0)
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if result.SourceCount != 1 {
		t.Fatalf("SourceCount = %d, want 1 (only agent-a's episodic memory)", result.SourceCount)
	}
}

// TestConsolidateDefaultDedupThreshold confirms passing <= 0 for
// dedupThreshold falls back to DefaultDedupThreshold rather than, say,
// treating every memory as a duplicate of every other.
func TestConsolidateDefaultDedupThreshold(t *testing.T) {
	c, store := newTestConsolidator()
	ctx := context.Background()

	for i, content := range []string{
		"the quarterly report is due friday",
		"remember to water the office plants",
	} {
		if _, err := store.Put(ctx, memory.Memory{
			AgentID:     "agent-1",
			WorkspaceID: "default",
			Kind:        memory.Episodic,
			Content:     content,
		}, 0); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	result, err := c.Consolidate(ctx, "default", "agent-1", memory.Episodic, 0)
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if result.SourceCount != 2 || result.DedupedCount != 2 {
		t.Fatalf("Result = %+v, want 2 distinct memories to both survive dedup under the default threshold", result)
	}
}
