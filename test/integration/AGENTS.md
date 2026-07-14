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
when combined end to end. Four files:

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
- `real_openai_test.go` — the **only** place in the whole repo that drives Cache-Pot
  against the real OpenAI API (real embeddings, real chat completions), not a mock.
  See its own section below — it found and helped fix a real bug the first time it
  was run.
- `full_command_sweep_test.go` — breadth coverage: every command in `api/commands.yaml`
  not already exercised by the other files, against a real server with the default
  mock providers (no network access needed, so this is free and CI-safe). Between
  this file, `main_test.go`, `auth_workspace_test.go`, and `real_openai_test.go`, every
  one of the 93 commands is driven over the real wire at least once.

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

Most of this package (`main_test.go`, `auth_workspace_test.go`, `metrics_test.go`,
`full_command_sweep_test.go`) runs against the real `internal/storage/memstore` engine
and the real default `mock` embed/completion providers — no network access or API key
needed, fully CI-safe. `real_openai_test.go` is the one exception: it drives the real
OpenAI API and auto-skips unless a working key is present (see its own section below).
`-race` matters for the mock-based tests because the pub/sub forwarder goroutine, the
TTL reaper, and the MCP HTTP listener all run concurrently with the test body.

## `real_openai_test.go`: the real-API tests

Every test in this file is gated by `requireRealOpenAI`, which reads `OPENAI_API_KEY`
from a real `.env` file at the repo root (`loadRealOpenAIEnv`) and calls `t.Skip` if
none is found. `.env` is git-ignored, so these **never run in CI** and never cost CI
anything — they're an opt-in local check for a developer who has their own OpenAI key.
Run them explicitly with:

```bash
go test ./test/integration/... -run TestRealOpenAI -v
```

Each real API call costs a small amount of real money and takes real network time, so
keep the number of calls per test small, and keep assertions loose (structural/numeric
properties — token counts, entity counts, similarity above/below a threshold — never
exact LLM output text, which varies run to run).

**This file already found and helped fix a real bug once**: the first version of
`TestRealOpenAIEmbeddingCostAnalyticsTracksRealUsage` failed because
`internal/semantic.SemanticCache` and `internal/memory.Store` were calling
`embed.Provider.Embed` directly instead of going through the usage-reporting
`EmbedBatchWithUsage` path, so real embedding cost/token tracking silently never fired
for either CACHE.SEMANTIC or MEMORY.*/AGENT.*, regardless of provider — a mock-only
test suite could never have caught this, since the mock provider has no real usage to
report either way. Fixed via `embed.EmbedOne`, a small helper that prefers the
usage-reporting path when a provider supports it. If you add a new call site that
embeds text, use `embed.EmbedOne`, not `provider.Embed` directly, or you'll reintroduce
the same silent gap.

`TestRealOpenAISemanticCacheThresholdBehavior` also directly verifies the exact
numbers in `docs/commands/semantic-cache.md`'s "Tune THRESHOLD for real embeddings"
warning (the `"what is k8s?"` vs `"What is Kubernetes?"` pair hits at a lowered
threshold but misses at the 0.85 default) — if a future embedding-model swap changes
that balance enough to fail this test, the docs claim needs re-verifying too, not just
this test.

## Package-specific gaps

- MCP *tool* calls are never driven against the real OpenAI API from this package —
  `internal/mcp`'s own test suite only uses mock providers. The underlying store logic
  is identical to the RESP commands `real_openai_test.go` does cover (MCP calls straight
  into the same shared instances, see `internal/mcp/AGENTS.md`), so this is a
  deliberately accepted gap, not an oversight.
