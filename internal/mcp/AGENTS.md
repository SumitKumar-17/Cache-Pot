# internal/mcp

## What this package is

The native MCP (Model Context Protocol) server: it exposes Cache-Pot's caches,
vector store, agent memory, consolidation, and knowledge graph as MCP tools,
operating directly against the *exact same* in-memory instances the RESP
server uses. First shipped in v0.3.0 (cache/vector tools only); grew
`remember`/`recall` in v0.4.0 and `consolidate`/`extract_entities`/
`find_related` in v0.6.0 as those domains landed. There is no adapter layer
and no separate process or storage — an MCP tool call and a RESP command are
two front doors onto the same state. This package is a single file,
`server.go` (~910 lines), plus `server_test.go`.

## The shared-instance invariant (the most important thing here)

`Server` (server.go:97) holds no state of its own beyond the objects passed
into `New`. `internal/server/server.go` builds `resp.Deps` once (around
line 136) — `SemanticCache: semantic.New(provider)`, `PromptCache:
semantic.NewPromptCache()`, `VectorStore: vector.New()`, `MemoryStore:
memoryStore`, `Consolidator: consolidator`, `GraphStore: graphStore`, etc. —
and then, ~40 lines later at server.go:180, calls:

```go
mcp.New(s.deps.SemanticCache, s.deps.PromptCache, s.deps.ToolCache,
    s.deps.VectorStore, s.deps.MemoryStore, s.deps.Consolidator,
    s.deps.GraphStore, s.deps.CompletionProvider, s.metrics, s.analytics)
```

Every argument is read directly back off the already-constructed `resp.Deps`
(and `s.metrics`/`s.analytics`), not reconstructed. `New`'s own doc comment
spells out why: constructing a fresh `semantic.SemanticCache`/`vector.Store`/
`memory.Store`/etc. here "would silently create a second, disconnected memory
space, defeating the entire point of 'no adapter layer'." **If you ever see
or write code that constructs a second instance of any domain store just for
MCP — even one that looks equivalent — that's a bug, not a feature.**
`TestSharedStateWithRESP` in `server_test.go` is the executable proof of this
invariant: it builds a real `resp.Deps` sharing the test's MCP-backing
instances, drives a raw RESP connection over a `net.Pipe`, and asserts writes
through one protocol are visible through the other, in both directions, for
every domain (cache, memory, memory history, consolidation, graph).

## Key types/contracts

- `Server` (server.go:97) — one per process, built by `New`, wraps an
  `sdkmcp.Server` from `github.com/modelcontextprotocol/go-sdk/mcp`.
  `Handler()` returns an `http.Handler` for the *streamable HTTP* transport
  (2025-03-26 MCP spec), meant to be mounted on the same long-lived process's
  `ServeMux` as `/metrics`/`/stats`/`/dashboard` — never spawned per-client
  over stdio, which would give each client its own disconnected memory.
- Every tool method (`cacheSemanticSet`, `remember`, `extractEntities`, ...)
  follows the same shape: `func(ctx, *sdkmcp.CallToolRequest, Input) (*sdkmcp.CallToolResult, Output, error)`,
  registered via `sdkmcp.AddTool` in one `register<Family>Tools()` per family
  (`registerCacheTools`, `registerVectorTools`, `registerMemoryTools`,
  `registerConsolidateTools`, `registerGraphTools`), all called from `New`.
  Adding a new tool to an existing family means adding a method + `AddTool`
  call in that family's `register...` func, not a new top-level file (this
  package deliberately stays one file).
- Defaults are hand-mirrored constants (`defaultSemanticModel`,
  `defaultMemoryWorkspace`, `defaultGraphDepth`, etc., server.go:59-88) kept
  in sync with the RESP handlers' own defaults *by convention, not by shared
  code* — if a RESP handler's default changes, update the matching constant
  here too, or MCP and RESP clients will silently diverge on omitted-param
  behavior.
