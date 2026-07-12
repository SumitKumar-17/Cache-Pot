# Architecture Overview

## Repo layout

```
cmd/cachepotd/       server entrypoint: parses flags/env into server.Config, runs the server
internal/
  server/            wires storage, auth, observability, and the RESP layer into a runnable process
    resp/            RESP2 protocol (encode/decode), command dispatch, Phase 1 command handlers
  storage/           the Engine interface — the seam between RESP handlers and any data-structure store
    memstore/        Phase 1's implementation of Engine: a sharded in-memory map
    ttl/             active (background) expiry reaper for memstore
  auth/               password-based AUTH gating (Phase 1: single shared password)
  observability/      structured logging + metrics
  tenancy/            workspace/multi-tenancy scaffolding (Phase 7)
  embed/              embedding-provider abstraction scaffolding (Phase 2)
  semantic/           semantic/prompt cache scaffolding (Phase 2)
  toolcache/          tool-result cache scaffolding (Phase 2)
  vector/             vector index scaffolding (Phase 3)
  mcp/                native MCP server scaffolding (Phase 3)
  memory/             agent memory domain scaffolding (Phase 4)
  eviction/           eviction policy scaffolding (LRU today, pluggable scoring in Phase 5)
  analytics/          cost/usage analytics scaffolding (Phase 5)
  consolidate/        memory consolidation scaffolding (Phase 6a)
  graph/              knowledge graph scaffolding (Phase 6b)
api/commands.yaml     authoritative command list across all 7 phases (source of truth for docs/commands)
test/                 integration tests
```

::: info
Packages listed above with "scaffolding" exist as Go packages/files in the
tree today, but implement Phase 2+ features that are **not wired into the
running server** yet. Only `cmd/cachepotd`, `internal/server` (including
`internal/server/resp`), `internal/storage` (including `memstore` and
`ttl`), `internal/auth`, and `internal/observability` are exercised by a
running Cache-Pot process today. See the [roadmap](/roadmap/) for what
activates each of the others.
:::

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

Phase 1 ships exactly one implementation — `internal/storage/memstore.Store`
— but the interface is deliberately designed so later phases (a
tiered/remote store, a store backed by persistence, etc.) can plug in an
alternate `Engine`, or wrap the existing one with additional behavior,
without touching the command dispatch layer in `internal/server/resp`.

### The `workspace` parameter

Every `Engine` method takes a `workspace string` as its first parameter,
even though Phase 1 only ever passes a single constant value (`"default"`).
This is deliberate, forward-looking design: Phase 7 introduces multi-tenancy
(`internal/tenancy`), where each tenant/agent gets an isolated keyspace — a
"workspace." Threading the parameter through every call site now, even
though it's unused for routing today, means Phase 7 can implement
per-workspace isolation inside the storage layer (e.g. namespacing shards or
maps per workspace) without changing a single call site in the RESP
handlers.

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
