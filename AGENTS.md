# AGENTS.md

Guidance for AI agents (and humans) making changes to Cache-Pot. Read this before
touching code — it captures conventions that aren't obvious from any single file, and
following them keeps the codebase consistent as it keeps growing.

## What this is

Cache-Pot is **a memory engine for AI agents**, not "Redis with vectors bolted on."
It's a single server that speaks the Redis protocol (RESP2) — so any existing Redis
client works against it unmodified — and grows into semantic caching, native vector
search, shared agent memory, cost analytics, and a knowledge graph, all sharing one
in-memory state. See [README.md](README.md) for the full pitch and
[ROADMAP.md](ROADMAP.md) for the phased plan. **Phases 1-6 of 7 are done and real** as
of this writing — check `git log` and `ROADMAP.md` before assuming what's current, this
will go stale.

## Golden rule: never present planned work as real

This project has a strict, load-bearing honesty policy, checked repeatedly throughout
its history:
- `api/commands.yaml` is the single source of truth for the command surface. Every
  command has a `status: real` or `status: planned`. The docs site generates its
  command tables from this file specifically so they can't drift from what's actually
  implemented — never hand-edit `docs/commands/*.md` tables, only the generated
  `_generated-table.md` include.
- A "planned" feature must never be exposed as if it works. When a capability
  genuinely can't do something (e.g. the mock embedding/completion providers doing no
  real language understanding), the code must degrade gracefully and honestly — return
  an empty/zero result, not a fabricated one, and document the limitation loudly in
  both a code comment and the docs. See `internal/llm/mock.go` and
  `internal/graph/extract.go` for the canonical example: `GRAPH.EXTRACT` against the
  mock completion provider always returns `[0, 0]`, verified by a real test, not just
  asserted.
- When you add or change a command, update `api/commands.yaml` and the relevant
  `docs/commands/*.md` narrative page in the same logical unit of work.

## Repo layout

```
cmd/cachepotd/main.go        entrypoint: flags/env -> server.Config -> server.Run
internal/server/             process wiring: RESP + MCP + /metrics/stats/dashboard listeners
internal/server/resp/        RESP2 protocol, command dispatch, all handlers_*.go
internal/storage/            Engine interface (the seam) + memstore (sharded KV impl) + ttl (active expiry)
internal/auth/                single shared-password AUTH (Phase 1) — Phase 7 will make this per-workspace
internal/embed/               embeddings: Provider interface, mock + OpenAI impls
internal/llm/                 text GENERATION: CompletionProvider interface, mock + OpenAI chat impls
internal/semantic/            CACHE.SEMANTIC (similarity) + CACHE.PROMPT (exact-match), on internal/embed
internal/toolcache/           TOOL.CACHE (exact-match tool-call result cache)
internal/vector/              native flat vector index: VECTOR.UPSERT/SEARCH/DELETE
internal/memory/              agent memory: MEMORY.PUT/GET/SEARCH, built on internal/vector for ranking
internal/consolidate/         SUMMARY.CREATE: non-destructive dedup + internal/llm summarization
internal/graph/               knowledge graph: GRAPH.EXTRACT/RELATED, entity extraction via internal/llm
internal/mcp/                 native MCP server — same shared instances as resp.Deps, no adapter layer
internal/observability/       Metrics (+/metrics, /stats, /dashboard), embed/completion instrumentation
internal/analytics/           cost tracking: embedding/completion $ cost, opt-in COST-driven money-saved
internal/eviction/            Policy interface (LRU, Weighted), consumed by memstore's --max-entries bound
internal/tenancy/             Phase 7 skeleton only — not implemented yet
docs/                          VitePress site, own package.json — see docs/AGENTS.md
api/commands.yaml             source of truth for the whole command surface across all 7 phases
```

**The seam to respect**: `internal/storage.Engine` is an interface; `internal/storage/
memstore` is its only concrete implementation. RESP handlers depend on the `Engine`
interface via `resp.Deps.Engine`, never on `*memstore.Store` directly. The same
discipline applies everywhere a domain package (semantic/vector/memory/graph/
consolidate) is shared between the RESP layer and the MCP layer: both call into the
*exact same* constructed instance (built once in `internal/server/server.go`, threaded
into both `resp.Deps` and `mcp.New(...)`), never a second, disconnected instance. This
is what makes "an MCP tool call and a RESP command are two front doors onto the same
memory" true rather than aspirational — if you ever construct a second `semantic.New(...)`
or similar instead of reusing the shared one, you've silently broken that guarantee.

