# AGENTS.md — internal/vector

Read the root `AGENTS.md` first; this file only covers what's specific to this package.

## Role

`internal/vector` is Cache-Pot's native vector index: a flat (brute-force), namespace-
partitioned store backing `VECTOR.UPSERT`/`VECTOR.SEARCH`/`VECTOR.DELETE` directly, and
consumed by `internal/memory` (which builds `MemoryStore` on top of a `vector.Store` for
`MEMORY.SEARCH`/`AGENT.RECALL` ranking — see `internal/memory/store.go:144`). Shipped in
v0.3.0 alongside the native MCP server.

## Key types and invariants

- `Store` (`store.go`) is the concrete, currently-wired implementation: a
  `map[namespace]map[id]vecEntry` guarded by a single `sync.RWMutex`. `Index`
  (`index.go`) is a separate, narrower single-collection interface
  (`Upsert`/`Search`/`Delete`, no namespace concept of its own) that a future ANN
  backend could implement per namespace — **`Store` predates this interface and does not
  itself implement it**; don't assume `Index` describes `Store`'s actual API, check
  `store.go`'s method signatures directly (they take an explicit `namespace string`
  first arg, plus richer `Search` options `Index` doesn't have: metadata filter, hybrid
  opts).
- **Three distance metrics** (`DistanceMetric`: `Cosine`, `Dot`, `Euclidean`), computed
  via `internal/embed`'s free functions (`embed.Cosine`/`Dot`/`Euclidean`). Cosine/Dot
  are "higher is better"; Euclidean is a *distance* — "lower is better." `Store.Search`
  handles the sort-direction flip internally (`higherIsBetter := hybrid != nil || metric
  != Euclidean`) — if you add a metric, you must also handle its sort direction here.
- **Metadata filtering**: `Search`'s `filter map[string]string` requires *every*
  key/value pair to match exactly (`matchesFilter`) — no partial/fuzzy matching, no
  numeric comparison operators, string equality only. A nil/empty filter always matches
  everything.
- **Namespacing**: the outer partitioning key for everything. `internal/memory` uses
  workspace ID as the namespace (see `s.vecStore.Search(workspaceID, ...)` in
  `memory/store.go`), so namespace isolation is how per-workspace memory isolation is
  actually implemented under the hood, not a separate mechanism. Cross-namespace search
  is impossible by construction — an unknown/empty namespace search returns `nil`, not
  an error.
- **"Naive hybrid search"** (`HybridOpts`): blends a normalized vector score with a
  keyword-overlap score (`final = Alpha*vecScore + (1-Alpha)*kwScore`). The keyword side
  is deliberately unsophisticated — lowercase+whitespace tokenize, unique-token-overlap
  fraction, **no stemming, no IDF weighting, no phrase matching**. `normalizeForBlend`
  maps Euclidean distance to `1/(1+distance)` specifically so it can be blended on the
  same "higher is better" scale as Cosine/Dot — this transform is explicitly "naive, not
  principled" per its own doc comment. Don't mistake this for a real hybrid-search
  algorithm (BM25 + dense fusion, RRF, etc.) when triaging a ranking complaint — it's
  documented as naive by design.
- Dimension mismatches between the query vector and a stored entry are **skipped
  silently**, not an error — a namespace can in principle hold mixed-dimension vectors
  if misused, and one bad entry shouldn't fail the whole search (`TestSearchDimensionMismatchSkipped`).
  Same silent-skip treatment for a NaN metric result (e.g. Cosine against an all-zero
  vector).
- `Upsert` on an existing `(namespace, id)` **entirely replaces** vector/metadata/text —
  never a merge (`TestUpsertReplacesEntirely`). Stale metadata from a previous upsert is
  never still searchable.
- Result ordering ties are broken by ID ascending, for deterministic output —
  significant if you're writing a test that asserts on result order with equal scores.

## Package-specific gotchas

- A single `sync.RWMutex` guards the *entire* store (all namespaces), not one lock per
  namespace — deliberate, documented in `store.go`: brute-force scanning is the
  bottleneck either way at this stage, so per-namespace locking would add complexity
  without a real concurrency win. Don't "fix" this into per-namespace locks without
  first establishing lock contention is actually the bottleneck.
- `Upsert` copies the input vector (`stored := make(...); copy(...)`) so the caller
  mutating their slice afterward can't corrupt stored state — preserve that copy if you
  refactor `Upsert`.

## Testing

```bash
go test ./internal/vector/...
```

`-race` matters — `Store` has real concurrent-access surface via its `RWMutex`; run it
as part of the repo-wide `go test ./... -race` sweep after touching `store.go`. No
mocks needed: tests construct raw `[]float32` vectors directly and assert on `Search`
ordering/scores (`store_test.go` — see `TestSearchMetricSwitchChangesRanking` for how the
three metrics can rank the same three vectors three different ways, and
`TestHybridSearchChangesRanking` for the keyword-overlap blend flipping a ranking).

## Honest limitations

- This is a **brute-force / flat index — no ANN, linear scan over every entry in the
  namespace on every `Search` call**. `Index` exists as a seam a future ANN backend
  could satisfy, but nothing implements it today; `Store` (the thing actually wired to
  `VECTOR.*` and `internal/memory`) does its own O(n) scan per namespace, per query. This
  scales linearly with namespace size, not sub-linearly — fine for the namespace sizes
  this project currently targets, but a real bottleneck once a single namespace holds
  a very large number of vectors. This is the same "flat index first, ANN later"
  tradeoff `internal/semantic.SemanticCache` makes for its own partitions.
- Hybrid search's keyword-overlap component has no relevance weighting at all (every
  token counts equally regardless of how common/informative it is) — a document that
  happens to share common stopword-like tokens with the query scores the same as one
  sharing rare, meaningful tokens.
