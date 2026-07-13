# Command Reference

This page lists every Cache-Pot command across all seven roadmap phases —
**all seven are real as of Phase 7**. It is generated directly from
[`api/commands.yaml`](https://github.com/SumitKumar-17/cache-pot/blob/main/api/commands.yaml),
the single source of truth for the command surface, so it can't silently
drift out of sync with that file.

- **✅ Real** — implemented today. (Phase 5 added no new RESP commands — it added an
  optional `COST` argument to two existing ones, plus HTTP endpoints and config flags;
  see [Observability](/getting-started/observability).)
- **🔶 Planned** — designed and scoped in the [roadmap](/roadmap/), not yet
  implemented. This convention stays documented for any future command added beyond
  the original 7-phase roadmap; running a genuinely unimplemented command always fails
  with an unknown-command error.

For narrative, per-category documentation (with examples), see:

- [Connection](/commands/connection) — real, Phase 1
- [Generic (Keys/TTL)](/commands/generic) — real, Phase 1
- [Strings](/commands/strings) — real, Phase 1
- [Hashes](/commands/hashes) — real, Phase 1
- [Lists](/commands/lists) — real, Phase 1
- [Sets](/commands/sets) — real, Phase 1
- [Sorted Sets](/commands/sorted-sets) — real, Phase 1
- [Pub/Sub & Transactions](/commands/pubsub-and-transactions) — real, Phase 1
- [Semantic Cache](/commands/semantic-cache) — real, Phase 2 (+ Phase 5's `COST` option)
- [Tool Cache](/commands/tool-cache) — real, Phase 2
- [Vector Search](/commands/vector) — real, Phase 3
- [Agent Memory](/commands/memory) — real, Phase 4
- [MCP Server](/getting-started/mcp-server) — real, Phases 3-7 (tools, not RESP commands)
- [Observability](/getting-started/observability) — real, Phase 5 (`/metrics`, `/stats`,
  `/dashboard`, eviction — not RESP commands)
- [Consolidation & Knowledge Graph](/commands/graph) — real, Phase 6 (quality depends
  on the [completion provider](/getting-started/completions))
- [Versioning](/commands/versioning) — real, Phase 7
- [Workspaces & Multi-Tenancy](/getting-started/workspaces) — real, Phase 7 (auth/
  isolation, not its own RESP commands)

## Full command table

<!--@include: ./_generated-table.md-->
