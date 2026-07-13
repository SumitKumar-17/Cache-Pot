# Observability, Cost Analytics & Eviction

::: tip Phase 5 — real
Metrics, cost analytics, the dashboard, and bounded eviction all work today.
:::

## Metrics

Every RESP command and MCP tool call is recorded: per-cache-type hits/misses
(semantic, prompt, tool), vector search and agent-memory read/write counts, MCP calls
(overall and by tool), embedding-provider calls (total, errors, and an in-flight
gauge — the "embedding queue depth" signal), evictions, and per-command-family latency
(count/average/max).

Two endpoints on the [MCP server's](/getting-started/mcp-server) port (`--mcp-port`,
default `6381` — these share that listener rather than getting a dedicated one):

- **`GET /metrics`** — Prometheus text exposition format, hand-rolled (no
  `prometheus/client_golang` dependency), ready to scrape.
- **`GET /stats`** — the same data as JSON, plus the cost-analytics fields below.

```bash
curl http://localhost:6381/metrics
curl http://localhost:6381/stats
```

## Cost analytics

`internal/analytics` tracks two things, both **real, never fabricated**:

1. **Embedding token/cost usage** — captured from the OpenAI API's actual
   `usage.total_tokens` response field when `--embed-provider openai` is in use, priced
   against a small published-pricing table (`text-embedding-3-small`,
   `text-embedding-3-large`, `text-embedding-ada-002`). This table is a snapshot of
   published pricing as of when it was written — verify current pricing yourself
   rather than treating it as always up to date. The dependency-free `mock` provider
   makes no real API call, so it reports no usage — `/stats`'s `embedding_by_model`
   simply stays empty rather than guessing a cost.
2. **Money saved** — driven entirely by an **opt-in** `COST <dollars>` option you can
   add to `CACHE.SEMANTIC SET` and `CACHE.PROMPT SET`, representing what the cached
   response cost you to originally produce (e.g. the LLM completion cost). A later hit
   on that entry records `COST` as savings. An entry with no `COST` contributes exactly
   `0` — never an estimate.

```bash
redis-cli -p 6380 CACHE.SEMANTIC SET "What is Kubernetes?" "K8s is a container orchestrator." COST 0.015
redis-cli -p 6380 CACHE.SEMANTIC GET "What is Kubernetes?"
# hit -> records $0.015 saved
```

`COST` is deliberately not available on `TOOL.CACHE`: "money saved" here specifically
means "avoided an LLM completion call," which is what `CACHE.SEMANTIC`/`CACHE.PROMPT`
exist for — a tool call's cost is a different and much more varied cost model, out of
scope for this pass.

## Dashboard

**`GET /dashboard`** renders a plain, server-side HTML page (no JS framework, no
external CSS) showing money saved, embedding tokens/cost by model, per-cache hit
rates, per-family latency, and the most expensive cached prompts that have been hit at
least once. It's an operator/debug view, not a product surface.

```bash
open http://localhost:6381/dashboard   # or just curl it
```

## Eviction

`internal/storage/memstore` — the core Redis-compatible key/value engine — can bound
its total live key count and evict once that bound is exceeded:

```bash
./bin/cachepotd --max-entries 5000 --eviction-policy weighted
```

- `--max-entries` (default `0` = unlimited) is a **server-wide** total across every
  key, not per-workspace.
- `--eviction-policy`: `lru` (default — evicts the least-recently-used candidate) or
  `weighted` — a composite score combining recency, access frequency, and (where
  available) a cost/importance hint, so a frequently-accessed-but-not-recently-touched
  key can outlive a recently-touched-but-rarely-used one. Any other value fails at
  startup rather than silently falling back.
- This bound applies to the core KV engine only (plain Redis-style keys) — the
  semantic/prompt/tool caches, vector store, and agent memory each still only expire by
  TTL, with no size bound of their own yet.

::: warning Approximate, not exact, below the shard count
Eviction samples a bounded number of keys **in the shard receiving the new write**
(the same "sample, don't scan everything" approach the TTL reaper already uses) rather
than scanning the whole keyspace. In practice this means the resident-key floor is
`max(--max-entries, roughly one key per populated shard)` — with the default 32
shards, setting `--max-entries` below ~32 will NOT bound the keyspace anywhere near
that low; it converges closer to the shard count instead. Once `--max-entries` is
comfortably above the shard count (the realistic use case — bounding a cache at
thousands of keys, say), the bound holds close to exact. This was verified directly:
300 inserts with `--max-entries 50` converged to 54 resident keys with exactly 246
evictions accounted for.
:::