## Conventions that recur across this codebase

- **`workspace` is threaded through everything, first-parameter, even where only
  `"default"` is ever passed today.** This is deliberate pre-wiring for Phase 7
  multi-tenancy — don't remove it as "unused," and when you add a new store/command,
  thread a `workspace string` parameter through it the same way, even if nothing
  enforces it yet.
- **Expand the interface/constructor, update every call site.** This project's normal
  way of adding a capability to an existing type is to widen its method signature or
  constructor and fix up every caller, rather than bolting on a parallel method. Look
  at how `MemoryStore.Search`'s signature grew across Phases 4-6, or how
  `InstrumentProvider`/`mcp.New` gained parameters each phase, before choosing a
  different pattern.
- **Redis-shaped errors matter.** Real Redis clients pattern-match on error string
  prefixes (`WRONGTYPE`, `ERR wrong number of arguments for 'x' command`). Reuse the
  helpers in `internal/server/resp/errors.go` — don't invent a new error string shape
  for a new command.
- **Command registration**: one `handlers_<family>.go` file per command family, a
  `Register<Family>(r *Registry)` function, called from `registry_all.go`'s
  `RegisterAll`. Follow this exactly for new commands — see `CONTRIBUTING.md`'s
  step-by-step.
- **Mock providers are real, deterministic, dependency-free, and honestly limited.**
  `embed.NewMock`/`llm.NewMock` never make a network call, never fabricate semantic
  meaning, and are documented as such. When you build something on top of a provider,
  write the graceful-degradation path for the mock *and test it* — don't just assume
  the real provider will always be configured.
- **Bounded sampling over full scans**, for anything that has to touch "all the keys":
  the TTL reaper, the `--max-entries` evictor, and the flat vector/memory search all
  intentionally sample or brute-force-scan within a bounded scope rather than
  maintaining an always-sorted global structure. This is a considered tradeoff
  (documented at each site), not an oversight — don't "fix" it into a global exact
  structure without discussing the tradeoff first.
- **Non-destructive by default.** Dedup in `internal/consolidate` never deletes
  anything from the store; it only narrows what feeds a summarization prompt. Prefer
  additive/non-destructive designs for anything touching stored user data unless
  there's a clear, explicit, opt-in reason not to.

## Development

```bash
go build ./...
go vet ./...
gofmt -l .                 # should print nothing
golangci-lint run ./...    # requires golangci-lint v2.x — see .golangci.yml's `version: "2"`
go test ./... -race
```

Manual end-to-end check (don't rely on unit tests alone for anything touching the wire
protocol or a new listener):

```bash
go build -o bin/cachepotd ./cmd/cachepotd
./bin/cachepotd --port 6380 --mcp-port 6381
redis-cli -p 6380 PING
curl http://localhost:6381/stats
```

Docker: `docker build -f deployments/docker/Dockerfile -t cache-pot:dev .` or
`docker compose -f deployments/compose/docker-compose.yml up --build`.

**Before calling anything done**: build, vet, gofmt, lint, and the full race-enabled
test suite must all be clean, AND you should have actually driven the real behavior
over the wire at least once (raw RESP socket, `redis-cli`, or a real MCP client call) —
this project has caught real bugs this way that unit tests alone missed (e.g. a
`--help` flag that was leaking a real secret in cleartext, only visible by actually
running the binary).

## Commit style

Small, focused commits — one logical unit of work each (a new capability, a docs
update, a bug fix), not one giant commit per phase. See `git log` for the established
granularity. Explain *why* in the commit body, not just *what* — the diff already shows
what changed.

## Known, honest gaps (check before assuming something works)

- No persistence: Cache-Pot Phase 1's core KV store is volatile, in-memory only.
- MCP has no authentication layer yet — it doesn't gate by workspace/credentials the
  way RESP's `AUTH` does.
- Eviction (`--max-entries`) is approximate below the shard count (32 by default) — see
  `docs/getting-started/observability.md`'s eviction section for the verified numbers.
- `GRAPH.EXTRACT`/`SUMMARY.CREATE` quality is entirely dependent on the configured
  completion provider; the default `mock` provider does no real language understanding.
- Phase 7 (memory version history, real per-workspace isolation/auth) is the one phase
  left on the original roadmap — check `ROADMAP.md` for current status.

For deeper context on *why* a given piece of this exists, `git log --oneline` and the
individual commit messages are unusually detailed on purpose — read them before
assuming something is under-designed.
