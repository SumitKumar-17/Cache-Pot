# Connection Commands

::: info Phase 1 — Real
Every command on this page is implemented today.
:::

Connection commands handle the handshake, liveness checks, authentication,
and basic introspection for a client connection.

| Command | Summary |
|---|---|
| `PING` | Check server liveness / measure round-trip latency |
| `ECHO` | Echo back the given argument |
| `SELECT` | Select a logical database (Phase 1 only accepts db `0`) |
| `HELLO` | Protocol handshake (RESP2 only in Phase 1; RESP3 is rejected cleanly) |
| `AUTH` | Authenticate the connection against the configured password |
| `CLIENT` | Client introspection/control (`GETNAME`/`SETNAME` in Phase 1) |
| `COMMAND` | Minimal command-table introspection |
| `INFO` | Minimal server info/stats sections |
| `QUIT` | Close the connection |

## Notes

- `SELECT` exists for client-library compatibility, but Cache-Pot has a
  single logical database (`0`) in Phase 1 — there's no `SELECT 1`,
  `SELECT 2`, etc. yet. Multiple isolated keyspaces arrive with Phase 7's
  multi-tenancy work (see the [roadmap](/roadmap/)), scoped as workspaces
  rather than numbered databases.
- `HELLO` only negotiates RESP2. Clients requesting RESP3 get a clean
  rejection rather than a silently-broken RESP3 session — see [Redis
  compatibility](/architecture/redis-compatibility).
- `AUTH` is only required if the server was started with `--password` /
  `CACHEPOT_PASSWORD` set — see [Configuration](/getting-started/configuration).

## Example

```bash
redis-cli -p 6380 PING
# PONG
redis-cli -p 6380 ECHO "hello"
# "hello"
redis-cli -p 6380 -a s3cret AUTH s3cret
# OK
```
