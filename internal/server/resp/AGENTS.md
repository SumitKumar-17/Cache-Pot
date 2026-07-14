# internal/server/resp

Read the root `AGENTS.md` first; this file only covers what's specific to this
package.

## Role

The RESP2 protocol layer and command dispatch: every command handler in the whole
project lives here, one `handlers_<family>.go` file per family. This is the biggest,
most-touched package in the repo (25+ files). Anything a client sends over the wire
passes through `types.go` (framing) -> `dispatch.go`'s `Registry.Handle` (lookup,
auth, MULTI-queueing, arity) -> the matched `HandlerFunc` -> a `Reply` written back
through `Writer`.

## Key types and the request path

- `ReadCommand`/`Writer` (`types.go`): hand-rolled RESP2 encode/decode. Accepts both
  real RESP arrays (`*N\r\n$len\r\n...`) and bare inline commands (whitespace-split, no
  framing) since some tools talk to a RESP server that way. `Reply` is a self-encoding
  closure (`func(*Writer) error`); handlers build replies compositionally
  (`Array(Bulk(a), Bulk(b))`) rather than writing bytes directly.
- `Command`/`Registry` (`dispatch.go`): a case-insensitive `map[string]*Command`.
  `Registry.Handle` is the *single* place auth-gating, MULTI-queueing, and arity
  checking happen — never duplicate that logic inside a handler. `AllowedNoAuth` marks
  pre-AUTH commands (AUTH, HELLO, QUIT, COMMAND, INFO); `NoQueue` marks commands that
  run immediately even inside `MULTI` (the tx commands themselves, QUIT, the
  SUBSCRIBE family).
- `HandleConn` (`conn.go`): the per-connection loop — read, dispatch, record
  metrics/latency, write, and only `Flush` when the read buffer is empty (this is what
  makes pipelining work: several replies can be buffered before one flush).
- `ClientState`/`Deps` (`client.go`): `Deps` is process-wide, built once in
  `internal/server/server.go` and shared by every connection; `ClientState` is
  per-connection (auth status, `Workspace`, MULTI/WATCH state, pub/sub subscriptions).
  `authorizedForWorkspace` is the workspace-isolation gate — see below.
- `RegisterAll` (`registry_all.go`): the master list of every `Register<Family>` call.
  A new command family must be added here or it's simply unreachable.

## Conventions specific to this package

- **One file per command family, one `Register<Family>(r *Registry)` function per
  file.** Follow the existing `handlers_*.go` files exactly (see `CONTRIBUTING.md`'s
  step-by-step) — don't fold a new family into an existing file or split one family
  across files.
- **Every workspace-scoped command checks `cs.authorizedForWorkspace(workspace)`
  before touching any store**, returning `Err(ErrWorkspaceNotAuthorized(workspace))` on
  failure. Grep `handlers_memory.go`/`handlers_agent.go`/`handlers_vector.go`/
  `handlers_graph.go` for the exact call shape before adding a new workspace-scoped
  command — the core KV handlers (strings/hashes/lists/sets/zsets/generic) do *not* do
  this check themselves because they have no explicit workspace argument; they simply
  operate on `cs.Workspace` directly, which is already pinned by AUTH.
- **Error strings are centralized in `errors.go`.** Never hand-roll a new "WRONGTYPE"/
  "ERR ..." string inline in a handler — real Redis clients pattern-match on these
  prefixes, so reuse `ErrFromStorage`/the `Err*Msg` constants, or add a new constant
  there if a genuinely new error shape is needed.
- **Option-parsing loops are hand-rolled per handler** (a `for i := N; i < len(args);`
  loop switching on `strings.ToUpper(args[i])`), not a shared flag-parsing helper — this
  is deliberate given RESP's flat `[]string` argument shape and Redis's own
  keyword-option conventions (`EX`/`PX`/`NX`/`XX` on `SET`, `MATCH`/`COUNT` on `SCAN`,
  etc.); match this pattern for a new command's options rather than introducing a new
  parsing abstraction.
- **`commandFamily` (`metrics_family.go`) must stay in sync with new commands.** Flat
  Redis-style names need an entry in `coreFamilies`; `MODULE.ACTION`-style names
  (`CACHE.*`, `VECTOR.*`, `MEMORY.*`, ...) are derived from the module prefix via
  `moduleFamilies` instead — only add a `coreFamilies` entry for genuinely new
  flat-named commands, not module-prefixed ones.
- **`defaultMemoryWorkspace`/`defaultMemoryKind`/`defaultMemorySearchK`** (declared in
  `handlers_memory.go`) are reused by `handlers_agent.go` and
  `handlers_consolidate.go` so `AGENT.REMEMBER`/`AGENT.RECALL`/`SUMMARY.CREATE` default
  identically to `MEMORY.PUT`/`MEMORY.SEARCH` — don't redeclare a second copy of these
  constants in a new file.

## Testing

```bash
go test ./internal/server/resp/... -race
```

`-race` matters: `TestPUnsubscribeStopsDelivery`-style pub/sub tests and anything
touching `ClientState.subCh`/the forwarder goroutine in `handlers_pubsub.go` exercise
real concurrency. Tests assert *exact RESP wire bytes* (including error-string
prefixes) rather than just checking a Go-level return value — match that style for a
new handler's tests; a test that only checks "no error" would miss a wire-format
regression a real client would choke on. `_test.go` files are one per handler family,
mirroring the `handlers_*.go` split.

## Package-specific gaps

- `INCR`/`INCRBY`/`DECR`/`DECRBY` (`handlers_string.go`) are not atomic under
  concurrent writers to the same key — `Get` then `Set` are two separate `Engine`
  calls, not one shard-locked read-modify-write, unlike `HINCRBY`. Documented at
  `incrBy`'s doc comment; closing this would need a dedicated `IncrBy` method on
  `storage.Engine`.
- `FLUSHALL` only flushes the caller's own workspace (same as `FLUSHDB`) — `Engine`
  has no "list all workspaces" API (see `handlers_generic.go`).
