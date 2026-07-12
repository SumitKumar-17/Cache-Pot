# Vector Commands

::: info Planned — Phase 3
These commands are designed but not implemented yet. See the
[roadmap](/roadmap/) for details.
:::

| Command | Summary |
|---|---|
| `VECTOR.UPSERT` | Insert or update a vector embedding in a named index |
| `VECTOR.SEARCH` | Nearest-neighbor search over a vector index |
| `VECTOR.DELETE` | Delete a vector from a named index |

## What this will do

Phase 3 adds a native vector store to Cache-Pot, so vector search doesn't
require a separate database:

- A flat (brute-force) index to start, supporting cosine, dot-product, and
  Euclidean distance
- Metadata filtering and namespaces alongside vectors
- Naive hybrid keyword + vector search

Phase 3 also introduces a native MCP server exposing `remember` / `recall` /
`search` / `store_vector` / `find_similar` directly against the engine —
without an adapter layer translating to/from another vector database's API.

None of this exists in the codebase today. See
[internal/vector](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/vector)
and [internal/mcp](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/mcp)
for the current scaffolding.
