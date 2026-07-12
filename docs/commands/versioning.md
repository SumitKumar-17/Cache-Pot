# Versioning Commands

::: info Planned — Phase 7
This command is designed but not implemented yet. See the
[roadmap](/roadmap/) for details.
:::

| Command | Summary |
|---|---|
| `MEMORY.HISTORY` | Fetch the version history of a memory item |

## What this will do

Phase 7 is a cross-cutting retrofit of a `workspace` dimension across every
subsystem built in Phases 1-6, plus full memory versioning: every write to a
memory item retrievable by history. `MEMORY.HISTORY` will let a caller ask
"what did this memory item look like at an earlier point in time" — the
building block for point-in-time reads like "what did the agent know
yesterday."

This is also the phase that grows Phase 1's single shared `--password` /
`CACHEPOT_PASSWORD` auth (see [Configuration](/getting-started/configuration))
into full per-workspace auth/ACLs.

None of this exists in the codebase today. See
[internal/tenancy](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/tenancy)
for the current scaffolding.
