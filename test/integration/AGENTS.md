# test/integration

Read the root `AGENTS.md` first; this file only covers what's specific to this
package.

## Role

Black-box tests that drive a **real, compiled-in-process `server.Run`/`server.RunListener`
instance** over the actual wire — real TCP, a real `go-redis` client (or raw
`net.Dial` for protocol-level edge cases), real HTTP for the MCP/metrics/dashboard
endpoints. This is deliberately *not* unit testing: `internal/server/resp`'s own
`_test.go` files already assert exact wire bytes at the handler level; this package
exists to catch the class of bug unit tests structurally can't — a broken listener,
a real client library disagreeing with the protocol, two features that only break
when combined end to end. Three files:

- `main_test.go` — core RESP: strings/hashes/lists/sets/zsets, `MULTI`/`EXEC`/`WATCH`,
  pipelining, and the `HELLO 3` rejection path (real client libraries probe this on
  connect; a bad rejection path breaks "drop-in compatible" even without RESP3
  support).
- `auth_workspace_test.go` — real per-workspace `AUTH`/isolation enforcement:
  NOAUTH-before-AUTH in multi-workspace mode, a workspace-scoped command succeeding
  against the connection's own workspace and getting `NOPERM` against another, across
  `MEMORY.*`/`VECTOR.*`/`GRAPH.*` independently (they enforce it separately, so all
  three are checked), plus the `--password`/`--workspace-credentials`
  mutual-exclusivity startup error.
- `metrics_test.go` — the MCP-port HTTP surface (`/metrics`, `/stats`, `/dashboard`):
  drives real cache activity over RESP, then asserts the Prometheus text, JSON, and
  HTML views all reflect it, including the cost-analytics ("money saved") figures.

Not covered here: MCP *tool* calls (streamable-HTTP MCP protocol) have their own
real-wire test setup in `internal/mcp`'s own `server_test.go` (`httptest.Server` +
`sdkmcp.StreamableClientTransport`), including a cross-protocol test proving an MCP
write and a RESP read observe the same state — see `internal/mcp/AGENTS.md`.

## Conventions specific to this package

- **Always bind the listener yourself and hand it to `server.RunListener`**, never
  `server.Run` with a fixed port: `net.Listen("tcp", "127.0.0.1:0")` picks a free port,
  eliminating any bind-race between choosing a port and the server claiming it. See
  `startServer`/`startServerWithConfig`/`startServerWithMCP` — copy one of these
  helpers for a new test file rather than inventing a fourth variant.
- **`t.Cleanup` cancels the context and waits (bounded, 5s) for the server goroutine
  to actually exit** before the test ends, so tests never leak a listening goroutine
  into the next test. Preserve this in any new "start a server" helper.
- **`newClient` (`main_test.go`) pins go-redis to RESP2 explicitly**
  (`Protocol: 2`) — go-redis v9 defaults to requesting RESP3 via `HELLO`, which Cache-Pot
  rejects, so every test using the high-level client needs this. Raw-wire tests
  (`TestWatchAbort`, `TestPipelining`, `TestHello3RawProtocolError`) bypass go-redis
  entirely via `net.Dial` when they need to control the exact bytes sent or the exact
  interleaving between two connections.
- **MCP/metrics tests poll before asserting** (`waitForHTTP`): the HTTP listener starts
  in a separate goroutine inside `server.RunListener`, so a test hitting it immediately
  after the setup call returns can race the bind. Use `waitForHTTP`/`freePort` rather
  than a fixed `time.Sleep`.

## Testing

```bash
go build -o /tmp/cachepotd ./cmd/cachepotd   # optional manual sanity check, not required by the tests themselves
go test ./test/integration/... -race
```

No mocks: every test here runs against the real `internal/storage/memstore` engine
and the real default `mock` embed/completion providers (never `--embed-provider
openai`/`--completion-provider openai` — these tests must pass offline, with no
network access or API key). `-race` matters because the pub/sub forwarder goroutine,
the TTL reaper, and the MCP HTTP listener all run concurrently with the test body.

## Package-specific gaps

- No test here exercises `--completion-provider openai` or `--embed-provider openai`
  against a real OpenAI endpoint — that would require real network access and a paid
  API key, so it's out of scope for this package by design (see the mock-provider
  graceful-degradation tests in `internal/llm`/`internal/embed`/`internal/graph`
  instead).
- Consolidation (`SUMMARY.CREATE`) and knowledge-graph (`GRAPH.EXTRACT`/`GRAPH.RELATED`)
  commands are exercised over the wire in `auth_workspace_test.go` only incidentally
  (as part of the workspace-isolation check for `GRAPH.RELATED`); there's no dedicated
  end-to-end test for the full consolidate/extract flow in this package — that logic's
  real coverage lives in `internal/consolidate`/`internal/graph`'s own unit tests.
