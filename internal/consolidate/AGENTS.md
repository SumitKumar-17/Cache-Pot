# internal/consolidate

## What this package does

Implements `SUMMARY.CREATE`: turns a cluster of an agent's accumulated memories
(typically episodic) into a single new `long_term` memory via
`internal/llm.CompletionProvider`. It's the summarization half of v0.6.0
("Consolidation & Knowledge Graph") — the sibling of `internal/graph`, and the
first consumer of `internal/llm` (Cache-Pot's first text-*generation*
provider; everything before it only produced embeddings). Backs both the RESP
`SUMMARY.CREATE` command (`internal/server/resp/handlers_consolidate.go`) and
the MCP `consolidate` tool (`internal/mcp/server.go`) — both call the exact
same shared `*Consolidator`.

## Key types and contracts

- `Consolidator` (`New(store *memory.Store, completion llm.CompletionProvider) *Consolidator`)
  — the only exported type. Constructed once in `internal/server/server.go`
  and threaded into both `resp.Deps` and `mcp.New(...)`; never construct a
  second one just to back MCP (see root AGENTS.md's shared-instance rule).
- `Consolidator.Consolidate(ctx, workspaceID, agentID string, kind memory.Kind, dedupThreshold float64) (Result, error)`
  is the entire public surface. Steps: `memory.Store.List` by
  `(workspaceID, agentID, kind)` → dedupe by cosine similarity → build a
  numbered-list prompt from survivors' `Content` → `CompletionProvider.Complete`
  → `memory.Store.Put` the result as a new `memory.LongTerm` memory with
  provenance in `Metadata` (`consolidated_from_kind`, `source_count`,
  `deduped_count`).
- **Zero memories is not an error.** If `List` finds nothing, `Consolidate`
  returns a zero `Result` (`SummaryID == ""`) and `nil` error — "nothing to
  summarize" is a legitimate outcome. Don't turn this into an error path.
- **The dedup pass is non-destructive** (repo-wide convention, see root
  AGENTS.md) — `dedupe` only decides what feeds the summarization prompt; it
  never calls `Delete`/mutates the store. Every source memory is still
  present, unchanged, after `Consolidate` returns. This is asserted by a real
  test (`TestConsolidateDedupsNearDuplicatesNonDestructively`), not just
  documented — if you touch `dedupe`, keep that test green.
- **Dedup keeps the *most recent* representative per cluster**, not the
  first-seen or highest-similarity one: `dedupe` sorts most-recent-first (by
  `CreatedAt`, ID as tiebreak for equal timestamps) and does a greedy
  single-pass cluster assignment. Changing the sort order silently changes
  which memory's wording survives into the summary prompt.
- `DefaultDedupThreshold = 0.95` is deliberately high — near-verbatim
  restatements only, not "topically related." A `dedupThreshold <= 0` passed
  into `Consolidate` falls back to this constant; it's not validated as a
  cosine value in `[-1,1]` beyond that.
- `Result{SummaryID, SourceCount, DedupedCount}` — `SourceCount` is pre-dedup,
  `DedupedCount` is post-dedup (i.e. how many representatives were actually
  summarized). `SourceCount - DedupedCount` is exactly the count fed into
  `Metrics.MemoriesDeduped` by the RESP handler.

## Conventions/gotchas specific to this package

- `summarySystemPrompt` is intentionally plain — not a place for prompt
  engineering, and the mock `CompletionProvider` ignores the system prompt
  entirely (see `internal/llm/mock.go`).
- `SUMMARY.CREATE` is on-demand only — there is no scheduler/cron inside this
  package or anywhere else in the repo; "nightly consolidation" means the
  caller runs `SUMMARY.CREATE` themselves on a schedule.
- `dedupe` is unexported but tested directly (not just through
  `Consolidate`) — `consolidate_test.go` constructs `memory.Memory` values by
  hand with explicit `CreatedAt`/`Embedding` fields to test tie-breaking,
  since `memory.Store.Put` always stamps `CreatedAt` with the real wall clock
  and offers no override.
- This package depends on `*memory.Store` concretely, not an interface — if
  you need to swap the backing store for a test double, there isn't one;
  tests use a real `memory.New(embed.NewMock(n))`.

## Testing

```
go test ./internal/consolidate/...
```
`-race` isn't load-bearing here (no goroutines spawned in this package), but
running the full suite with `-race` per root AGENTS.md is still expected
before calling anything done. Tests use `embed.NewMock(8)` and `llm.NewMock()`
— no network calls, fully deterministic. `llm.NewMock()`'s `Complete` prefixes
output with `"[mock completion, no real generation] "`, which
`TestConsolidateDedupsNearDuplicatesNonDestructively` asserts on directly —
don't rely on the mock's exact output text elsewhere.

## Known limitations

- Summary quality is entirely dependent on the configured `CompletionProvider`
  — the `mock` provider produces no real summarization (see root AGENTS.md's
  gaps list; this is the same honesty constraint `internal/graph` documents
  for `GRAPH.EXTRACT`).
- `dedupe`'s clustering is a single greedy pass over a sorted list, not a
  proper transitive-closure clustering — a memory can fail to merge with a
  cluster if it's within threshold of a *dropped* duplicate but not of that
  cluster's kept representative. This is an accepted tradeoff for a small,
  bounded-per-agent memory count (`memory.Store.List` is not paginated by this
  package), not a bug.
