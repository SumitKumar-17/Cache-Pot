# Storage Engine

Phase 1's storage backend, `internal/storage/memstore`, is a sharded,
in-memory implementation of the `storage.Engine` interface (see
[Architecture Overview](/architecture/overview)). This page describes how it
actually works — no forward-looking claims about later phases.

## Sharded map

`memstore.Store` holds a fixed number of shards (32 by default), each an
independent `shard` guarded by its own `sync.RWMutex`:

```go
type Store struct {
	shards []*shard
	n      int
	// ...
	globalMu sync.Mutex // see "Transactions" below
}

type shard struct {
	mu       sync.RWMutex
	data     map[string]*Entry
	ttlKeys  map[string]struct{}
	versions map[string]uint64
}
```

Every key is namespaced by workspace before routing: the composite key is
`workspace + "\x00" + key` (NUL is a safe separator since it can't appear in
a RESP bulk string used as a key in practice). That composite key is hashed
with FNV-1a and reduced mod the shard count to pick a shard. This is why
`Rename` — which can move a key from one shard to another — has to lock
shards in ascending index order when the source and destination land on
different shards, to avoid a lock-order deadlock against a concurrent rename
in the opposite direction.

Each `shard` also tracks:

- `ttlKeys` — the subset of keys that currently have a non-nil expiry, so
  the TTL reaper (below) can sample just "keys with TTL" instead of scanning
  the whole shard every tick.
- `versions` — a per-key monotonic mutation counter used by `WATCH`/`EXEC`.
  This is tracked independently of the `Entry` object's lifetime
  specifically so a key's version keeps incrementing across delete+recreate
  cycles — Redis semantics require that `DEL` then `SET` on a watched key
  still aborts a pending transaction.

## TTL: active + passive expiry

Cache-Pot expires keys two ways, both implemented today:

- **Passive expiry**: every read or write path checks `Entry.expired(now)`
  before touching a key, and lazily deletes it (bumping its WATCH version)
  if it's expired. This guarantees correctness — an expired key is never
  observably "alive" — without needing the background reaper to have run
  yet.
- **Active expiry**: `internal/storage/ttl.Reaper` runs on a ticker
  (100ms interval by default) and, each tick, calls `SweepShard` on every
  shard. `SweepShard` examines a bounded sample (20 keys by default) from
  that shard's `ttlKeys` set and deletes any that have expired. Bounding the
  sample size means a tick is never a full-table scan, even on a shard with
  many keys. This exists so an expired key that's never read again still
  gets reclaimed in bounded time, instead of lingering in memory
  indefinitely.

The reaper depends only on a narrow `Sweepable` interface
(`NumShards() int`, `SweepShard(shardIndex, sampleSize int) int`), so
`internal/storage/ttl` has no dependency on `memstore`'s internals (or vice
versa).

## Transactions: the global-lock tradeoff

`MULTI`/`EXEC`/`WATCH` are implemented at the `Engine` level via two pieces:

- `WatchVersion(workspace, key)` reads a key's current mutation-version
  counter, letting the RESP layer snapshot versions at `WATCH` time and
  compare them again at `EXEC` time.
- `Exec(fn func() error) error` runs the queued transaction body while
  holding `Store.globalMu` — a single mutex across the *entire* store, not
  a per-key or per-shard lock.

This is a deliberate Phase 1 tradeoff: a single global mutex avoids the
lock-ordering/deadlock complexity of a proper cross-shard locking protocol,
for what is, at Phase 1 traffic levels, a low-throughput feature (only
`MULTI`/`EXEC` bodies serialize against each other — ordinary,
non-transactional commands still use the normal per-shard locks and run
concurrently with each other and with transaction bodies' individual
operations). It is called out in the source as a candidate to revisit in
Phase 5 if transaction throughput becomes a bottleneck; it hasn't been
revisited yet.

## What this means operationally

- All data lives in process memory. There is no persistence layer in Phase
  1 — see [Redis Compatibility](/architecture/redis-compatibility) for the
  full implications (data is lost on restart, no RDB/AOF).
- Memory usage is bounded only by the host's available RAM and whatever TTLs
  clients set — there's no eviction policy wired in yet beyond what TTL
  expiry removes (`internal/eviction` exists as scaffolding for later
  phases, not active today).
