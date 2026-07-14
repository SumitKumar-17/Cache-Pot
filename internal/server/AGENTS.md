# AGENTS.md — internal/server

## Role

This package is the process-wiring layer: it turns a `Config` into a running
Cache-Pot server. `cmd/cachepotd` calls into this package only (`server.Run`); this
package in turn constructs the storage engine, auth, embedding/completion providers,
every domain package (semantic/vector/memory/consolidate/graph), and the shared
`resp.Deps` struct, then starts the RESP listener (`internal/server/resp.HandleConn`
per connection) and — if enabled — an HTTP mux for the native MCP server plus
`/metrics`, `/stats`, `/dashboard`.

## Key pieces

- **`Config`** (`config.go`) — every server setting: `Port`, `Password`,
  `WorkspaceCredentials []auth.Credential`, `MaxConnections`, `EmbedProvider`,
  `OpenAIAPIKey`, `OpenAIAPIBase`, `CompletionProvider`, `OpenAICompletionModel`,
  `MCPPort`, `MaxEntries`, `EvictionPolicy`. `Password` and `WorkspaceCredentials` are
  documented as mutually exclusive, but `Config` itself doesn't enforce that — see
  `buildAuthenticator` below. `0` means "disabled/unlimited" for `MCPPort` and
  `MaxEntries`, matching the repo-wide "0 means off" convention.
- **`DefaultConfig()`** and the `Default*` constants — `DefaultPort` (6380) and
  `DefaultMCPPort` (6381) are deliberately not well-known Redis/HTTP ports, so
  `cachepotd` doesn't collide with a real local Redis during dev.
- **`Run(ctx, cfg) error`** — binds `cfg.Port` itself via `net.Listen`, then calls
  `RunListener`.
- **`RunListener(ctx, cfg, ln) error`** — takes an already-bound `net.Listener`
  instead. This exists specifically so tests can bind `net.Listen(":0")` (a random free
  port) and hand the listener straight in, with no bind-race between picking a port and
  the server claiming it. `test/integration` uses this exclusively — never `Run`.
- **`Server`** — holds `cfg`, `logger`, `metrics`, `analytics`, and `deps *resp.Deps`.
  Built and run entirely inside `RunListener`/`(*Server).run`.
- **`buildEmbedProvider`, `buildCompletionProvider`, `buildAuthenticator`,
  `buildEvictionPolicy`** — all follow the same fail-loudly-at-startup convention: an
  unrecognized provider/policy name, or `"openai"` selected without an API key, is a
  startup error, never a lazy failure at first use. `buildAuthenticator` is the *only*
  place `Password`/`WorkspaceCredentials` mutual exclusivity is actually checked
  (returns an error if both are set).

## The shared-instance invariant (this is where it's enforced)

`(*Server).run` is the literal place the root `AGENTS.md`'s "same shared instance
across RESP and MCP" rule gets implemented, so read it closely before changing
construction order:

- `provider` (embed.Provider) is wrapped exactly once via
  `observability.InstrumentProvider`, then that *same* wrapped value is passed to both
  `semantic.New(provider)` and `memory.New(provider)`.
- `completionProvider` is wrapped exactly once via
  `observability.InstrumentCompletionProvider`, then shared into `consolidate.New`,
  `resp.Deps.CompletionProvider`, and `mcp.New(...)`.
- `memoryStore`, `consolidator`, and `graphStore` are each constructed exactly once and
  passed into *both* `resp.Deps` and `mcp.New(...)` — never construct a second
  `memory.New`/`consolidate.New`/`graph.New` for the MCP side. If you add a new domain
  package following this pattern, construct it once here and thread the same value into
  both places.

## Gotchas specific to this package

- `/metrics`, `/stats`, and `/dashboard` are mounted on the **same mux as the MCP
  server**, and only exist when `cfg.MCPPort != 0` — there is no standalone metrics
  port. `http.ServeMux` (Go 1.22+) prefers the specific `/metrics`/`/stats`/`/dashboard`
  patterns over the catch-all `/` the MCP handler owns, so this doesn't break MCP
  clients hitting `/`. If you need metrics reachable without MCP enabled, that's a
  deliberate future change (a dedicated `--metrics-port`), not a quick fix here.
- The `MaxConnections` rejection path writes a raw literal
  `"-ERR max number of clients reached\r\n"` directly to the socket in the accept loop,
  bypassing `resp/errors.go`'s helpers entirely (that code runs before a connection ever
  reaches `resp.HandleConn`). If you change this message, keep the RESP error-line
  format by hand.
- Graceful shutdown: `ctx.Done()` sets `closing`, closes the listener, and (if MCP is
  running) calls `mcpSrv.Shutdown` with a `shutdownGrace` (5s) timeout. The accept loop
  distinguishes a real accept error from "listener closed because we're shutting down"
  via the `closing` atomic — don't reintroduce logging noise on the expected
  shutdown-triggered accept error.
- `engine := memstore.New(32, ...)` hard-codes 32 shards here — that's the number the
  root `AGENTS.md` and `docs/getting-started/observability.md` reference for
  "approximate below the shard count" eviction behavior.

## Testing

There is no `*_test.go` file directly in this directory. This package is exercised
end-to-end by the sibling top-level package `test/integration`, which calls
`server.RunListener` against a `net.Listen(":0")` port and drives it with a real
`go-redis` client and, in places, a raw `net.Dial` connection:

```bash
go test ./test/integration/... -race
```

For the protocol/handler logic this package wires up, test the layer below directly:

```bash
go test ./internal/server/resp/... -race
```

`-race` matters for `test/integration` — `RunListener`'s accept loop and each
connection run in their own goroutines, and shutdown coordination uses
`sync.WaitGroup`/`atomic.Bool`.

## Limitation specific to this package

Metrics/stats/dashboard availability is coupled to `--mcp-port` (see above) — this is a
real, current constraint, not a documented "known gap" elsewhere, so don't assume a
standalone metrics endpoint exists when `--mcp-port 0` is set.
