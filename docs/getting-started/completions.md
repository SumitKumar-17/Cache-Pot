# LLM Completions

::: tip v0.6.0 — real
Cache-Pot's first text-*generation* capability. Everything before v0.6.0
(`CACHE.SEMANTIC`, `MEMORY.SEARCH`, `VECTOR.SEARCH`, ...) only ever produced
*embeddings* — a vector representing meaning, never new text. `SUMMARY.CREATE` and
`GRAPH.EXTRACT` need an LLM that actually writes text (a consolidated summary, a
structured entity/relationship extraction), so v0.6.0 added that as a new provider
abstraction, `internal/llm.CompletionProvider`, deliberately kept independent from
`internal/embed.Provider`.
:::

## Configuration

| Flag | Environment variable | Default | Description |
|---|---|---|---|
| `--completion-provider` | `CACHEPOT_COMPLETION_PROVIDER` | `mock` | `mock` (deterministic, dependency-free, no real generation) or `openai` |
| `--openai-completion-model` | `OPENAI_COMPLETION_MODEL` | `gpt-4o-mini` | Chat completion model, when `--completion-provider openai` |

`openai` reuses the same `--openai-api-key`/`--openai-api-base` already used for
embeddings — one OpenAI account/endpoint serves both. See
[configuration](/getting-started/configuration) for the full flag reference.

## The `mock` provider does no real generation — read this before relying on it

`llm.NewMock()` is deterministic and dependency-free, matching this project's
`embed.NewMock` precedent exactly: it makes no network call and performs **no real
language understanding or generation**. It returns a fixed, clearly-marked string (a
`"[mock completion, no real generation] ..."` prefix over a truncated echo of the
input) and always reports zero token usage — nothing fabricated.

Concretely, this means:
- [`SUMMARY.CREATE`](/commands/graph#summary-create-consolidation) against the mock
  provider stores *something* as the summary (whatever the mock returned), but it is
  not a real summary — don't expect it to make semantic sense.
- [`GRAPH.EXTRACT`](/commands/graph#graph-extract-populate-the-knowledge-graph) against
  the mock provider **always extracts zero entities and zero relations** — the mock
  can't produce the structured JSON extraction needs, and the extraction code treats
  that gracefully as "nothing found," not an error. This is deliberate and tested, not
  a bug.

Use `--completion-provider openai` for either command to do something semantically
real. Every completion call is instrumented the same way embedding calls are (see
[Observability](/getting-started/observability)): `/metrics`/`/stats` expose
`cachepot_completion_calls_total`/`_errors_total`/`_in_flight`, and real token usage
feeds a separate `completion` cost total in the `/dashboard`, kept distinct from
embedding cost.
