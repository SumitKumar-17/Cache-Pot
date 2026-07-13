# Configuration

`cachepotd` (`cmd/cachepotd/main.go`) is configured via CLI flags, with each
flag falling back to an environment variable, and each environment variable
falling back to a hard-coded default. **Flags always win** when explicitly
passed, since a flag's own default is the resolved environment-variable (or
hard-coded) value.

| Flag | Environment variable | Default | Description |
|---|---|---|---|
| `--port` | `CACHEPOT_PORT` | `6380` | TCP port the RESP server listens on |
| `--password` | `CACHEPOT_PASSWORD` | *(empty — no auth required)* | Required `AUTH` password; empty means no authentication, matching Redis's own default |
| `--max-connections` | `CACHEPOT_MAX_CONNECTIONS` | `10000` | Maximum number of concurrent client connections; connections beyond this are rejected with a clean error and the socket is closed |
| `--embed-provider` | `CACHEPOT_EMBED_PROVIDER` | `mock` | Embedding provider backing `CACHE.SEMANTIC`: `mock` (deterministic, dependency-free, for local dev/testing) or `openai` |
| `--openai-api-key` | `OPENAI_API_KEY` | *(none)* | Required when `--embed-provider openai` is selected; startup fails with a clear error if missing |
| `--openai-api-base` | `OPENAI_API_BASE` | `https://api.openai.com/v1` | Base URL for the OpenAI-compatible embeddings API; override to point at Azure OpenAI or another compatible gateway |
| `--mcp-port` | `CACHEPOT_MCP_PORT` | `6381` | TCP port for the native [MCP server](/getting-started/mcp-server); `0` disables it. Also serves `/metrics`, `/stats`, and `/dashboard` (see [Observability](/getting-started/observability)) |
| `--max-entries` | `CACHEPOT_MAX_ENTRIES` | `0` | Maximum total live keys, server-wide, before [eviction](/getting-started/observability#eviction) kicks in; `0` means unlimited |
| `--eviction-policy` | `CACHEPOT_EVICTION_POLICY` | `lru` | Eviction policy used once `--max-entries` is exceeded: `lru` or `weighted`; any other value fails at startup |
| `--completion-provider` | `CACHEPOT_COMPLETION_PROVIDER` | `mock` | Text-generation provider backing `SUMMARY.CREATE`/`GRAPH.EXTRACT`: `mock` (no real generation — see [LLM Completions](/getting-started/completions)) or `openai` |
| `--openai-completion-model` | `OPENAI_COMPLETION_MODEL` | `gpt-4o-mini` | Chat completion model, when `--completion-provider openai` |
| `--workspace-credentials` | `CACHEPOT_WORKSPACE_CREDENTIALS` | *(empty — no per-workspace auth)* | Comma-separated `workspace:password` pairs enabling real, enforced [workspace isolation](/getting-started/workspaces); mutually exclusive with `--password` (startup error if both are set) |

## Loading config from a `.env` file

`cachepotd` reads a `.env` file (if present in the current working directory) at
startup and sets any variables it defines that aren't already present in the real
environment — a real environment variable always takes precedence over `.env`. This is
a minimal, dependency-free convenience loader (simple `KEY=VALUE` lines, no expansion,
no multi-line values) — not a general `.env` spec implementation.

```bash
# .env
OPENAI_API_KEY="sk-..."
OPENAI_API_BASE=https://api.openai.com/v1
```

```bash
./bin/cachepotd --embed-provider openai   # picks up OPENAI_API_KEY/OPENAI_API_BASE from .env
```

**`.env` is git-ignored — never commit it.** Treat any API key in it the same as you
would any other credential.

## Examples

Using flags:

```bash
./bin/cachepotd --port 6380 --password "s3cret" --max-connections 5000
```

Using environment variables:

```bash
export CACHEPOT_PORT=6380
export CACHEPOT_PASSWORD="s3cret"
export CACHEPOT_MAX_CONNECTIONS=5000
./bin/cachepotd
```

Mixing both (the flag wins for any value explicitly passed):

```bash
export CACHEPOT_PORT=6380
./bin/cachepotd --port 7000   # listens on 7000, not 6380
```

## Notes

- `--port` defaults to `6380`, not Redis's `6379`, deliberately — so
  `cachepotd` doesn't collide with a real local Redis instance during
  development or testing.
- If `--password` (or `CACHEPOT_PASSWORD`) is set, clients must issue
  `AUTH <password>` before running other commands. See
  [Connection commands](/commands/connection).
- The three connection flags are the entire Phase 1 configuration surface;
  `--embed-provider`/`--openai-api-key`/`--openai-api-base` are Phase 2 additions for
  [`CACHE.SEMANTIC`](/commands/semantic-cache); `--mcp-port` is a Phase 3 addition for
  the [MCP server](/getting-started/mcp-server); `--max-entries`/`--eviction-policy`
  are Phase 5 additions (see [Observability](/getting-started/observability));
  `--completion-provider`/`--openai-completion-model` are Phase 6 additions for
  [`SUMMARY.CREATE`/`GRAPH.EXTRACT`](/commands/graph) (see
  [LLM Completions](/getting-started/completions)); `--workspace-credentials` is a
  Phase 7 addition for real [workspace isolation](/getting-started/workspaces). There
  is no config file yet.
- `CACHE.PROMPT` and `TOOL.CACHE` don't use an embedding provider — they're
  exact-match caches, so `--embed-provider` only affects `CACHE.SEMANTIC`.
- `--openai-completion-model` reuses the *existing* `--openai-api-key`/
  `--openai-api-base` — one OpenAI account/endpoint serves both embeddings and
  completions, no duplicate key/base flags.
