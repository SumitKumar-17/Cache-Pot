# Command Reference

This page lists every Cache-Pot command — **all of them are real as of v0.7.0**.
It is generated directly from
[`api/commands.yaml`](https://github.com/SumitKumar-17/cache-pot/blob/main/api/commands.yaml),
the single source of truth for the command surface, so it can't silently
drift out of sync with that file.

- **✅ Real** — implemented today. (v0.5.0 added no new RESP commands — it added an
  optional `COST` argument to two existing ones, plus HTTP endpoints and config flags;
  see [Observability](/getting-started/observability).)
- **🔶 Planned** — designed and scoped, not yet implemented. This convention stays
  documented for any future command; running a genuinely unimplemented command always
  fails with an unknown-command error.

For narrative, per-category documentation (with examples), see:

- [Connection](/commands/connection) — real, v0.1.0
- [Generic (Keys/TTL)](/commands/generic) — real, v0.1.0
- [Strings](/commands/strings) — real, v0.1.0
- [Hashes](/commands/hashes) — real, v0.1.0
- [Lists](/commands/lists) — real, v0.1.0
- [Sets](/commands/sets) — real, v0.1.0
- [Sorted Sets](/commands/sorted-sets) — real, v0.1.0
- [Pub/Sub & Transactions](/commands/pubsub-and-transactions) — real, v0.1.0
- [Semantic Cache](/commands/semantic-cache) — real, v0.2.0 (+ v0.5.0's `COST` option)
- [Tool Cache](/commands/tool-cache) — real, v0.2.0
- [Vector Search](/commands/vector) — real, v0.3.0
- [Agent Memory](/commands/memory) — real, v0.4.0
- [MCP Server](/getting-started/mcp-server) — real, v0.3.0-v0.7.0 (tools, not RESP commands)
- [Observability](/getting-started/observability) — real, v0.5.0 (`/metrics`, `/stats`,
  `/dashboard`, eviction — not RESP commands)
- [Consolidation & Knowledge Graph](/commands/graph) — real, v0.6.0 (quality depends
  on the [completion provider](/getting-started/completions))
- [Versioning](/commands/versioning) — real, v0.7.0
- [Workspaces & Multi-Tenancy](/getting-started/workspaces) — real, v0.7.0 (auth/
  isolation, not its own RESP commands)

## Full command table

<!--@include: ./_generated-table.md-->
