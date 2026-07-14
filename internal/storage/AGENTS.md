# internal/storage

## Role

Defines `storage.Engine`, the single seam between the RESP command layer
(`internal/server/resp`) and any concrete data-structure backend. `internal/storage/
memstore` is the only concrete implementation; `internal/storage/ttl` is a helper the
memstore implementation drives. RESP handlers depend on `resp.Deps.Engine` (the
interface), never on `*memstore.Store` directly — see root `AGENTS.md`'s "the seam to
respect."

This package holds no state and no logic beyond the interface + a handful of
Redis-shaped sentinel errors. If you're fixing a bug that manifests as wrong behavior,
the fix almost always belongs in `memstore`, not here — only touch this package when the
*contract* itself needs to change (new method, changed signature, new error case).

## Contract (engine.go)

- Every method takes `workspace string` as the first parameter, even ones that don't
  obviously need multi-tenancy yet. This is the repo-wide "workspace threaded through
  everything" convention (see root AGENTS.md) — don't add a method that omits it.
- `Get`/`Set` (and anything else touching a possibly-wrong-typed key) return
  `storage.ErrWrongType`, not a silent not-found, when the key exists as a different
  kind. This is the one place the interface deviates from a plain `(val, ok)` shape —
  RESP handlers translate `ErrWrongType` into the literal `WRONGTYPE ...` Redis error
  string. If you add a new typed accessor, preserve this.
- Other sentinel errors: `ErrNotInteger` (HINCRBY etc. on a non-numeric field),
  `ErrNotFloat`, `ErrIndexOutOfRange` (LSET on an out-of-bounds index), `ErrNoSuchKey`
  (LSET on an absent key). Reuse these rather than inventing new error values — the RESP
  layer pattern-matches on them.
- `WatchVersion`/`Exec` exist purely to support `MULTI`/`WATCH`/`EXEC`
  (`internal/server/resp/handlers_tx.go`). `Exec` runs the transaction body while
  holding the engine's global transaction lock — see `memstore`'s AGENTS.md for the
  tradeoff this implies.
- `Close` stops any background goroutines the engine owns (memstore's TTL reaper). Any
  new Engine implementation must make `Close` actually stop what it started, or
  processes leak goroutines across restarts/tests.

## Extending the interface

This project's normal way to add a storage capability is **widen the interface, fix up
every call site** — not bolt on a parallel method (see root AGENTS.md). If you add a
new command family that needs a new Engine method: add it to the interface in
`engine.go`, implement it in `memstore/store.go`, and update any other Engine
implementation (currently none exist, but check before assuming).

## Testing

No test file lives directly in this package (it's an interface + sentinel errors,
nothing to unit-test in isolation). Verify a contract change by running the
implementation's tests:

```
go test ./internal/storage/... -race
```

## Known gaps

None specific to this package beyond what's in the root AGENTS.md's "Known, honest
gaps" (no persistence, approximate eviction, FLUSHALL scoped to caller's workspace) —
those are properties of `memstore`, inherited here only because this is the interface it
satisfies.
