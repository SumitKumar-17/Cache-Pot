# Architecture Overview

## Repo layout

```
cmd/cachepotd/       server entrypoint: parses flags/env into server.Config, runs the server
internal/
  server/            wires storage, auth, observability, and the RESP + MCP layers into a runnable process
    resp/            RESP2 protocol (encode/decode), command dispatch, all command handlers
  storage/           the Engine interface — the seam between RESP handlers and any data-structure store
    memstore/        the implementation of Engine: a sharded in-memory map, keys namespaced by workspace
    ttl/             active (background) expiry reaper for memstore
  auth/               AUTH gating: single shared password, or real per-workspace credentials (Phase 7)
  observability/      structured logging + metrics (/metrics, /stats, /dashboard)
  embed/              embedding-provider abstraction: mock + OpenAI (Phase 2)
  semantic/           semantic/prompt cache: CACHE.SEMANTIC, CACHE.PROMPT (Phase 2)
  toolcache/          tool-result cache: TOOL.CACHE (Phase 2)
  vector/             native flat vector index: VECTOR.UPSERT/SEARCH/DELETE (Phase 3)
  mcp/                native MCP server, sharing the same instances as resp.Deps (Phase 3)
  memory/             agent memory domain: MEMORY.*/AGENT.*, version history (Phases 4, 7)
  eviction/           eviction policies: LRU, Weighted, consumed by memstore's --max-entries bound (Phase 5)
  analytics/          cost/usage analytics: embedding/completion $ cost tracking (Phase 5)
  llm/                text-generation abstraction: CompletionProvider, mock + OpenAI (Phase 6)
  consolidate/        memory consolidation: SUMMARY.CREATE (Phase 6a)
  graph/              knowledge graph: GRAPH.EXTRACT/RELATED (Phase 6b)
api/commands.yaml     authoritative command list across all 7 phases (source of truth for docs/commands)
test/                 integration tests
```

All seven phases are wired into the running server today — every package above is
exercised by a real running Cache-Pot process, not just present in the tree. See the
[roadmap](/roadmap/) for what each phase added.

## The seam: `storage.Engine`

Everything the RESP command handlers need from a backing store is expressed
as a single Go interface, `storage.Engine`
(`internal/storage/engine.go`). This is the one seam in the system: the RESP
protocol layer (`internal/server/resp`) only ever talks to an `Engine`, never
to `memstore` directly.

```go
type Engine interface {
	Get(workspace, key string) (val []byte, ok bool, err error)
	Set(workspace, key string, val []byte, opts SetOpts) (ok bool, prevVal []byte, hadPrev bool, err error)
	Del(workspace string, keys ...string) (deleted int)
	// ... hash / list / set / sorted-set operations ...

	WatchVersion(workspace, key string) uint64
	Exec(fn func() error) error
	Close() error
}
```

There's exactly one implementation — `internal/storage/memstore.Store` — but
the interface is deliberately designed so a future store (a tiered/remote
store, a store backed by persistence, etc.) can plug in an alternate
`Engine`, or wrap the existing one with additional behavior, without
touching the command dispatch layer in `internal/server/resp`.

### The `workspace` parameter

Every `Engine` method takes a `workspace string` as its first parameter, and
`memstore` namespaces every key by `(workspace, key)` internally (see
`memstore.nsKey`) — this has been real routing since Phase 1, not just an
inert placeholder. Phase 7 built real per-workspace **authorization** on top
of it: `--workspace-credentials` configures `workspace:password` pairs, and
`internal/auth`/`ClientState.authorizedForWorkspace` reject a command whose
workspace argument doesn't match the connection's authenticated workspace.
See [Workspaces & Multi-Tenancy](/getting-started/workspaces) for the full
behavior — including what's *not* covered (`CACHE.SEMANTIC`/`CACHE.PROMPT`/
`TOOL.CACHE` remain global caches, and the MCP server has no auth layer at
all).

## Request flow

1. `cmd/cachepotd/main.go` parses flags/env into a `server.Config` and calls
   `server.Run`.
2. `internal/server.Server.run` builds a `memstore.Store` (the concrete
   `Engine`), an `auth` gate, observability `Metrics`/`Logger`, a `PubSub`
   hub, and a command `Registry`, bundling them into `resp.Deps`. It then
   accepts TCP connections (bounded by `MaxConnections`) and hands each one
   to `resp.HandleConn`.
3. `internal/server/resp` reads a RESP2 command off the wire
   (`ReadCommand`), looks it up in the `Registry` (case-insensitive, with
   arity checking, no-auth allowances, and MULTI-queueing rules), and
   invokes its `HandlerFunc`.
4. Handlers call methods on `Deps.Engine` (the `storage.Engine` interface)
   and translate the result into a RESP2 `Reply`, which is written back
   through a buffered `Writer` — supporting pipelining, since multiple
   replies can be buffered before a single `Flush`.

See [Storage Engine](/architecture/storage-engine) for how `memstore`
implements `Engine`, and [Redis Compatibility](/architecture/redis-compatibility)
for exactly what protocol surface is and isn't supported today.