- Optional-vs-zero disambiguation: fields that need to distinguish "omitted"
  from "explicit zero" (`CacheSemanticGetInput.Threshold`,
  `RecallInput.Threshold`) are `*float64`; fields where that distinction
  doesn't matter (`ConsolidateInput.DedupThreshold`, `FindRelatedInput.Depth`)
  are plain values that fall back on `<= 0`. Match whichever pattern the
  existing field for that concept already uses.
- `extract_entities`' graceful-degradation contract: with the mock
  `CompletionProvider` (the default), `graph.Extract` always returns `(0, 0)`
  — no entities, no relations — and this is surfaced as a normal successful
  result (`ExtractEntitiesOutput{0, 0}`), not an error. Verified by
  `TestExtractEntitiesWithMockDegradesGracefully`. A memory id that doesn't
  exist, by contrast, *is* a real tool error (`TestExtractEntitiesNoSuchMemoryIsToolError`)
  — don't conflate "nothing to extract" with "nothing to operate on."
- `agent_id` in `recall` is always applied as a hard filter — a client can
  only ever recall the agent id it names, never another agent's memories,
  even from the same workspace (`TestRecallDoesNotLeakOtherAgentsMemories`).
  This is an agent-identity boundary, not a security boundary — see the gap
  below.

## Tools currently registered (15, see `TestListTools`)

`cache_semantic_set`, `cache_semantic_get`, `cache_prompt_set`,
`cache_prompt_get`, `tool_cache_set`, `tool_cache_get`, `store_vector`,
`find_similar`, `delete_vector`, `remember`, `recall`, `memory_history`,
`consolidate`, `extract_entities`, `find_related`. `find_similar` covers pure
vector search only — `VECTOR.SEARCH`'s `HYBRID` keyword+vector option is not
exposed as an MCP tool. There is deliberately no `summarize`-as-tool distinct
from `consolidate`, and no MCP-level equivalent of `FLUSHALL`/`FLUSHDB`.

## Conventions specific to this package

- Metrics: every tool method's first line is
  `s.metrics.MCPCallRecorded("<tool_name>")`, followed by the relevant
  domain-specific metric call on success (`SemanticCacheHit`, `MemoryWrite`,
  `GraphExtractionPerformed`, ...) — mirror both when adding a tool, or
  `/metrics`/`/stats` will undercount MCP traffic relative to RESP.
  `s.metrics` and `s.analytics` are the same shared instances RESP records
  into, so money-saved/cost figures reflect both protocols' traffic.
- JSON has no nil-vs-empty-array distinction the way RESP has null-array vs.
  empty-array. Tools like `memory_history` and `find_related` collapse both
  RESP-side outcomes to a plain empty JSON slice — don't try to "fix" this
  into some sentinel value.

## Testing

```
go test ./internal/mcp/... -race
```

`server_test.go`'s `newTestEnv` builds real (non-mocked, except the embedding
provider) instances of every domain store — `embed.NewMock(8)` is the only
double — wires them through `mcp.New` exactly like production, and drives the
server over a real `httptest.Server` + `sdkmcp.StreamableClientTransport`, so
tests exercise the actual wire protocol, not direct Go calls into `Server`.
`TestSharedStateWithRESP` additionally spins up a real `resp.Deps` + RESP
connection over `net.Pipe` using a hand-rolled minimal inline RESP client
(`inlineRESPClient`, bottom of the test file) — reuse it rather than writing
a second one if you need to assert cross-protocol behavior for a new tool.

## Known gap: MCP has no authentication at all

Restating the root AGENTS.md gap because it is the single most
safety-critical fact about this specific package: **every tool that accepts
a `workspace`/`namespace` field trusts it unconditionally.** There is no
`ClientState.authorizedForWorkspace` equivalent, no credential check, nothing
— confirmed by reading every handler in `server.go`: `workspace` is read
straight from the JSON input (falling back to `defaultMemoryWorkspace`) and
passed directly to the store, with zero gating, even when
`--workspace-credentials` is configured for the RESP side. Any MCP client can
read or write any workspace's memories/vectors/graph. See
`docs/getting-started/mcp-server.md` and `docs/getting-started/workspaces.md`.
Do not add a tool that assumes workspace isolation is enforced somewhere
upstream — it isn't, anywhere in this package or its caller.
