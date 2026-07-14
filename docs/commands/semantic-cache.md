# Semantic Cache Commands

::: tip v0.2.0 — real
These commands work today. They require an embedding provider — see
[configuration](/getting-started/configuration) for `--embed-provider`. Their optional
`COST` argument was added in v0.5.0 — see [Observability](/getting-started/observability).
:::

| Command | Summary |
|---|---|
| `CACHE.SEMANTIC` | Cache/retrieve an LLM response by embedding-similarity match |
| `CACHE.PROMPT` | Cache/retrieve a response by an exact (template, variables, model) key |

## CACHE.SEMANTIC

Caches an LLM response keyed by *meaning*, not exact string match — a rephrased-but-
equivalent prompt can still hit the cache.

```
CACHE.SEMANTIC SET <prompt> <response> [MODEL <model>] [TEMP <temperature>] [TTL <seconds>] [COST <dollars>]
CACHE.SEMANTIC GET <prompt> [MODEL <model>] [TEMP <temperature>] [THRESHOLD <float>]
```

- `MODEL` and `TEMP` partition the cache — a hit is only ever considered against entries
  stored under the same model and temperature. Both default to a fixed default
  partition if omitted.
- `GET` embeds `<prompt>` and returns the response of the closest previously-`SET`
  prompt in that partition, if its cosine similarity is at or above `THRESHOLD`
  (default `0.85`). Otherwise it returns a nil reply, same as `GET` on a missing key.
- Matching is a brute-force scan within the partition (no approximate index yet — same
  flat-scan approach as the [native vector store](/commands/vector), which also has no
  ANN index yet).
- `COST` (optional, non-negative) records what this response cost you to produce (e.g.
  an LLM completion's cost) — see [Observability](/getting-started/observability) for
  how this feeds the cost-analytics dashboard's "money saved" figure on later hits. A
  negative or non-numeric `COST` is a RESP error.

```bash
redis-cli -p 6380 CACHE.SEMANTIC SET "What is Kubernetes?" "K8s is a container orchestrator." MODEL gpt-4
redis-cli -p 6380 CACHE.SEMANTIC GET "What is kubernetes?" MODEL gpt-4
# -> "K8s is a container orchestrator."  (case-only difference, well above 0.85)
```

::: warning Tune THRESHOLD for real embeddings
`0.85` reliably catches near-identical phrasing (case/whitespace differences) against
real OpenAI embeddings. Substantial paraphrases — e.g. `"what is k8s?"` against
`"What is Kubernetes?"` — can score meaningfully lower (verified directly against the
OpenAI API; that pair misses at `0.85` but hits around `0.5-0.7` depending on the
model). The right threshold is workload-dependent: start around `0.75` and tune from
there, rather than assuming `0.85` is universally correct. The dependency-free `mock`
provider does not reproduce this — it's tuned to keep worded-differently duplicates
close together, so don't use it to calibrate a real threshold.
:::

## CACHE.PROMPT

Caches a response keyed by the *exact* combination of a prompt template, its variables,
and a model — useful when you want deterministic reuse rather than similarity matching.

```
CACHE.PROMPT SET <template> <variables_json> <model> <response> [TTL <seconds>] [COST <dollars>]
CACHE.PROMPT GET <template> <variables_json> <model>
```

- `<variables_json>` is a JSON object, e.g. `{"name":"Sumit","lang":"Go"}`. It's
  canonicalized before hashing, so key order in the JSON doesn't affect the cache key.
- The cache key includes the raw `<template>` text itself, so **changing the template
  string automatically invalidates only the entries tied to that exact template** — no
  separate invalidation step is needed.
- Invalid JSON in `<variables_json>` is a RESP error on both `SET` and `GET`.
- `COST` (optional, non-negative) — same meaning and "money saved" behavior as
  `CACHE.SEMANTIC SET`'s `COST`, see [Observability](/getting-started/observability).

```bash
redis-cli -p 6380 CACHE.PROMPT SET "Hello {{name}}" '{"name":"Sumit"}' gpt-4 "Hi Sumit!"
redis-cli -p 6380 CACHE.PROMPT GET "Hello {{name}}" '{"name": "Sumit"}' gpt-4
# -> "Hi Sumit!"  (key order in the JSON doesn't matter)
```

See [`internal/semantic`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/semantic)
for the implementation, and [tool cache](/commands/tool-cache) for the related
tool-result caching command that shipped alongside it in v0.2.0.
