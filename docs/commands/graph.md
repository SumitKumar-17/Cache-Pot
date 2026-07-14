# Consolidation & Knowledge Graph Commands

::: tip v0.6.0 — real
These commands work today. Their quality depends entirely on the configured
[completion provider](/getting-started/completions) — see the honesty note below
before relying on them for anything beyond the dependency-free `mock` provider's
plumbing-level testing.
:::

| Command | Summary |
|---|---|
| `SUMMARY.CREATE` | Dedup + LLM-summarize an agent's memories into one long-term memory |
| `GRAPH.EXTRACT` | LLM-extract entities/relationships from a memory into the knowledge graph |
| `GRAPH.RELATED` | Find nodes related to a given node in the knowledge graph |

This was the biggest, most research-adjacent piece of work in the whole build —
entity/relationship extraction quality and consolidation judgment calls are genuinely
not a weekend feature. It's split into two related capabilities, both real now, both
built on [LLM completions](/getting-started/completions) — Cache-Pot's first
text-*generation* capability (everything before v0.6.0 only ever produced embeddings).

## SUMMARY.CREATE — consolidation

```
SUMMARY.CREATE <agent_id> [WORKSPACE <workspace>] [KIND <kind>] [DEDUP_THRESHOLD <float>]
```

- Lists every memory for `<agent_id>` of `KIND` (default `episodic`) in `WORKSPACE`
  (default `default`).
- **Dedup**: clusters near-duplicates by cosine similarity at or above
  `DEDUP_THRESHOLD` (default `0.95`), keeping the most recent per cluster —
  **for the summarization input only**. Nothing is ever deleted from the store; this is
  fully non-destructive by design.
- **Summarize**: sends the deduplicated set to the completion provider, stores the
  result as a new `long_term` memory (with metadata recording source/deduped counts),
  and returns its id.
- Returns a nil reply (not an error) if there was nothing to summarize.

```bash
redis-cli -p 6380 AGENT.REMEMBER research-bot "debugged an auth bug: expired JWT" KIND episodic
redis-cli -p 6380 AGENT.REMEMBER research-bot "debugged another auth bug: expired JWT again" KIND episodic
redis-cli -p 6380 SUMMARY.CREATE research-bot
# -> a new long_term memory id; MEMORY.GET it to see the generated summary
```

There is no automatic/nightly scheduling — this is an on-demand command. Running it on
a timer yourself (e.g. cron, a sidecar) is the way to get the "nightly consolidation"
behavior described in the original vision; Cache-Pot doesn't run a background
scheduler for this yet.

## GRAPH.EXTRACT — populate the knowledge graph

```
GRAPH.EXTRACT <workspace> <memory_id>
```

Fetches the memory, asks the completion provider to extract entities and relationships
from its content as JSON, and adds them to the graph — plus a `memory:<id>` node with
`mentions` edges to every extracted entity, so the graph stays traceable back to its
source. Returns `[entities_added, relations_added]`. Errors if the memory doesn't
exist.

::: warning The mock provider extracts nothing — honestly
`internal/llm`'s dependency-free `mock` provider does no real language understanding.
It cannot produce the structured JSON extraction needs, so `GRAPH.EXTRACT` against it
**always returns `[0, 0]`** — a graceful "nothing extracted," not an error and not a
fabricated graph. This is deliberate and tested. Real entity/relationship extraction
requires `--completion-provider openai` (see
[configuration](/getting-started/configuration)).
:::

## GRAPH.RELATED — query the knowledge graph

```
GRAPH.RELATED <workspace> <node_id> [DEPTH <n>]
```

Breadth-first traversal from `<node_id>` up to `DEPTH` hops (default `1`), treating
edges as undirected for reachability — returns every distinct reachable node id,
excluding the start node. Empty array (not an error) for an unknown node or no
relations.

```bash
redis-cli -p 6380 GRAPH.RELATED default redis
# -> ["used_by:project_a", "memory:mem-1", ...]
```

`extract_entities`/`find_related` are also available as MCP tools — see the
[MCP server](/getting-started/mcp-server) page.

See [`internal/consolidate`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/consolidate)
and [`internal/graph`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/graph)
for the implementation.
