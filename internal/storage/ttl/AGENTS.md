# internal/storage/ttl

## Role

Active-expiry background reaper for `internal/storage/memstore`. Passive expiry (a read
checking `ExpiresAt` against now) already guarantees correctness — nothing expired is
ever *returned*; this package exists only to bound how long an expired key that's never
read again can linger in memory, taking up space and inflating `EntryCount`.

## Contract

- `Sweepable` is the only coupling point to memstore: `NumShards() int` and
  `SweepShard(shardIndex, sampleSize int) (expiredRemoved int)`. It's deliberately
  narrow — this package has zero import-level dependency on memstore internals (or vice
  versa: memstore imports `ttl`, not the other way around). If you need the reaper to
  do something new, extend `Sweepable`, don't reach into memstore from here.
- `New(store, interval, sampleSize)` — `interval <= 0` defaults to 100ms,
  `sampleSize <= 0` defaults to 20. Both defaults are covered by tests
  (`TestNewDefaultsIntervalAndSampleSize`, `TestNewNegativeValuesAlsoDefault`); if you
  change either default, update those tests' expectations, not just the code.
- `Run(ctx)` is meant to be started as `go reaper.Run(ctx)` and ticks every shard once
  per interval until `ctx` is canceled. It sweeps shards **sequentially within a tick**,
  not concurrently — a tick's total duration is `NumShards() * (per-shard sweep cost)`.
  There's no re-entrancy guard against overlapping ticks; a very slow `SweepShard` under
  a very short `interval` could in principle pile up, but that's a memstore lock-hold
  concern, not something this package guards against.
- Each tick's sweep is a bounded sample (`sampleSize` keys-with-TTL per shard), never a
  full scan — this package is the canonical example of this repo's "bounded sampling
  over full scans" convention (see root AGENTS.md); `memstore`'s eviction sampling
  mirrors the same philosophy deliberately.

## Testing

```
go test ./internal/storage/ttl/... -race
```

No real memstore or real clock involved: `reaper_test.go` uses `fakeSweepable`, a
mutex-guarded double that just records call counts and the last `sampleSize` it was
given per shard — no import of `memstore` at all. Tests rely on real wall-clock
`time.Sleep`/ticker timing (millisecond-scale intervals), not an injectable clock, so
they're inherently timing-sensitive; `-race` matters because `fakeSweepable` is read
from the test goroutine while `Run`'s own goroutine writes to it. If a reaper test
becomes flaky under load, look at whether the sleep/timeout margins in
`reaper_test.go` need widening before assuming a real bug.

## Known gaps

None specific to this package — see root AGENTS.md and `memstore`'s AGENTS.md for the
shard-count-dependent eviction caveat, which is a memstore property, not a `ttl`
property (this package doesn't evict, only expires).
