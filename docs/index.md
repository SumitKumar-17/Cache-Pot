# Cache-Pot

**The memory engine for AI agents.**

Cache-Pot is a single, Redis-compatible server where agents cache, remember,
retrieve, share, and reason over information — instead of developers
stitching together Redis + a vector database + Mem0/LangMem-style memory
frameworks + custom MCP glue code.

It speaks the Redis protocol (RESP2), so adopting it starts with swapping a
connection string. It grows into shared, semantic, versioned memory that
every agent and model in your stack — Claude, GPT, Gemini, Cursor, and
whatever comes next — can read and write.

Redis-compatibility is the *adoption mechanism*: a five-minute drop-in for
anything already speaking RESP2. It is not the whole pitch. The pitch is one
memory engine instead of four separate services.

## Why not just Redis + Pinecone + Mem0 + MCP adapters?

| | Redis | + Vector DB | + Mem0/LangMem | + MCP adapters | **Cache-Pot** |
|---|---|---|---|---|---|
| Fast KV cache | ✅ | | | | ✅ |
| Semantic/prompt/tool-call caching | | | partial | | ✅ |
| Vector search | | ✅ | | | ✅ |
| MCP-native tool access | | | | ✅ | ✅ |
| Agent memory (semantic recall) | | | ✅ | | Planned ([Phase 4](/roadmap/)) |
| Shared memory across agents/models | | | partial | | Planned ([Phase 4](/roadmap/)) |
| Separate services to run & pay for | — | 2 | 3 | 4 | **1** |

Cache-Pot's bet: these are not separate problems. They're one memory engine
with different retrieval modes. Keeping them in one service means no
duplicated infrastructure, no cross-service consistency bugs, and no glue
code.

## Quickstart

```bash
docker compose -f deployments/compose/docker-compose.yml up --build
redis-cli -p 6380 PING
redis-cli -p 6380 SET hello world
redis-cli -p 6380 GET hello
```

See the full [installation](/getting-started/installation) and
[quickstart](/getting-started/quickstart) guides for building from source and
connecting with a client library.

## Status: Phases 1-3

Cache-Pot is being built in seven phases (see the [roadmap](/roadmap/)).
**Today, Phases 1, 2, and 3 are real.**

- ✅ **Real today:** RESP2 protocol, pipelining, strings/hashes/lists/sets/sorted
  sets, TTL (active + passive expiry), transactions (`MULTI`/`EXEC`/`WATCH`),
  Pub/Sub (Phase 1) — `CACHE.SEMANTIC`, `CACHE.PROMPT`, and `TOOL.CACHE`
  (Phase 2) — `VECTOR.UPSERT`/`SEARCH`/`DELETE` and a native
  [MCP server](/getting-started/mcp-server) sharing the same memory (Phase 3).
  See the [command reference](/commands/) for the exact list.
- 🔶 **Designed, not built yet:** shared agent memory, memory versioning, a
  knowledge graph, cost analytics, and multi-tenancy. These are scoped in the
  [roadmap](/roadmap/) but do not exist in the codebase today.

Cache-Pot is also volatile, in-memory-only storage — there is no persistence
yet, and data is lost on restart. Read
[Redis compatibility](/architecture/redis-compatibility) for the full, honest
list of what "Redis-compatible" does and does not mean right now.

## Where to go next

- [Getting Started](/getting-started/installation) — install, run, and connect
- [Commands](/commands/) — the full command reference, generated from the
  authoritative command list
- [Architecture](/architecture/overview) — how the server is put together
- [Roadmap](/roadmap/) — the full seven-phase plan
