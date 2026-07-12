# Cache-Pot

**Cache-Pot is the memory engine for AI agents.** It's a single, Redis-compatible server
where agents cache, remember, retrieve, share, and reason over information — instead of
developers stitching together Redis + a vector database + Mem0/LangMem-style memory
frameworks + custom MCP glue code.

It speaks the Redis protocol, so adopting it starts with swapping a connection string.
It grows into shared, semantic, versioned memory that every agent and model in your stack
can read and write.

> **Status: Phase 1.** Today, Cache-Pot is a real, working Redis-compatible cache
> (RESP2, core data structures, TTL, transactions, pub/sub). Semantic caching, vector
> search, agent memory, and everything else below is designed and scoped, not yet built.
> See [ROADMAP.md](ROADMAP.md) for the full arc and honest status of each pillar.

## Why not just Redis + Pinecone + Mem0?

| | Redis | + Vector DB | + Mem0/LangMem | + MCP adapters | **Cache-Pot** |
|---|---|---|---|---|---|
| Fast KV cache | ✅ | | | | ✅ |
| Vector search | | ✅ | | | Planned (Phase 3) |
| Agent memory (semantic recall) | | | ✅ | | Planned (Phase 4) |
| Shared memory across agents/models | | | partial | | Planned (Phase 4) |
| MCP-native tool access | | | | ✅ | Planned (Phase 3) |
| Separate services to run & pay for | — | 2 | 3 | 4 | **1** |

Cache-Pot's bet: these are not separate problems. They're one memory engine with
different retrieval modes, and keeping them in one service means no duplicated
infrastructure, no cross-service consistency bugs, and no glue code.

## Quickstart

```bash
docker compose -f deployments/compose/docker-compose.yml up --build
redis-cli -p 6380 PING
redis-cli -p 6380 SET hello world
redis-cli -p 6380 GET hello
```

Or build and run locally:

```bash
go build -o bin/cachepotd ./cmd/cachepotd
./bin/cachepotd --port 6380
```

Cache-Pot works with any existing RESP2 Redis client (go-redis, redis-py, ioredis, etc.)
for the commands it currently implements — see
[docs/commands](docs/commands/index.md) for exactly which ones, and
[docs/architecture/redis-compatibility.md](docs/architecture/redis-compatibility.md)
for what "compatible" honestly means today.

## Documentation

Full docs (vision, getting started, command reference, architecture, roadmap) live in
[`docs/`](docs/) as a VitePress site:

```bash
cd docs && npm install && npm run docs:dev
```

## Roadmap

Cache-Pot is built in seven phases, from a Redis-compatible core to a fully versioned,
multi-tenant memory engine with semantic caching, native vector search, shared agent
memory, cost analytics, and a knowledge graph. See [ROADMAP.md](ROADMAP.md).

## License

[Apache-2.0](LICENSE)
