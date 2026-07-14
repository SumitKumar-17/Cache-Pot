# internal/storage/memstore

## Role

The one real implementation of `storage.Engine`: a sharded, in-memory map with
workspace-namespaced keys, passive + active TTL expiry, optional `--max-entries`
eviction, and `MULTI`/`EXEC`/`WATCH` support. Everything a RESP command touches when it
reads or writes a key ends up here. If a bug report is "a command returns the wrong
value/type/error," this is almost always where the fix lives (see `internal/storage/
AGENTS.md` for when the fix instead belongs in the `Engine` interface itself).

## Sharding scheme

- `defaultNumShards = 32` (used when `New`/`NewWithClock` is given `numShards <= 0`).
  Each shard (`shard` in `shard.go`) is an independent `map[string]*Entry` guarded by
  its own `sync.RWMutex` — most operations only ever lock one shard.
  - Routing: `nsKey(workspace, key)` builds the composite key `workspace + "\x00" +
    key` (NUL as separator — not a legal byte in a RESP bulk string key in practice, so
    it's collision-safe), then `shardIndex` hashes that composite key with FNV-32a and
    takes it mod `n`. **Routing is by composite key, not by workspace or bare key
    alone** — two different workspaces' same-named key almost always land in different
    shards, and there's no guarantee two keys in the same workspace land in the same
    shard either.
  - `Rename` is the one operation that touches two shards at once (old key's shard,
    new key's shard) when they differ. It locks both, always in ascending shard-index
    order, specifically to avoid a lock-order deadlock against a concurrent rename in
    the opposite direction — if you touch `Rename`, preserve that ordering.

## `nsKey` and workspace scoping

`nsKey` is the *entire* workspace-isolation mechanism at the storage layer — there is no
separate per-workspace map or index. Operations that need "every key in workspace X"
(`Keys`, `Scan`, `FlushDB`) fall back to a full cross-shard scan filtering on the
`workspace + "\x00"` prefix (`keysForWorkspace`) — there's no reverse index from
workspace to its keys, so these are O(total keys across all workspaces), not O(keys in
that workspace). This is fine at today's scale but is the thing to know before assuming
`KEYS`/`SCAN`/`FLUSHDB` are cheap.

## Transaction atomicity: the global-mutex tradeoff

`Store.globalMu` (a single `sync.Mutex`) is held for the *entire* body of `Exec(fn)` —
i.e., all of a `MULTI`/`EXEC` block runs under one process-wide lock, serializing every
transaction against every other transaction (and against nothing else — non-transaction
commands still only take their own shard's lock and can interleave with a transaction's
individual operations at the shard level, though `Exec`'s caller is expected to hold
`globalMu` for the whole sequence to get atomicity). This is a deliberate
simplicity-over-throughput choice made once, documented at the `globalMu` field and the
`Exec` method in `store.go`, to avoid a cross-shard lock-ordering protocol for a
low-throughput feature. Don't "fix" this into finer-grained locking without discussing
the tradeoff — it's the same kind of considered tradeoff as the bounded-sampling ones
below, not an oversight.

`WatchVersion`/the per-key `versions` map (in `shard.go`) back `WATCH`'s optimistic
locking. Non-obvious: the version counter lives independently of the `Entry` object —
deleting and recreating a key still increments its version (Redis semantics: a
`DEL`+`SET` on a watched key must still abort a transaction watching it), so don't
"simplify" versioning to live on `Entry` itself, it would break across delete/recreate
cycles.

## TTL: passive vs. active expiry

- **Passive** (correctness-bearing): every read path (`get`, `getForWriteLocked`) checks
  `Entry.expired(now)` and treats an expired entry as absent, lazily deleting it on the
  write-path variant (`getForWriteLocked`). This alone guarantees an expired key is
  never returned, regardless of whether the reaper has gotten to it.
  - `now` is `s.now()`, a function field, not `time.Now()` called directly — this is
    what lets tests inject a controllable clock (`NewWithClock`) instead of sleeping in
    real time.
- **Active** (bounded, not correctness-bearing): `Store` implements `ttl.Sweepable`
  (`NumShards`/`SweepShard`) and starts a `ttl.Reaper` goroutine in `New`/`NewWithClock`,
  ticking every 100ms, sampling up to 20 keys-with-TTL per shard per tick
  (`shard.ttlKeys`, populated/cleared alongside `data` by `setEntryLocked`/
  `setExpiryLocked`/`deleteEntryLocked` so the reaper never has to scan the full shard).
  This exists only to bound memory held by expired-but-unread keys — see
  `internal/storage/ttl`'s own AGENTS.md for the reaper itself.
  - `Close()` cancels the reaper's context and blocks until its goroutine actually
    exits (`<-s.reaperDone`) — any new `Store` construction path must preserve this or
    tests/process shutdown can leak the goroutine.

## `--max-entries` / eviction

- `entryCount` is an exact, atomically-maintained, server-wide (not per-workspace)
  count of live entries, kept in sync by every insert/delete path via
  `shard.setEntryLocked`/`deleteEntryLocked` (both take a pointer to the same counter,
  shared across all shards) — DEL, HSET/LPUSH/etc. first-write, FLUSHDB/FLUSHALL,
  RENAME, and reaper-driven expiry all flow through those two functions, so none of
  those call sites needs to remember to touch the counter itself. If you add a new
  mutation path, route it through `setEntryLocked`/`deleteEntryLocked` rather than
  writing to `shard.data` directly, or the count will drift.
- `WithMaxEntries(n)` (`n <= 0` = unlimited, the default) plus `WithEvictionPolicy`
  (defaults to `eviction.LRU{}`) and `WithOnEvict` (a callback for
  `internal/observability` metrics, wired by `internal/server`, keeping this package
  decoupled from observability) configure eviction. The actual scoring is
  `internal/eviction.Policy.Score(Signals{LastAccess, Now, AccessCount})` — this
  package just supplies the signals from `Entry.LastAccess`/`Entry.AccessCount`.
  - **Non-obvious invariant** (`evictIfNeededLocked` in `store.go`): eviction only ever
    picks a victim from the *same shard* that's about to receive the new key —
    `pickEvictionVictimLocked` samples up to `evictionSampleSize` (20) entries from that
    one shard, never a global scan. If that shard happens to be empty, eviction is a
    no-op even though the global count is already over the cap; the count keeps
    growing until some later insert lands in a shard that already has an occupant to
    evict. Net effect: the resident-key floor is `max(maxEntries, ~one entry per
    populated shard)`, not an exact `maxEntries` — with the default 32 shards,
    `--max-entries 5` converges to roughly 32 resident keys, not 5. This is documented
    at `evictIfNeededLocked` and is the memstore-specific instance of the repo-wide
    "eviction approximate below shard count" gap in root AGENTS.md — if you're chasing
    a "why didn't it evict down to N" bug, this is almost certainly why, not a real bug.
  - Eviction only triggers on genuinely inserting a *new* key (`ensureKind`'s
    not-found branch, and `Set`'s not-`exists` branch) — updating an existing key never
    triggers it, since that doesn't grow `entryCount`.

## Other conventions/gotchas

- `pattern.go`'s `globMatch` is a from-scratch Redis-glob implementation (`*`, `?`,
  `[...]` with `^` negation and `a-z` ranges, minimal `\` escaping) used by
  `KEYS`/`SCAN MATCH`. It does not attempt full Redis escaping semantics — if a bug
  report involves an edge-case glob pattern, check here before assuming it's a
  handler-layer bug.
- `Scan` recomputes and sorts the *entire* matching keyspace on every call, using sort
  position as the cursor — deterministic and test-friendly, but O(n log n) per call
  rather than Redis's real stable reverse-binary iteration. This is the workspace-scan
  cost mentioned above, not a separate issue.
- Every returned `[]byte`/map value is a defensive copy (`append([]byte(nil), ...)`) —
  callers never get a slice/map aliasing internal storage. Preserve this in any new
  method; it's what makes it safe for RESP handlers to hold onto returned values across
  goroutine boundaries.

## Testing

```
go test ./internal/storage/memstore/... -race
```

`-race` is not optional here — `TestConcurrentAccess` in `store_test.go` spins up 50
goroutines hammering `Set`/`Get`/`HSet`/`HGet`/`Del` concurrently across 8 shards
specifically to catch shard-locking bugs, and several other tests
(`TestShardingDistributesKeys`, `TestActiveReaperSweepsExpiredKeys`) run the real
background reaper goroutine concurrently with the test body.

Test double: `testClock` (in `store_test.go`) is a mutex-guarded injectable clock passed
via `NewWithClock(numShards, clock.Now, opts...)` — this is how `TestTTLExpiryWithInjectableClock`,
`TestMaxEntriesEvictsLRUVictim`, etc. assert TTL/recency behavior deterministically
without real `time.Sleep`. When adding a new time-dependent test, use `newTestStore(t,
clock, numShards)` (also in `store_test.go`) rather than `New(...)` + real sleeps.

## Known gaps specific to this package

- Eviction's shard-local victim search means the effective resident-key floor is
  roughly one entry per populated shard, not an exact `maxEntries` — see the
  `--max-entries` section above (this is the memstore-specific detail behind root
  AGENTS.md's more general "eviction approximate below shard count" line).
- `Keys`/`Scan`/`FlushDB` are full cross-shard scans filtered by workspace prefix —
  there is no reverse index from workspace to its keys, so these are O(all keys in the
  store), not O(keys in the target workspace).
