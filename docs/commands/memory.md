# Agent Memory Commands

::: tip Phase 4 — real
These commands work today, built on the [vector store](/commands/vector) from Phase 3
for semantic search. See [MCP Server](/getting-started/mcp-server) for the equivalent
`remember`/`recall` MCP tools.
:::

| Command | Summary |
|---|---|
| `MEMORY.PUT` | Store a memory item for an agent in a workspace |
| `MEMORY.GET` | Retrieve a memory item by id |
| `MEMORY.SEARCH` | Semantic search over a workspace's memories, across all agents unless filtered |
| `AGENT.REMEMBER` | High-level helper to store a memory from raw content |
| `AGENT.RECALL` | High-level helper to recall only this agent's own memories |

This is where Cache-Pot becomes an actual memory engine rather than a cache: a real
memory domain layer with short-term, long-term, episodic, and semantic memory kinds,
indexed for semantic search via the same flat vector index Phase 3 introduced.

## MEMORY.PUT

```
MEMORY.PUT <agent_id> <content> [ID <id>] [WORKSPACE <workspace>] [KIND short_term|long_term|episodic|semantic] [METADATA <metadata_json>] [TTL <seconds>]
```

- `WORKSPACE` defaults to `default`, `KIND` defaults to `long_term`.
- `ID`: omit to generate a new memory id (returned as a bulk string); pass one to
  **upsert** that exact memory — its `version` is bumped, replacing the previous
  content/embedding. Full version history isn't retrievable yet (that's
  [Phase 7](/roadmap/)'s `MEMORY.HISTORY`) — only the current version is kept.
- `METADATA` is a JSON object of arbitrary key/value metadata; invalid JSON is a RESP
  error.
- Content is embedded via the server's [configured embedding provider](/getting-started/configuration)
  so `MEMORY.SEARCH` can later rank it by meaning.

```bash
redis-cli -p 6380 MEMORY.PUT research-bot "User prefers concise, bullet-point summaries"
# "a1b2c3..."   (generated memory id)
```

## MEMORY.GET

```
MEMORY.GET <workspace> <id>
```

Returns a flat array of field/value pairs — `id`, `agent_id`, `kind`, `content`,
`metadata` (JSON string), `created_at` (RFC3339), `version` — or a nil array if the id
doesn't exist or has expired.

## MEMORY.SEARCH

```
MEMORY.SEARCH <workspace> <query> [AGENT <agent_id>] [KIND <kind>] [K <n>] [THRESHOLD <float>] [WITHSCORES]
```

**This is the payoff of "shared memory, no silos":** without `AGENT`, this searches
*every* agent's memories in the workspace, ranked by semantic similarity to `<query>` —
one agent's memory is recallable by a completely different agent or model. Pass `AGENT`
to scope it to one agent's own memories (or use [`AGENT.RECALL`](#agent-recall) below,
which does this more ergonomically). `KIND` filters to one memory kind. Reply shape
matches [`VECTOR.SEARCH`](/commands/vector): ids (or id+score pairs with `WITHSCORES`),
best match first, empty array if nothing matches.

```bash
redis-cli -p 6380 MEMORY.PUT alice "Alice likes Python and Go" KIND long_term
redis-cli -p 6380 MEMORY.PUT bob   "Bob prefers Rust for systems work" KIND long_term
redis-cli -p 6380 MEMORY.SEARCH default "what languages does the user like?" WITHSCORES
# returns memories from BOTH alice and bob — no per-agent silo
```

## AGENT.REMEMBER

```
AGENT.REMEMBER <agent_id> <content> [WORKSPACE <workspace>] [KIND ...] [METADATA <metadata_json>] [TTL <seconds>]
```

Identical to `MEMORY.PUT` without the `ID` option — always stores a brand-new memory and
returns its generated id. The simple "just remember this" entry point.

## AGENT.RECALL

```
AGENT.RECALL <agent_id> <query> [WORKSPACE <workspace>] [KIND <kind>] [K <n>] [THRESHOLD <float>] [WITHSCORES]
```

Identical to `MEMORY.SEARCH`, except `<agent_id>` is always applied as the `AGENT`
filter — this **never** returns another agent's memory, even when its content is a close
semantic match. The ergonomic, always-scoped counterpart to `MEMORY.SEARCH`'s
workspace-wide search.

```bash
redis-cli -p 6380 AGENT.RECALL alice "what languages does the user like?"
# only alice's own memory — bob's is never returned here
```

See [`internal/memory`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/memory)
for the implementation.
