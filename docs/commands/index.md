# Command Reference

This page lists every Cache-Pot command across all seven roadmap phases —
both what's real today and what's designed but not built yet. It is
generated directly from [`api/commands.yaml`](https://github.com/SumitKumar-17/cache-pot/blob/main/api/commands.yaml),
the single source of truth for the command surface, so it can't silently
drift out of sync with that file.

- **✅ Real** — implemented today, in Phase 1 or Phase 2.
- **🔶 Planned** — designed and scoped in the [roadmap](/roadmap/), not yet
  implemented. Running a planned command against a Phase 1-2 server will fail
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
- [Semantic Cache](/commands/semantic-cache) — real, Phase 2
- [Tool Cache](/commands/tool-cache) — real, Phase 2
- [Vector Search](/commands/vector) — planned, Phase 3
- [Agent Memory](/commands/memory) — planned, Phase 4
- [Knowledge Graph](/commands/graph) — planned, Phase 6
- [Versioning](/commands/versioning) — planned, Phase 7

## Full command table

<!--@include: ./_generated-table.md-->
