# internal/eviction

## Role

Defines the scoring policies used to pick which entry to reclaim when
`internal/storage/memstore`'s `--max-entries` cap is hit. This package has no
dependency on storage/memstore — it only scores an entry given a primitive
`Signals` struct — which is what lets memstore import it without a cycle.

## Key types

- **`Policy` interface** (`policy.go`): one method, `Score(Signals) float64`.
  Higher score = evicted first. Any new policy must follow this convention —
  don't flip it in a new implementation.
- **`Signals`** (`policy.go`): `LastAccess`, `Now`, `AccessCount`, `CostHint`,
  `Importance`. Not every field is populated by every caller — memstore never
  sets `CostHint`/`Importance` (it has no notion of cache cost or caller
  importance), so they're always zero from that call site. **Treat zero as
  "no signal," not "explicitly low."** Any new `Policy` must degrade sanely
  when a field is zero (see `Weighted.Score`'s `1/(1+x)` shape, which
  saturates at its max contribution — most evictable-by-that-signal — when
  the underlying value is 0).
- **`LRU`** (`lru.go`): the default, recency-only policy — score is just age
  in seconds since `LastAccess`. Stateless value type.
- **`Weighted`** (`weighted.go`), shipped v0.5.0: a composite policy summing
  four independently-normalized (`x/(1+x)` or `1/(1+x)`) contributions —
  recency, frequency, cost, importance — each weighted by
  `Weighted.Weights["recency"|"frequency"|"cost"|"importance"]`. Missing keys
  are weight 0. Nil/empty `Weights` falls back to `DefaultWeights`
  (`{recency: 0.6, frequency: 0.4}`). The normalization is deliberate: it
  keeps each contribution in roughly `[0,1)` so no signal's raw units (seconds
  vs. small integer counts vs. an arbitrary cost scale) dominates the sum
  purely by magnitude, and guarantees a finite (non-NaN/Inf) score even for
  all-zero `Signals`.

## Integration point (read before touching either side)

`memstore.Store` holds a `policy eviction.Policy` field (defaults to
`eviction.LRU{}` even when eviction is disabled) and calls
`shard.pickEvictionVictimLocked(policy, now, evictionSampleSize)` from
`Store.evictIfNeededLocked` (`internal/storage/memstore/store.go`) whenever a
brand-new key would push `entryCount` over `maxEntries`. That victim search:

- **Never leaves the shard receiving the new insert.** It samples up to
  `evictionSampleSize` (20) entries in *that one shard only*, not a global
  scan/sort — same bounded-sampling philosophy as the TTL reaper.
- **Is a no-op if that shard happens to be empty**, even if the store is
  globally over `maxEntries` — eviction only "bites" once a shard already has
  an occupant. With the default 32 shards, this makes the resident-key floor
  roughly `max(maxEntries, one entry per populated shard)`, not an exact
  `maxEntries` — e.g. `--max-entries 5` converges to ~32 resident keys, not 5.
  This is the documented, accepted "approximate below shard count" gap
  (root `AGENTS.md`'s Known Gaps) — it originates entirely in memstore's
  per-shard victim search, not in this package's scoring math.

If you're asked to make eviction exact below the shard count, that requires
changing memstore's victim-search scope (global vs. per-shard), not anything
in this package — don't try to fix it by changing `Policy`/`Signals`.

## Conventions/gotchas specific to this package

- Both `LRU` and `Weighted` are pure functions of `Signals` — no internal
  state, no locking, safe to share a single instance across shards/goroutines
  (which memstore does: one `policy` field on `*Store`, called from every
  shard).
- `Weighted.Score` is defined on `*Weighted` (pointer receiver) while `LRU`'s
  is on the value type `LRU` — match whichever receiver style suits a new
  policy's mutability, both are already accepted by the `Policy` interface.

## Testing

```
go test ./internal/eviction/...
```

No `-race` concerns (pure, stateless scoring functions — no shared mutable
state within the package). No mocks/test doubles needed; `weighted_test.go`
constructs `Signals` values directly and asserts score ordering/finiteness.
`LRU` has no dedicated test file — its behavior is exercised indirectly via
memstore's eviction tests.

## Limitations

- No policy in this package currently reads real memory pressure (RSS,
  configured byte budget) — `maxEntries` is a raw entry count, not bytes, and
  that's a memstore-level constraint, not something fixable here.
