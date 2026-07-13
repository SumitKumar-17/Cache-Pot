# Roadmap

Cache-Pot was built in seven phases, all now complete. Each phase is
additive — later phases build on storage/vector/memory primitives introduced
earlier — and each is honestly scoped rather than compressed to look smaller
than it is. Phases 6 and 7 in particular were large, cross-cutting efforts
and are treated as such below.

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
- Version is bumped on every `MEMORY.PUT` to the same id; full version
  history retrieval was added later in Phase 7's `MEMORY.HISTORY`

See the [agent memory commands](/commands/memory) page.

## Phase 5 — Observability, Cost Analytics, Smarter Eviction ✅

- Structured metrics: per-cache-type hits/misses, vector-search and
  agent-memory read/write counts, MCP calls (overall and by tool), embedding
  call instrumentation (total/errors/in-flight "queue depth" gauge), and
  per-command-family latency — exposed via `/metrics` (Prometheus text) and
  `/stats` (JSON) on the MCP port
- Cost analytics: real embedding token/cost tracking from OpenAI's actual
  usage response field, plus an opt-in, caller-reported `COST` option on
  `CACHE.SEMANTIC`/`CACHE.PROMPT` `SET` driving a "money saved" total — a
  `/dashboard` HTML page shows money saved, tokens/cost by model, hit rates,
  latency, and the most expensive cached prompts
- Eviction beyond LRU: a real `--max-entries`-bounded trigger on the core KV
  engine, with a `Weighted` policy combining recency, access frequency, and
  an optional cost/importance hint — approximate below the shard count, see
  [Observability](/getting-started/observability#eviction) for the honest
  caveat and verified numbers

See the [Observability](/getting-started/observability) page.

## Phase 6 — Consolidation & Knowledge Graph ✅ (largest phase)

This was the biggest, most research-adjacent piece of work in the roadmap —
entity/relationship extraction quality and consolidation judgment calls are
genuinely not a weekend feature. Both halves are real now, both built on a
brand-new capability this phase introduced: `internal/llm.CompletionProvider`
— [LLM completions](/getting-started/completions), Cache-Pot's first
text-*generation* provider (everything before Phase 6 only ever produced
embeddings).

- **6a — Consolidation (`SUMMARY.CREATE`):** dedup of near-duplicate memories
  via vector similarity (non-destructive by design — nothing is ever deleted
  from the store, only excluded from the summarization input), and real
  LLM summarization of the deduplicated set into a new long-term memory.
  **Not automatic/nightly** — this is an on-demand command; running it on a
  schedule yourself (cron, a sidecar) is how to get nightly-style behavior
  today.
- **6b — Knowledge Graph (`GRAPH.EXTRACT`/`GRAPH.RELATED`):** real
  entity/relationship extraction from memory content via the completion
  provider, in-memory graph storage with source-memory provenance edges, and
  breadth-first relationship queries. **Quality depends entirely on the
  configured completion provider** — the dependency-free `mock` provider
  honestly extracts zero entities/relations (verified, not a bug); real
  extraction needs `--completion-provider openai`.

See the [Consolidation & Knowledge Graph commands](/commands/graph) page.

## Phase 7 — Multi-Tenancy & Versioning Hardening ✅ (cross-cutting)

Depended on everything before it — this was fundamentally a retrofit of a
`workspace` dimension across every subsystem built in Phases 1-6, which is
why it was scoped last rather than assumed free if bolted on early.

- **Workspace isolation:** real, enforced via `--workspace-credentials`
  (`workspace:password` pairs) — `AUTH` now determines which workspace a
  connection is authorized for, and every workspace-scoped command
  (`MEMORY.*`/`AGENT.*`/`VECTOR.*`/`GRAPH.*`) rejects a mismatched workspace
  with `NOPERM`. Mutually exclusive with Phase 1's single shared
  `--password`. `CACHE.SEMANTIC`/`CACHE.PROMPT`/`TOOL.CACHE` remain global,
  unscoped caches by design; the native MCP server has no auth/workspace
  enforcement at all — an honest, documented gap. See
  [Workspaces & Multi-Tenancy](/getting-started/workspaces).
- **Full memory versioning:** every `MEMORY.PUT` upsert keeps its prior
  version (bounded to 100 per record), retrievable oldest-first via
  `MEMORY.HISTORY <workspace> <id> [LIMIT <n>]`. History is purged when a
  record expires (deliberate anti-leak choice, not a bug).

See the [versioning commands](/commands/versioning) and
[Workspaces & Multi-Tenancy](/getting-started/workspaces) pages.
