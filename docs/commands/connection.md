# Connection Commands

::: info v0.1.0 — Real
Every command on this page is implemented today.
:::

Connection commands handle the handshake, liveness checks, authentication,
and basic introspection for a client connection.

| Command | Summary |
|---|---|
| `PING` | Check server liveness / measure round-trip latency |
| `ECHO` | Echo back the given argument |
| `SELECT` | Select a logical database (only accepts db `0`) |
| `HELLO` | Protocol handshake (RESP2 only; RESP3 is rejected cleanly) |
| `AUTH` | Authenticate the connection against the configured password |
| `CLIENT` | Client introspection/control (`GETNAME`/`SETNAME`) |
| `COMMAND` | Minimal command-table introspection |
| `INFO` | Minimal server info/stats sections |
| `QUIT` | Close the connection |

## Notes

- `SELECT` exists for client-library compatibility, but Cache-Pot has a
  single logical database (`0`) — there's no `SELECT 1`, `SELECT 2`, etc.
  Isolated keyspaces are scoped as **workspaces**, not numbered databases —
  see [Workspaces & Multi-Tenancy](/getting-started/workspaces).
- `HELLO` only negotiates RESP2. Clients requesting RESP3 get a clean
  rejection rather than a silently-broken RESP3 session — see [Redis
  compatibility](/architecture/redis-compatibility).
- `AUTH` is required if the server was started with `--password` /
  `CACHEPOT_PASSWORD`, or with `--workspace-credentials` (in which case the
  password also determines which workspace the connection is authorized
  for) — see [Configuration](/getting-started/configuration) and
  [Workspaces & Multi-Tenancy](/getting-started/workspaces).

## Example

```bash
redis-cli -p 6380 PING
# PONG
redis-cli -p 6380 ECHO "hello"
# "hello"
redis-cli -p 6380 -a s3cret AUTH s3cret
# OK
```
