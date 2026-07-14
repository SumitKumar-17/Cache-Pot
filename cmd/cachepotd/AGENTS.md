# AGENTS.md — cmd/cachepotd

## Role

`cachepotd` is the process entrypoint. `main.go` turns CLI flags/env vars into a
`server.Config` and calls `server.Run` (see `internal/server`), which does the actual
work of wiring up storage, auth, and the RESP/MCP listeners. This package should stay
thin: it owns flag/env parsing and process bootstrap only, never protocol or storage
logic.

## Key pieces

All functions here are unexported (`package main`), so "contract" means "what a caller
inside `main()` can rely on," not a public API.

- **`main()`** — calls `loadDotEnv(".env")`, then `parseConfig()`, then
  `server.Run(context.Background(), cfg)`. Any error from either step prints to stderr
  prefixed `"cachepotd:"` and exits 1.
- **`loadDotEnv(path string)`** — a deliberately minimal `.env` loader: `KEY=VALUE`
  lines, `#`-comments, trims surrounding quotes, no multi-line values, no variable
  expansion. **Never overrides a variable already set in the real environment** — real
  env wins over `.env`. Missing/unreadable file is silently ignored (not an error).
- **`parseWorkspaceCredentials(s string) ([]auth.Credential, error)`** — parses
  `"workspace:password,workspace2:password2"` into `[]auth.Credential`. Empty input →
  `(nil, nil)`. A malformed entry (no `:`, or an empty workspace/password on either
  side) is a hard error — this function does *not* silently skip bad entries.
- **`parseConfig() (server.Config, error)`** — builds the full `server.Config`.
  Precedence is flag > env var > hard-coded default for every setting.

## Gotchas specific to this package

- **Secrets never get a flag default pulled from the environment.** `--password` and
  `--openai-api-key` are defined with an empty string default (not `envPassword`/
  `envOpenAIAPIKey`), and the env-var fallback is applied manually *after*
  `flag.Parse()`. This is intentional: Go's `flag` package prints every flag's default
  in `--help` output, and printing a real secret there would leak it into terminal
  scrollback, screenshots, and CI logs. If you add a new secret-shaped flag, follow this
  same two-step pattern — do not give it a real default.
- `--password` and `--workspace-credentials` are parsed independently here with no
  cross-check; their mutual exclusivity is enforced downstream in
  `server.buildAuthenticator`, not in this package. Don't duplicate that check here.
- `parseWorkspaceCredentials`'s error is the only validation this package does on
  workspace credentials — malformed entries fail at startup, not lazily.

## Testing

There is no `*_test.go` file in this directory — `parseConfig`, `parseWorkspaceCredentials`,
and `loadDotEnv` currently have no unit tests of their own. To verify a change here:

```bash
go build -o bin/cachepotd ./cmd/cachepotd
./bin/cachepotd --port 6380 --mcp-port 6381
redis-cli -p 6380 PING
```
Also exercise the specific flag/env path you changed (e.g. `CACHEPOT_WORKSPACE_CREDENTIALS=acme:secret1 ./bin/cachepotd --port 6380` then `redis-cli -p 6380 AUTH secret1`), and confirm `--help` never prints a real secret if you touch flag definitions.

## Known limitation

`parseWorkspaceCredentials` and `loadDotEnv` are unexported and untested by any
automated test in this repo (no unit test here, and `test/integration` never feeds a
malformed `--workspace-credentials` string through this package's parser — it builds
`server.Config` directly in Go). If you touch either function's parsing logic, verify by
hand.
