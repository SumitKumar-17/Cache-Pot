# Redis Compatibility

Cache-Pot's adoption pitch is that it's a drop-in for RESP2 clients. This
page is the honest, explicit accounting of what that does and doesn't mean
**today**. Nothing on this page describes planned work as if it already
exists — see the [release history](/roadmap/) for what's actually shipped.

## What "Redis-compatible" means today

- The RESP2 wire protocol: encoding, decoding, and pipelining (multiple
  commands sent back-to-back, replies batched before a flush).
- The core data-structure commands: strings, hashes, lists, sets, and sorted
  sets. See the [command reference](/commands/) for the exact list.
- TTL semantics: `EXPIRE`/`PEXPIRE`/`TTL`/`PTTL`/`PERSIST`, with both active
  and passive expiry (see [Storage Engine](/architecture/storage-engine)).
- Transactions: `MULTI`/`EXEC`/`DISCARD`/`WATCH`/`UNWATCH`, with
  optimistic-locking semantics matching Redis (a changed watched key aborts
  `EXEC`).
- Pub/Sub: `SUBSCRIBE`/`UNSUBSCRIBE`/`PSUBSCRIBE`/`PUBLISH`.
- Standard Redis error shapes for common cases, e.g. `WRONGTYPE Operation
  against a key holding the wrong kind of value` when a command is used
  against a key of the wrong data type.
- Any existing RESP2 client library (go-redis, redis-py, ioredis, jedis,
  node-redis, etc.) can connect and use the commands above without special
  handling — see the [quickstart](/getting-started/quickstart).

## What is explicitly NOT supported yet

- **RESP3** — `HELLO` only negotiates RESP2; a RESP3 handshake request is
  rejected cleanly rather than silently downgraded or partially honored.
- **Lua scripting** (`EVAL`/`EVALSHA`/`SCRIPT`) — not implemented.
- **Cluster mode** (`CLUSTER *`) — not implemented; Cache-Pot runs as a
  single process today.
- **Replication** (`REPLICAOF`/`SLAVEOF`, the replication protocol) — not
  implemented.
- **Persistence** — no RDB snapshotting, no AOF. See "Volatile, in-memory
  only" below.
- **Bitmaps** (`SETBIT`/`GETBIT`/`BITCOUNT`/etc.) — not implemented.
- **Geo commands** (`GEOADD`/`GEOSEARCH`/etc.) — not implemented.
- **Streams** (`XADD`/`XREAD`/etc.) — not implemented.
- Multiple logical databases — `SELECT` exists for client-library
  compatibility but only accepts database `0`.

If a client library or tool depends on any of the above, it will either fail
outright or silently no-op depending on how that client handles an unknown
command — Cache-Pot does not attempt to fake support for any of these.

## Volatile, in-memory only

Cache-Pot's storage (`internal/storage/memstore`) is **entirely in-memory**.
There is no write-ahead log, no snapshotting, and no on-disk representation
of any kind:

- **All data is lost when the process restarts** — a crash, a deploy, a
  `docker compose down`, or a plain `kill` all have the same effect: an
  empty store on the next start.
- The `cachepot-data` volume declared in the shipped
  `docker-compose.yml` is a reserved no-op today — nothing is actually
  persisted to it.
- Do not point Cache-Pot at any workload where losing all cached/stored
  data on restart is unacceptable.

Persistence isn't built yet, and isn't currently scoped as upcoming work;
treat Cache-Pot as strictly a volatile cache/store until that changes.

## Where this goes next

None of the gaps above are permanent design decisions — they're simply not
built yet, and nothing beyond the current version is currently scoped. See
the [release history](/roadmap/) for what's already shipped, and the
[landing page](/) for the overall "memory engine, not Redis clone" framing
this compatibility layer serves.
