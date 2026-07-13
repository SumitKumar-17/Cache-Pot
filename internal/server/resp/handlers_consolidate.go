package resp

import (
	"context"
	"strconv"
	"strings"

	"github.com/SumitKumar-17/cache-pot/internal/memory"
)

// defaultSummaryKind is SUMMARY.CREATE's default KIND when omitted:
// episodic memories are exactly what Phase 6's roadmap describes
// consolidating into long-term summaries ("summarization of episodic-memory
// clusters into long-term memory").
const defaultSummaryKind = memory.Episodic

// RegisterConsolidate adds SUMMARY.CREATE, backed by internal/consolidate's
// Consolidator (see internal/consolidate/consolidate.go) -- itself backed by
// the same memory.Store and llm.CompletionProvider instances every other
// memory/LLM-facing command uses.
func RegisterConsolidate(r *Registry) {
	r.Register(&Command{Name: "SUMMARY.CREATE", MinArgs: 2, MaxArgs: -1, Handler: handleSummaryCreate})
}

// handleSummaryCreate implements:
//
//	SUMMARY.CREATE <agent_id> [WORKSPACE <workspace>] [KIND <kind>]
//	               [DEDUP_THRESHOLD <float>]
//
// Lists every memory belonging to agent_id (in workspace, of the given
// kind), deduplicates near-identical ones by embedding similarity, and
// summarizes the result into one new long_term memory via
// internal/consolidate.Consolidator.Consolidate -- see that method's doc
// comment for the exact dedup/summarize/store steps, and in particular for
// why the dedup pass never deletes anything from the store.
//
// Returns the new summary memory's id as a bulk string, or a nil bulk
// string (NullBulk) if agent_id has zero memories of kind in workspace --
// "nothing to summarize" is a legitimate outcome, not an error, matching
// how CACHE.SEMANTIC GET's miss case returns nil rather than erroring.
func handleSummaryCreate(cs *ClientState, args []string) Reply {
	agentID := args[1]

	workspace := defaultMemoryWorkspace
	kind := defaultSummaryKind
	var dedupThreshold float64

	for i := 2; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return Err(ErrSyntaxMsg)
		}
		switch strings.ToUpper(args[i]) {
		case "WORKSPACE":
			workspace = args[i+1]
		case "KIND":
			k, ok := memory.ParseKind(args[i+1])
			if !ok {
				return Err(ErrSyntaxMsg)
			}
			kind = k
		case "DEDUP_THRESHOLD":
			t, err := strconv.ParseFloat(args[i+1], 64)
			if err != nil {
				return Err(ErrNotFloatMsg)
			}
			dedupThreshold = t
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	result, err := cs.Deps.Consolidator.Consolidate(context.Background(), workspace, agentID, kind, dedupThreshold)
	if err != nil {
		return Err("ERR " + err.Error())
	}
	cs.Deps.Metrics.ConsolidationPerformed()
	cs.Deps.Metrics.MemoriesDeduped(int64(result.SourceCount - result.DedupedCount))

	if result.SourceCount == 0 {
		return NullBulk()
	}
	return BulkString(result.SummaryID)
}
