# Workspaces & Multi-Tenancy

::: tip v0.7.0 — real
Real, enforced workspace isolation — not just a partitioning label.
:::

## What a workspace is

`workspace` has been threaded through Cache-Pot's storage layer since its very first
version (check `internal/storage.Engine`'s method signatures — every one takes a
`workspace string` first parameter), but until v0.7.0 it was purely a partitioning
label: any connection could read or write any workspace string it typed. v0.7.0 made
it a real tenant boundary, opt-in, on top of `AUTH`.

## Enabling it

By default (no change from earlier versions): a single shared `--password` (or none at all),
and every workspace string is unrestricted. To turn on real isolation, configure
per-workspace credentials instead:

```bash
./bin/cachepotd --workspace-credentials "acme:secret1,other:secret2"
```

`--workspace-credentials` / `CACHEPOT_WORKSPACE_CREDENTIALS` is a comma-separated list
of `workspace:password` pairs. It's **mutually exclusive with `--password`** — running
with both set is a startup error, since they express two different, incompatible auth
models.

Once configured:
- Every connection must `AUTH <password>` before any other command — there is no
  unauthenticated "default" workspace, unlike single-password mode.
- The password determines the workspace: `AUTH secret1` locks that connection to
  `acme` for its lifetime. Re-`AUTH`ing with a different password switches which
  workspace the connection is authorized for.
- Any command taking an explicit workspace/namespace argument — `MEMORY.*`,
  `AGENT.*`, `VECTOR.*`, `GRAPH.*` — is rejected with a `NOPERM` error if the argument
  doesn't match the connection's authenticated workspace. The core KV commands
  (`GET`/`SET`/`HSET`/etc.) have no explicit workspace argument at all; they simply
  operate within whichever workspace the connection is authenticated for.

```bash
redis-cli -p 6380 -a secret1 MEMORY.PUT bot "note" WORKSPACE acme
# OK — this connection authenticated as "acme"

redis-cli -p 6380 -a secret1 MEMORY.PUT bot "note" WORKSPACE other
# (error) NOPERM this connection is not authorized for workspace "other"
```

## What's NOT workspace-scoped

`CACHE.SEMANTIC`, `CACHE.PROMPT`, and `TOOL.CACHE` have no workspace concept at all —
they're global caches, by design (matching the roadmap's own scoping: workspace
isolation covers "the KV keyspace, vector namespaces, memory store, and graph," not
the LLM-response caches). If you need tenant separation for those too, that's not built
yet.

::: warning MCP has no authentication layer
The [native MCP server](/getting-started/mcp-server) calls the same shared stores
directly and has no concept of `AUTH` or workspace authorization at all — an MCP tool
call can read/write any workspace regardless of what `--workspace-credentials` is
configured. This is an honest, currently-unaddressed gap, not an oversight to be
quietly worked around — if you're using `--workspace-credentials` for real isolation,
don't expose the MCP port to untrusted callers.
:::

See [`internal/auth`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/auth)
for the implementation, and [configuration](/getting-started/configuration) for the
full flag reference.
