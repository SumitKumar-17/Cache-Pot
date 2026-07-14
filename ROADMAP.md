# Release History

Cache-Pot's original build arc shipped as seven releases, all now out. Each release is
additive — later ones build on storage/vector/memory primitives introduced earlier —
and each is honestly scoped rather than compressed to look smaller than it is. v0.6.0
and v0.7.0 in particular were large, cross-cutting efforts and are treated as such
below.

| Version | Date | Headline |
|---|---|---|
| v0.1.0 | 2026-07-12 | Redis-Compatible Core |
| v0.2.0 | 2026-07-12 | Semantic & Prompt-Aware Caching |
| v0.3.0 | 2026-07-12 | Native Vector Store + MCP Server |
| v0.4.0 | 2026-07-12 | Agent Memory + Shared Memory |
| v0.5.0 | 2026-07-13 | Observability, Cost Analytics, Smarter Eviction |
| v0.6.0 | 2026-07-13 | Consolidation & Knowledge Graph |
| v0.7.0 | 2026-07-13 | Multi-Tenancy & Versioning Hardening (current) |

## v0.1.0 — Redis-Compatible Core

The adoption mechanism: a real, drop-in-compatible Redis cache.

- RESP2 protocol, pipelining
- Strings, hashes, lists, sets, sorted sets
- TTL (active + passive expiry)
- Transactions (`MULTI`/`EXEC`/`WATCH`)
- Pub/Sub

**Not supported** (see [docs/architecture/redis-compatibility.md](docs/architecture/redis-compatibility.md)
for the full honest list): RESP3, Lua scripting, replication/cluster, persistence
(RDB/AOF), bitmaps, streams, geo commands.

## v0.2.0 — Semantic & Prompt-Aware Caching

- Embedding-provider abstraction (pluggable: `mock` for local dev/testing, `openai` for
  real embeddings)
- `CACHE.SEMANTIC` — cache LLM answers keyed by embedding similarity, not exact string
- `CACHE.PROMPT` — key by (prompt template + variables + model version); changing a
  template invalidates only affected entries
- `TOOL.CACHE` — cache tool-call results (GitHub/Slack/Jira/etc.) keyed by
  (tool name, canonicalized args), shared across agents

## v0.3.0 — Native Vector Store + MCP Server

- `VECTOR.UPSERT` / `VECTOR.SEARCH` / `VECTOR.DELETE` over a flat (brute-force) index —
  cosine, dot product, euclidean; metadata filtering; namespaces; naive hybrid
  keyword + vector search
- A native MCP server (streamable HTTP, `--mcp-port`) exposing `cache_semantic_set/get`,
  `cache_prompt_set/get`, `tool_cache_set/get`, `store_vector`, `find_similar`, and
  `delete_vector` directly against the same shared engine — no adapter layer.
  `summarize` from the original vision is intentionally **not** exposed: it needs the
  consolidation machinery v0.6.0 introduced, and faking it earlier would have violated
  the project's own honesty policy. `remember`/`recall` landed in v0.4.0 below.

## v0.4.0 — Agent Memory + Shared Memory

- Real memory domain layer: short-term, long-term, episodic, and semantic memory kinds,
  indexed via the v0.3.0 vector store for semantic search
- `MEMORY.PUT` / `MEMORY.GET` / `MEMORY.SEARCH`, `AGENT.REMEMBER` / `AGENT.RECALL`,
  plus `remember`/`recall` MCP tools
- Shared memory across agents and models (Claude, GPT, Gemini, Cursor, etc.) via
  agent/workspace metadata — no artificial per-client silos: `MEMORY.SEARCH` with no
  `AGENT` filter searches every agent's memories in a workspace
- Version is bumped on every `MEMORY.PUT` to the same id; full version history
  retrieval was added later in v0.7.0's `MEMORY.HISTORY`

## v0.5.0 — Observability, Cost Analytics, Smarter Eviction

- Structured metrics: per-cache-type hits/misses, vector-search and agent-memory
  read/write counts, MCP calls (overall and by tool), embedding call instrumentation
  (total/errors/in-flight "queue depth" gauge), and per-command-family latency —
  exposed via `/metrics` (Prometheus text) and `/stats` (JSON) on the MCP port
- Cost analytics: real embedding token/cost tracking from OpenAI's actual usage
  response field, plus an opt-in, caller-reported `COST` option on
  `CACHE.SEMANTIC`/`CACHE.PROMPT` `SET` driving a "money saved" total — a `/dashboard`
  HTML page shows money saved, tokens/cost by model, hit rates, latency, and the most
  expensive cached prompts
- Eviction beyond LRU: a real `--max-entries`-bounded trigger on the core KV engine,
  with a `Weighted` policy combining recency, access frequency, and an optional
  cost/importance hint — approximate below the shard count (documented and verified,
  see `docs/getting-started/observability.md`)

## v0.6.0 — Consolidation & Knowledge Graph

This was the biggest, most research-adjacent piece of work in the whole build —
entity/relationship extraction quality and consolidation judgment calls are genuinely
not a weekend feature. Both halves are real, both built on a capability this release
introduced: `internal/llm.CompletionProvider` — Cache-Pot's first text-*generation*
provider (everything before this only ever produced embeddings).

- **Consolidation (`SUMMARY.CREATE`):** dedup of near-duplicate memories via
  vector similarity (non-destructive by design — nothing is ever deleted from the
  store, only excluded from the summarization input), and real LLM summarization of
  the deduplicated set into a new long-term memory. Not automatic/nightly — an
  on-demand command; scheduling it yourself (cron, a sidecar) is how to get
  nightly-style behavior today.
- **Knowledge Graph (`GRAPH.EXTRACT`/`GRAPH.RELATED`):** real entity/relationship
  extraction from memory content via the completion provider, in-memory graph storage
  with source-memory provenance edges, and breadth-first relationship queries. Quality
  depends entirely on the configured completion provider — the dependency-free `mock`
  provider honestly extracts zero entities/relations (verified, not a bug); real
  extraction needs `--completion-provider openai`.

## v0.7.0 — Multi-Tenancy & Versioning Hardening

Depended on everything before it — this was fundamentally a retrofit of a `workspace`
dimension across every subsystem built up to this point, which is why it landed last
rather than assumed free if bolted on early.

- Real, enforced workspace isolation via `--workspace-credentials`
  (`workspace:password` pairs): `AUTH` now determines which workspace a connection is
  authorized for, and every workspace-scoped command (`MEMORY.*`/`AGENT.*`/
  `VECTOR.*`/`GRAPH.*`) rejects a mismatched workspace with `NOPERM`. Mutually exclusive
  with the single shared `--password` from v0.1.0 — see
  [docs/getting-started/workspaces.md](docs/getting-started/workspaces.md).
  `CACHE.SEMANTIC`/`CACHE.PROMPT`/`TOOL.CACHE` remain global, unscoped caches by design;
  the native MCP server has no auth/workspace enforcement at all — an honest,
  documented gap, not silently worked around.
- Full memory versioning: every `MEMORY.PUT` upsert keeps its prior version (bounded to
  100 per record), retrievable oldest-first via `MEMORY.HISTORY <workspace> <id>
  [LIMIT <n>]` — see
  [docs/commands/versioning.md](docs/commands/versioning.md). History is purged when a
  record expires (deliberate anti-leak choice, not a bug).
