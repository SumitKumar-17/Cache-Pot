# internal/memory

## Role

The agent-memory domain layer: short-term, long-term, episodic, and semantic
memories keyed by (workspace, agent), embedded and ranked for similarity
search via `internal/vector`. Backs `MEMORY.PUT`/`MEMORY.GET`/`MEMORY.SEARCH`
(`internal/server/resp/handlers_memory.go`) and the ergonomic
`AGENT.REMEMBER`/`AGENT.RECALL` wrappers (`handlers_agent.go`, which add no
store logic of their own). `MEMORY.PUT`/`GET`/`SEARCH` and `AGENT.REMEMBER`/
`RECALL` shipped in v0.4.0; full version history (`MEMORY.HISTORY`) landed in
v0.7.0. The MCP server's `remember`/`recall` tools call into this exact same
`Store` instance too — see the shared-instance invariant below.

## Key types and invariants

- **`MemoryStore` interface** (`store.go`): `Put`, `Get`, `Search`, `History`,
  `List`. `Store` is the only implementation.
- **`Store`** wraps an `embed.Provider` and reuses `internal/vector.Store`
  *purely as a search index* (namespace = workspace) — it is not the source
  of truth. Full `Memory` records live in `records map[workspaceID]map[id]*Memory`;
  a separate `history map[workspaceID]map[id][]Memory` holds prior versions.
  A single `RWMutex` guards both maps; `vector.Store` guards its own state.
- **Versioning contract**: `Put` to an existing `(workspace, id)` bumps
  `Version` and replaces content/embedding/metadata *in place* as current.
  The record it's about to overwrite is snapshotted into `history` first
  (bounded to `maxMemoryHistoryPerRecord` = 100 prior versions per id, oldest
  dropped first). `CreatedAt` is preserved across version bumps — it's
  creation time, not update time. `History` returns the full lineage
  oldest-first, ending with the current version.
- **Shared memory by default**: `Search`/`List` with no `AgentID` filter
  search *every* agent's memories in a workspace — there is no per-agent silo
  unless the caller explicitly scopes with `AgentID`. This is a deliberate
  product invariant, not a missing filter.
- **Lazy TTL expiry**: every read path (`Get`, `Search`, `History`, `List`)
  checks `expired(now)` itself and evicts on the way out (deletes from
  `records`, `vecStore`, and `history`) — there's no background reaper here.
  `History` on an id whose *current* version has expired returns `(nil, nil)`,
  same "not a caller mistake" convention as `Get`'s `found=false`.
  `deleteHistoryLocked` is called whenever a current record is deleted,
  because `History`'s public surface has no way to reach a record with no
  current version — keeping that history around would be a pure leak.
- **`List` vs `Search`**: `List` needs no query to embed/rank against and
  returns full records including `Embedding` — it's the entry point
  `internal/consolidate` uses to gather memories for dedup comparison.
  `Search` embeds a query and ranks by cosine similarity through
  `vector.Store.Search`, pushing `AgentID`/`Kind` filters down into the
  vector store's own metadata filter (`agent_id`/`kind` are stored as vector
  metadata purely for this).
- Shared-instance invariant (see root AGENTS.md): `memory.New(provider)` is
  constructed exactly once, in `internal/server/server.go`, and that same
  `*Store` is threaded into both `resp.Deps.MemoryStore` and `mcp.New(...)`,
  and into `consolidate.New(memoryStore, ...)`. Never construct a second
  `memory.New(...)` for any of these call sites.

## Conventions/gotchas specific to this package

- `Memory.Embedding`, `CreatedAt`, and `Version` are always computed by
  `Put`, never taken from the caller's input struct — don't add a code path
  that trusts a caller-supplied value for these fields.
- History entries must be un-aliased snapshots: `Put` builds an entirely new
  `*Memory` on every call rather than mutating the existing one, so a
  captured history entry never later changes underneath you. If you touch
  `Put`, preserve this — `store_test.go`'s `TestHistoryBuildsUpOldestFirstEndingAtCurrent`
  asserts on backing-array identity to catch aliasing regressions.
- Empty/no-match results are `nil` slices, not errors and not empty-but-non-nil
  slices — `Search`, `List` both follow this; `Get`/`History` use `found=false`
  / `(nil, nil)` respectively. Match whichever convention the method you're
  touching already uses.

## Testing

```
go test ./internal/memory/...
```
`-race` matters here: `Store` has a single `RWMutex` guarding two maps read
and written from concurrent RESP/MCP call sites in production. Tests use
`embed.NewMock(8)` (deterministic, no network) and override `Store.now` to
a fixed/advanceable clock for TTL-expiry tests instead of real sleeps.

## Known limitations

- No persistence (repo-wide gap — see root AGENTS.md).
- `maxMemoryHistoryPerRecord` (100) is a hard cap: an id versioned more than
  100 times permanently loses its oldest history entries, by design, not a
  bug to fix without discussion.
