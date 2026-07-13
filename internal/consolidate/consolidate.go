// Package consolidate implements Phase 6b's memory consolidation: turning a
// cluster of accumulated agent memories (typically episodic) into a single
// long-term summary via internal/llm's CompletionProvider. It backs the
// SUMMARY.CREATE RESP command (see
// internal/server/resp/handlers_consolidate.go) and the mirroring
// "consolidate" MCP tool (see internal/mcp/server.go).
//
// Consolidate's dedup pass is deliberately non-destructive: it only decides
// which memories feed the summarization prompt, never which memories exist
// in the store. See Consolidator.Consolidate's doc comment for why, and for
// how a future "delete superseded duplicates" enhancement would build on
// top of this without changing today's behavior.
package consolidate

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/SumitKumar-17/cache-pot/internal/embed"
	"github.com/SumitKumar-17/cache-pot/internal/llm"
	"github.com/SumitKumar-17/cache-pot/internal/memory"
)

// DefaultDedupThreshold is the cosine-similarity cutoff Consolidate uses
// when the caller passes a threshold <= 0. 0.95 is deliberately high: it's
// meant to only merge memories that are near-verbatim restatements of each
// other -- e.g. the same episodic event logged twice with minor wording
// differences (exactly the kind of closeness internal/embed's mock provider
// documents its near-duplicate fixtures landing at, see
// internal/embed/mock.go) -- not memories that are merely topically
// related. That distinction matters here in a way it doesn't for
// MEMORY.SEARCH's own threshold-less "best K matches" default (see
// internal/memory/store.go's Search doc comment): a search ranks, dedup
// merges, and merging on a loose threshold would silently throw away
// distinct information.
const DefaultDedupThreshold = 0.95

// summarySystemPrompt is the fixed system prompt used to build every
// consolidation request. Deliberately simple -- this is not a place for
// prompt-engineering cleverness (see this package's doc comment) -- and the
// mock CompletionProvider ignores the system prompt entirely anyway (see
// internal/llm/mock.go's doc comment).
const summarySystemPrompt = "You are consolidating an AI agent's accumulated memories into a single, concise long-term summary. Read the numbered memory contents below and produce one coherent summary that preserves the important facts and drops redundancy."

// Consolidator turns a set of an agent's memories into a single stored
// long-term summary. It is constructed once (see internal/server/server.go)
// with the shared *memory.Store and llm.CompletionProvider instances used
// everywhere else in the process -- the same "construct once, pass shared
// instances in" discipline internal/mcp.New and internal/semantic.New
// follow.
type Consolidator struct {
	store      *memory.Store
	completion llm.CompletionProvider
}

// New builds a Consolidator backed by store and completion.
func New(store *memory.Store, completion llm.CompletionProvider) *Consolidator {
	return &Consolidator{store: store, completion: completion}
}

// Result reports what one Consolidate call did.
type Result struct {
	// SummaryID is the new long_term memory's id, or "" if there was
	// nothing to summarize (SourceCount == 0) -- not an error, see
	// Consolidate's doc comment.
	SummaryID string

	// SourceCount is how many memories matched (workspaceID, agentID,
	// kind) before dedup.
	SourceCount int

	// DedupedCount is how many of those SourceCount memories survived the
	// dedup pass and were actually fed into the summarization prompt.
	DedupedCount int
}

