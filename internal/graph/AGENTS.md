# internal/graph

Cache-Pot's knowledge graph: `GRAPH.EXTRACT` (entity/relationship extraction from a
stored memory, via `internal/llm.CompletionProvider`) and `GRAPH.RELATED` (BFS traversal
over the resulting graph). Shipped in v0.6.0 alongside `internal/consolidate`, both built
on `internal/llm.CompletionProvider` — Cache-Pot's first text-*generation* provider.

## Files

- `graph.go` — `Store`: workspace-partitioned, in-memory directed labeled graph (`Node`,
  `Edge`, `UpsertNode`, `UpsertEdge`, `GetNode`, `Related`). Pure data structure, no
  upstream dependency — mirrors `internal/vector.Store`'s "leaf package" shape.
- `extract.go` — `Extract(ctx, completion llm.CompletionProvider, store *Store, ...)`:
  the orchestration that calls the completion provider, parses its JSON response, and
  writes nodes/edges into `Store`. Depends on `internal/llm` but *not* on
  `internal/memory` — callers (RESP/MCP) fetch the memory themselves and pass in plain
  `memoryID`/`memoryContent` strings.
- `store_test.go` — pure `Store` behavior (upsert idempotency, BFS depth, undirected
  traversal, workspace isolation).
- `extract_test.go` — `Extract`'s parsing/degradation contract.

## Key types and the mock-degradation contract

- `Store` is safe for concurrent use via a single `sync.RWMutex` — not a hot path, so one
  coarse lock is intentional, not a shortcut.
- `Edge` identity for `UpsertEdge`'s replace-in-place semantics is `(FromID, ToID, Label)`
  — `Weight` is excluded from identity, so re-upserting the same edge with a different
  `Weight` replaces the weight rather than creating a parallel edge.
- `Related` treats every edge as **undirected** for reachability (querying "related to
  Alice" must surface things Alice is the *target* of, not just the source of), excludes
  the start node itself, and reports each reachable node only once, paired with the edge
  from its first (shortest-hop) discovery — plain BFS semantics. `depth <= 0` silently
  defaults to `1` inside `Store.Related` itself (the RESP handler treats an explicit
  non-positive `DEPTH` as caller error instead — see `handlers_graph.go`).
- **The most important contract in this package**: `Extract` against a completion
  provider that can't produce its exact requested JSON shape — which is exactly what
  `internal/llm`'s mock provider does (it echoes a truncated slice of the input, never
  valid JSON) — returns `(0, 0, nil)`, not an error, and leaves the graph store
  completely untouched. This is proven by
  `extract_test.go:TestExtractWithMockDegradesGracefully`, which runs `Extract` against
  the real `llm.NewMock()` (not a stand-in fake) and asserts zero counts, nil error, and
  no memory-provenance node created. Only a genuine failure to *call* `Complete` at all
  (e.g. a real provider's network error) is reported as a non-nil error
  (`TestExtractCompletionErrorPropagates`). A well-formed-but-empty response
  (`{"entities":[],"relations":[]}`) degrades the same way — see
  `TestExtractEmptyEntitiesGracefulZero`.
- The memory-provenance node (`"memory:" + memoryID`, via the internal `memoryNodeID`
  helper) and its `"mentions"` edges to every extracted entity are only added if at least
  one entity was actually extracted — never add one speculatively.
- `internal/server/resp/handlers_graph.go` and `internal/mcp/server.go` both call
  `graph.Extract` against `cs.Deps.GraphStore`/`s.graphStore` respectively — the *same*
  `*graph.Store` instance, constructed once in `internal/server/server.go` (`graphStore
  := graph.New()`) and threaded into both. Never construct a second `graph.New()` for
  either layer.

## Conventions/gotchas specific to this package

- Entity ids are expected to be short, stable, lowercase, underscore-separated strings
  (e.g. `"redis"`, `"project_a"`) — this is a prompt-engineering convention baked into
  `extractSystemPrompt`, not something `Store` enforces; `Store` accepts any string id.
- Dangling edges (referencing a node that was never upserted, or was upserted then never
  re-added) are tolerated by `Related`: the far endpoint is marked visited so it isn't
  reprocessed, but it is never included in results, since `Related` only ever returns
  real nodes.
- `extract.go`'s doc comments explain *why* the mock degrades this way in detail — read
  them before changing `extractSystemPrompt` or the parsing logic; the JSON shape is
  load-bearing for graceful degradation, not incidental.

## Testing

```
go test ./internal/graph/... -race
```

No build tags, no external services. `extract_test.go` uses two doubles: the real
`llm.NewMock()` (to prove graceful degradation against the actual mock, not a stand-in)
and a local `fakeCompletionProvider` (to return arbitrary fixed/erroring responses and
exercise the parsing/error paths that a real LLM's variability would make hard to pin
down deterministically).

## Known limitations

- Extraction quality is entirely dependent on the configured `CompletionProvider` — see
  root `AGENTS.md`'s gaps list. This package's honest floor with the default `mock`
  provider is zero entities/relations, always, by design.
- `Related`'s BFS re-scans every edge in the workspace on every hop (`for _, e :=
  range ws.edges`) rather than maintaining an adjacency index — fine at the scale this
  package targets, but a real cost if a workspace's graph grows very large.
