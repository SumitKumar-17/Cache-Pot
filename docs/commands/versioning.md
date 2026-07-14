# Versioning Commands

::: tip v0.7.0 — real
This command works today.
:::

| Command | Summary |
|---|---|
| `MEMORY.HISTORY` | Fetch the full version history of a memory item, oldest first |

## MEMORY.HISTORY

```
MEMORY.HISTORY <workspace> <id> [LIMIT <n>]
```

Every [`MEMORY.PUT`](/commands/memory) to an existing id bumps its `version` — v0.4.0
already tracked the version number, but discarded the prior content. v0.7.0 keeps it:
`MEMORY.HISTORY` returns every version of a memory item, **oldest first, ending at the
current version** — the building block for "what did the agent know yesterday."

- `LIMIT` (optional) caps the result to the most recent `N` versions — still returned
  oldest-first among themselves, not the oldest `N` overall.
- Each returned version has the same flat field shape as [`MEMORY.GET`](/commands/memory#memory-get):
  `id`, `agent_id`, `kind`, `content`, `metadata`, `created_at`, `version`.
- Returns a nil array — not empty — for an unknown or expired id, matching
  `MEMORY.GET`'s own missing-key convention.
- History is bounded to the 100 most recent prior versions per memory item (the oldest
  is dropped once exceeded) — a deliberate bound, not an oversight, matching this
  project's pattern of never letting an unbounded structure grow forever (see the TTL
  reaper's sampling or the cost-analytics dashboard's top-expensive-entries list for the
  same philosophy applied elsewhere). If a memory item's current version expires, its
  history is purged along with it — it would otherwise be unreachable dead weight.
- Same [workspace authorization](/getting-started/workspaces) rule as every other
  `MEMORY.*` command.

```bash
redis-cli -p 6380 MEMORY.PUT research-bot "draft 1" ID notes-1
redis-cli -p 6380 MEMORY.PUT research-bot "draft 2" ID notes-1
redis-cli -p 6380 MEMORY.PUT research-bot "final version" ID notes-1

redis-cli -p 6380 MEMORY.HISTORY default notes-1
# -> 3 versions, oldest first: "draft 1", "draft 2", "final version"

redis-cli -p 6380 MEMORY.HISTORY default notes-1 LIMIT 2
# -> the 2 most recent: "draft 2", "final version"
```

`memory_history` is also available as an MCP tool — see the
[MCP server](/getting-started/mcp-server) page.

See [`internal/memory`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/memory)
for the implementation.