// Consolidate lists every memory in workspaceID belonging to agentID with
// kind, deduplicates them by embedding cosine similarity, and summarizes
// the deduplicated set into one new memory.LongTerm memory.
//
// Steps:
//
//  1. List every memory matching (workspaceID, agentID, kind) via
//     memory.Store.List, including each one's stored embedding.
//  2. If step 1 found zero memories, return a zero Result (SummaryID == "",
//     SourceCount == 0, DedupedCount == 0) and a nil error: an empty input
//     is a legitimate, common outcome (an agent with no memories of that
//     kind yet), not a caller mistake.
//  3. Dedup (non-destructive -- see below): cluster the listed memories by
//     cosine similarity at or above dedupThreshold (DefaultDedupThreshold
//     if dedupThreshold <= 0). Within each cluster, keep exactly one
//     representative -- the most recently created, by Memory.CreatedAt --
//     and drop the rest from the summarization input only.
//  4. Build a system/user prompt from the deduplicated representative
//     set's Content fields and call CompletionProvider.Complete.
//  5. Store the result via memory.Store.Put as a new memory.LongTerm memory
//     with the same AgentID/WorkspaceID, Content set to the returned
//     summary text, and Metadata recording provenance
//     (consolidated_from_kind, source_count, deduped_count).
//
// IMPORTANT: the dedup pass in step 3 never deletes or modifies anything in
// the store -- it only decides which memories are fed into the
// summarization prompt in step 4. Every one of the SourceCount memories
// listed in step 1 is still present in the store, completely unchanged,
// after Consolidate returns. This is by design: it gives real value (a
// deduped, focused summarization input, instead of a prompt bloated with
// near-identical restatements) with zero data-loss risk. Automatically
// deleting the superseded duplicates after a successful consolidation is a
// reasonable future enhancement, not attempted here.
func (c *Consolidator) Consolidate(ctx context.Context, workspaceID, agentID string, kind memory.Kind, dedupThreshold float64) (Result, error) {
	if dedupThreshold <= 0 {
		dedupThreshold = DefaultDedupThreshold
	}

	all, err := c.store.List(ctx, workspaceID, memory.ListOptions{AgentID: agentID, Kind: &kind})
	if err != nil {
		return Result{}, fmt.Errorf("consolidate: list memories: %w", err)
	}
	sourceCount := len(all)
	if sourceCount == 0 {
		return Result{}, nil
	}

	reps := dedupe(all, dedupThreshold)

	contents := make([]string, len(reps))
	for i, m := range reps {
		contents[i] = fmt.Sprintf("%d. %s", i+1, m.Content)
	}
	userPrompt := "Memories:\n" + strings.Join(contents, "\n")

	summary, _, err := c.completion.Complete(ctx, summarySystemPrompt, userPrompt)
	if err != nil {
		return Result{}, fmt.Errorf("consolidate: complete: %w", err)
	}

	summaryID, err := c.store.Put(ctx, memory.Memory{
		AgentID:     agentID,
		WorkspaceID: workspaceID,
		Kind:        memory.LongTerm,
		Content:     summary,
		Metadata: map[string]string{
			"consolidated_from_kind": kind.String(),
			"source_count":           strconv.Itoa(sourceCount),
			"deduped_count":          strconv.Itoa(len(reps)),
		},
	}, 0)
	if err != nil {
		return Result{}, fmt.Errorf("consolidate: store summary: %w", err)
	}

	return Result{SummaryID: summaryID, SourceCount: sourceCount, DedupedCount: len(reps)}, nil
}

// dedupe clusters ms by cosine similarity at or above threshold, returning
// exactly one representative per cluster: the most recently created member.
// It is non-destructive by construction -- it only ever reads ms and
// returns a new slice; it never touches the store ms came from (see
// Consolidate's doc comment).
//
// The clustering is a simple greedy pass: ms is sorted most-recent-first,
// then each memory is compared in turn against every representative kept
// so far. It's dropped as a duplicate if any already-kept representative is
// within threshold of it, and kept (becoming a new representative)
// otherwise. Processing newest-first means the first member of any cluster
// to be examined -- and therefore the one kept -- is always that cluster's
// most recently created memory, exactly as documented.
func dedupe(ms []memory.Memory, threshold float64) []memory.Memory {
	sorted := make([]memory.Memory, len(ms))
	copy(sorted, ms)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		}
		// Deterministic tiebreak for equal timestamps (e.g. fixtures
		// created within the same mocked instant), so dedupe's output
		// doesn't depend on map/slice iteration order.
		return sorted[i].ID < sorted[j].ID
	})

	var reps []memory.Memory
	for _, m := range sorted {
		duplicate := false
		for _, rep := range reps {
			score := embed.Cosine(m.Embedding, rep.Embedding)
			if !math.IsNaN(score) && score >= threshold {
				duplicate = true
				break
			}
		}
		if !duplicate {
			reps = append(reps, m)
		}
	}
	return reps
}
