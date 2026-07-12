# Agent Memory Commands

::: info Planned — Phase 4
These commands are designed but not implemented yet. See the
[roadmap](/roadmap/) for details.
:::

| Command | Summary |
|---|---|
| `MEMORY.PUT` | Store a memory item for an agent/workspace |
| `MEMORY.GET` | Retrieve a memory item by ID |
| `MEMORY.SEARCH` | Semantic/metadata search over an agent's memories |
| `AGENT.REMEMBER` | High-level helper to store an agent memory from raw content |
| `AGENT.RECALL` | High-level helper to recall relevant agent memories for a prompt |

## What this will do

Phase 4 is where Cache-Pot becomes an actual memory engine rather than a
cache: a real memory domain layer with short-term, long-term, episodic, and
semantic memory kinds, built on the vector search and embedding primitives
from Phase 3.

- `MEMORY.PUT` / `MEMORY.GET` / `MEMORY.SEARCH` are the low-level primitives
  for storing and retrieving memory items.
- `AGENT.REMEMBER` / `AGENT.RECALL` are higher-level helpers on top — store
  raw content and later recall whatever's relevant to a prompt, without the
  caller managing embeddings or memory-kind classification directly.
- Critically, memory is **shared across agents and models** (Claude, GPT,
  Gemini, Cursor, etc.) via agent/workspace metadata, rather than siloed per
  client — one of Cache-Pot's core bets versus Mem0/LangMem-style
  per-framework memory.

None of this exists in the codebase today. See
[internal/memory](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/memory)
for the current scaffolding.

```bash
# illustrative only — not a working example against Phase 1
MEMORY.PUT agent:research-bot "User prefers concise, bullet-point summaries"
MEMORY.SEARCH agent:research-bot "how does this user like answers formatted?"
```
