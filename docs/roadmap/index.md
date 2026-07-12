# Roadmap

Cache-Pot is being built in seven phases. Each phase is additive — later
phases build on storage/vector/memory primitives introduced earlier — and
each is honestly scoped rather than compressed to look smaller than it is.
Phases 6 and 7 in particular are large, cross-cutting efforts and are
treated as such below.

Dependency chain: `1 → 2 → 3 → 4 → 5 → 6 → 7`.

## Phase 1 — Redis-Compatible Core ✅

The adoption mechanism: a real, drop-in-compatible Redis cache.

- RESP2 protocol, pipelining
- Strings, hashes, lists, sets, sorted sets
- TTL (active + passive expiry)
- Transactions (`MULTI`/`EXEC`/`WATCH`)
- Pub/Sub

**Not in Phase 1** (see [Redis Compatibility](/architecture/redis-compatibility)
for the full honest list): RESP3, Lua scripting, replication/cluster,
persistence (RDB/AOF), bitmaps, streams, geo commands.

## Phase 2 — Semantic & Prompt-Aware Caching ✅

- Embedding-provider abstraction (pluggable: `mock` for local dev/testing,
  `openai` for real embeddings)
- `CACHE.SEMANTIC` — cache LLM answers keyed by embedding similarity, not
  exact string
- `CACHE.PROMPT` — key by (prompt template + variables + model version);
  changing a template invalidates only affected entries
- `TOOL.CACHE` — cache tool-call results (GitHub/Slack/Jira/etc.) keyed by
  (tool name, canonicalized args), shared across agents

See the [semantic cache](/commands/semantic-cache) and
[tool cache](/commands/tool-cache) command pages.

## Phase 3 — Native Vector Store + MCP Server ✅

- `VECTOR.UPSERT` / `VECTOR.SEARCH` / `VECTOR.DELETE` over a flat
  (brute-force) index — cosine, dot product, euclidean; metadata filtering;
  namespaces; naive hybrid keyword + vector search
- A native MCP server (streamable HTTP, `--mcp-port`) exposing
  `cache_semantic_set/get`, `cache_prompt_set/get`, `tool_cache_set/get`,
  `store_vector`, `find_similar`, and `delete_vector` directly against the
  same shared engine — no adapter layer. `summarize` from the original
  vision is intentionally **not** exposed yet: it needs Phase 6
  consolidation machinery that doesn't exist, and faking it would violate
  the project's own honesty policy. `remember`/`recall` are now real, added
  alongside Phase 4 below.

See the [vector commands](/commands/vector) and
[MCP server](/getting-started/mcp-server) pages.

## Phase 4 — Agent Memory + Shared Memory ✅

- Real memory domain layer: short-term, long-term, episodic, and semantic
  memory kinds, indexed via the Phase 3 vector store for semantic search
- `MEMORY.PUT` / `MEMORY.GET` / `MEMORY.SEARCH`, `AGENT.REMEMBER` /
  `AGENT.RECALL`, plus `remember`/`recall` MCP tools
- Shared memory across agents and models (Claude, GPT, Gemini, Cursor, etc.)
  via agent/workspace metadata — no artificial per-client silos:
  `MEMORY.SEARCH` with no `AGENT` filter searches every agent's memories in a
  workspace
- Version is bumped on every `MEMORY.PUT` to the same id, but full version
  history retrieval is deliberately not built here — that's Phase 7's
  `MEMORY.HISTORY`

See the [agent memory commands](/commands/memory) page.

## Phase 5 — Observability, Cost Analytics, Smarter Eviction *(planned)*

- Structured event pipeline: hits/misses/semantic hits/memory reads/MCP
  calls/vector searches/latency/embedding queue depth
- Cost analytics: tokens, latency, cost, cache-hit-or-not, embedding cost,
  model used — a dashboard of money saved, hit rate, and most expensive
  prompts
- Eviction beyond LRU: pluggable policies scoring by frequency, recreation
  cost, token cost, importance, and user-defined priority

## Phase 6 — Consolidation & Knowledge Graph *(planned — largest phase)*

This phase is split into two sub-milestones because it is genuinely the
biggest, most research-adjacent piece of work in the roadmap —
entity/relationship extraction quality and consolidation judgment calls are
not a weekend feature.

- **6a — Consolidation:** nightly dedup of near-duplicate memories via
  vector similarity, summarization of episodic-memory clusters into
  long-term memory
- **6b — Knowledge Graph:** entity/relationship extraction from memory
  content, graph storage, `GRAPH.RELATED` relationship queries

See the [knowledge graph commands](/commands/graph) page.

## Phase 7 — Multi-Tenancy & Versioning Hardening *(planned — cross-cutting)*

Depends on everything before it — this is fundamentally a retrofit of a
`workspace` dimension across every subsystem built in Phases 1-6, which is
why it's scoped last rather than assumed free if bolted on early.

- Workspace isolation across KV keyspace, vector namespaces, memory store,
  and graph
- Full memory versioning: every write retrievable by history
  (`MEMORY.HISTORY` / point-in-time reads — "what did the agent know
  yesterday")
- Per-workspace auth/ACL (growing from Phase 1's single shared password)

See the [versioning commands](/commands/versioning) page.
