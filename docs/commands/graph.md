# Knowledge Graph Commands

::: info Planned — Phase 6
These commands are designed but not implemented yet. See the
[roadmap](/roadmap/) for details.
:::

| Command | Summary |
|---|---|
| `SUMMARY.CREATE` | Consolidate accumulated episodic memories into a summary |
| `GRAPH.RELATED` | Find entities/memories related to a given node in the knowledge graph |

## What this will do

Phase 6 is the largest phase in the roadmap, split into two sub-milestones
because entity/relationship extraction quality and consolidation judgment
calls are genuinely research-adjacent work, not a weekend feature:

- **Consolidation** (`SUMMARY.CREATE`): nightly dedup of near-duplicate
  memories via vector similarity, and summarization of episodic-memory
  clusters into long-term memory.
- **Knowledge Graph** (`GRAPH.RELATED`): entity/relationship extraction from
  memory content, graph storage, and relationship queries over that graph.

None of this exists in the codebase today. See
[internal/consolidate](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/consolidate)
and [internal/graph](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/graph)
for the current scaffolding.
