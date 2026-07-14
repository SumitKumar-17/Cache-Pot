# Vector Commands

::: tip v0.3.0 — real
These commands work today: a flat (brute-force) namespace-partitioned vector index.
See [architecture](/architecture/overview) for how it fits together, and
[MCP Server](/getting-started/mcp-server) for the equivalent `store_vector`/
`find_similar`/`delete_vector` MCP tools.
:::

| Command | Summary |
|---|---|
| `VECTOR.UPSERT` | Insert or update a vector (plus optional metadata/text) in a namespace |
| `VECTOR.SEARCH` | Nearest-neighbor search over a namespace |
| `VECTOR.DELETE` | Delete a vector by id from a namespace |

## VECTOR.UPSERT

```
VECTOR.UPSERT <namespace> <id> <vector_json> [METADATA <metadata_json>] [TEXT <text>]
```

- `<vector_json>` is a JSON array of numbers, e.g. `[0.1,0.2,0.3]`.
- `METADATA <metadata_json>` (optional) attaches a JSON object of key/value metadata,
  usable later with `VECTOR.SEARCH ... FILTER`.
- `TEXT <text>` (optional) attaches a raw text payload used only by `HYBRID` search
  (below) — independent of the vector/metadata.
- Upserting the same `(namespace, id)` again fully replaces the previous entry (not a
  merge). Returns `+OK`.

```bash
redis-cli -p 6380 VECTOR.UPSERT docs a '[1,0,0]' METADATA '{"lang":"go"}' TEXT "cats are cute"
redis-cli -p 6380 VECTOR.UPSERT docs b '[0,1,0]' METADATA '{"lang":"py"}' TEXT "dogs are loyal"
```

## VECTOR.SEARCH

```
VECTOR.SEARCH <namespace> <vector_json> [K <n>] [METRIC cosine|dot|euclidean]
              [FILTER <key> <value> ...] [HYBRID <query_text> [ALPHA <float>]] [WITHSCORES]
```

- `K` (default `10`) caps the number of results, best match first (highest similarity
  for `cosine`/`dot`, lowest distance for `euclidean`).
- `METRIC` (default `cosine`) selects the distance/similarity function. A vector whose
  dimension doesn't match the query is silently skipped, not an error.
- `FILTER <key> <value>` (repeatable) restricts candidates to those whose stored
  metadata has that exact key/value pair.
- `HYBRID <query_text> [ALPHA <float>, default 0.5]` blends vector similarity with a
  naive keyword-overlap score against each candidate's stored `TEXT` — this is
  intentionally simple (token overlap, no stemming/IDF/BM25), not a solved feature.
  Final score = `ALPHA * vectorScore + (1-ALPHA) * keywordScore`.
- `WITHSCORES` includes each result's score after its id (like Redis's own
  `ZRANGE ... WITHSCORES`). Without it, only ids are returned. An empty array is
  returned (not an error) for an unknown namespace or no matches.

```bash
redis-cli -p 6380 VECTOR.SEARCH docs '[1,0,0]' WITHSCORES
# 1) "a"
# 2) "1"
# 3) "b"
# 4) "0"

redis-cli -p 6380 VECTOR.SEARCH docs '[1,0,0]' FILTER lang py
# 1) "b"
```

## VECTOR.DELETE

```
VECTOR.DELETE <namespace> <id>
```

Returns `:1` if the id existed and was removed, `:0` if it didn't exist — same
integer-count convention as Redis's `DEL`.

```bash
redis-cli -p 6380 VECTOR.DELETE docs a
# (integer) 1
redis-cli -p 6380 VECTOR.DELETE docs a
# (integer) 0
```

See [`internal/vector`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/vector)
for the implementation.
