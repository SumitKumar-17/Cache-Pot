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
  `--embed-provider`/`--openai-api-key` are Phase 2 additions for
  [`CACHE.SEMANTIC`](/commands/semantic-cache). There is no config file yet,
  and no per-workspace configuration (that's Phase 7's multi-tenancy work;
  see the [roadmap](/roadmap/)).
- `CACHE.PROMPT` and `TOOL.CACHE` don't use an embedding provider — they're
  exact-match caches, so `--embed-provider` only affects `CACHE.SEMANTIC`.
