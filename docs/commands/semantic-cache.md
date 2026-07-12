# Semantic Cache Commands

::: info Planned — Phase 2
These commands are designed but not implemented yet. See the
[roadmap](/roadmap/) for details.
:::

| Command | Summary |
|---|---|
| `CACHE.SEMANTIC` | Semantic-similarity cache lookup for LLM responses |
| `CACHE.PROMPT` | Store/retrieve a prompt+response pair for semantic cache lookup |

## What this will do

Phase 2 introduces caching keyed by *meaning* rather than exact string match
— an LLM call with a rephrased-but-equivalent prompt should still hit the
cache. This depends on a pluggable embedding-provider abstraction (OpenAI,
local models, etc.), planned as part of the same phase.

- `CACHE.SEMANTIC` will look up a cached LLM response by embedding
  similarity against previously cached prompts, rather than requiring an
  exact string match.
- `CACHE.PROMPT` will key by `(prompt template + variables + model
  version)`, so changing a template invalidates only the entries affected by
  that change — not the entire cache.

None of this exists in the codebase today. See
[internal/semantic](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/semantic)
for the current scaffolding, and [tool-cache](/commands/tool-cache) for the
related tool-result caching commands landing in the same phase.
