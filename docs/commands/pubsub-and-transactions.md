# Pub/Sub & Transactions

::: info v0.1.0 — Real
Every command on this page is implemented today.
:::

## Pub/Sub

| Command | Summary |
|---|---|
| `SUBSCRIBE` | Subscribe to one or more channels |
| `UNSUBSCRIBE` | Unsubscribe from one or more channels (or all, if none given) |
| `PUBLISH` | Publish a message to a channel |
| `PSUBSCRIBE` | Subscribe to channels matching a glob pattern |
| `PUNSUBSCRIBE` | Unsubscribe from one or more patterns (or all, if none given) |

```bash
# terminal 1
redis-cli -p 6380 SUBSCRIBE updates

# terminal 2
redis-cli -p 6380 PUBLISH updates "hello subscribers"
```

## Transactions

| Command | Summary |
|---|---|
| `MULTI` | Begin queueing commands for atomic execution |
| `EXEC` | Execute all commands queued since `MULTI` |
| `DISCARD` | Discard all commands queued since `MULTI` |
| `WATCH` | Watch keys, aborting a subsequent `EXEC` if any changed |
| `UNWATCH` | Forget all watched keys |

```bash
redis-cli -p 6380
> WATCH balance
OK
> MULTI
OK
> DECRBY balance 10
QUEUED
> INCRBY balance:savings 10
QUEUED
> EXEC
1) (integer) 90
2) (integer) 110
```

If `balance` was modified by another client between `WATCH` and `EXEC`,
`EXEC` returns a null reply and none of the queued commands run — the same
optimistic-locking contract as Redis.

### Implementation note: the global-lock tradeoff

Every key tracks a per-key mutation-version counter used by `WATCH`.
Transaction bodies (the queued commands run at `EXEC` time) execute while
holding a **single global mutex** across the whole store, rather than a
per-key or per-shard locking protocol. This is a deliberate
simplicity-over-throughput tradeoff: it avoids lock-ordering/deadlock
complexity for what is, at today's traffic levels, a low-throughput feature.
Non-transactional commands are unaffected — they still use Cache-Pot's
normal sharded locking (see [Storage Engine](/architecture/storage-engine))
and run concurrently with each other. Only the body of a `MULTI`/`EXEC`
block serializes against other transaction bodies. This is a candidate to
revisit if transaction throughput ever becomes a bottleneck — it hasn't
needed to be so far.
