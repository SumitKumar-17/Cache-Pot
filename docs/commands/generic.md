# Generic Commands (Keys / TTL)

::: info Phase 1 — Real
Every command on this page is implemented today.
:::

Generic commands operate on keys regardless of the type of value stored at
them: existence, deletion, expiry, type introspection, and iteration.

| Command | Summary |
|---|---|
| `DEL` | Delete one or more keys |
| `EXISTS` | Count how many of the given keys exist |
| `EXPIRE` | Set a key's time-to-live in seconds |
| `PEXPIRE` | Set a key's time-to-live in milliseconds |
| `TTL` | Get a key's remaining time-to-live in seconds |
| `PTTL` | Get a key's remaining time-to-live in milliseconds |
| `PERSIST` | Remove a key's expiry, making it persistent |
| `TYPE` | Get the data type stored at a key |
| `KEYS` | Find all keys matching a glob pattern |
| `SCAN` | Cursor-based incremental iteration over the keyspace |
| `RENAME` | Rename a key |
| `FLUSHDB` | Remove all keys from the current workspace |
| `FLUSHALL` | Remove all keys from all workspaces (Phase 1: single workspace, so this behaves the same as `FLUSHDB`) |

## TTL semantics

Cache-Pot expires keys two ways, both active today (see [Storage
Engine](/architecture/storage-engine) for the implementation):

- **Passive expiry**: any read or write on a key checks its expiry first and
  treats an expired key as absent.
- **Active expiry**: a background reaper periodically samples keys that have
  a TTL set and deletes any that have expired, so expired keys don't linger
  in memory indefinitely just because nothing reads them.

```bash
redis-cli -p 6380 SET session:abc "..."
redis-cli -p 6380 EXPIRE session:abc 60
# (integer) 1
redis-cli -p 6380 TTL session:abc
# (integer) 60
redis-cli -p 6380 PERSIST session:abc
# (integer) 1
redis-cli -p 6380 TTL session:abc
# (integer) -1
```

## SCAN semantics

`SCAN`'s cursor is a Phase 1 simplification: rather than Redis's reverse
binary iteration, Cache-Pot recomputes and sorts the full matching keyspace
per call and uses the sort position as the cursor. This gives fully
deterministic ordering, at the cost of being `O(n log n)` per call — fine at
Phase 1 key-count scales, and a candidate to revisit if that changes.

```bash
redis-cli -p 6380 SCAN 0 MATCH "user:*" COUNT 10
```
