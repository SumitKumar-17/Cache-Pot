# internal/observability

Process-wide instrumentation: atomic counters (`Metrics`), two decorator wrappers that
instrument `embed.Provider` and `llm.CompletionProvider`, a slog constructor, and the
`/metrics` (Prometheus text), `/stats` (JSON), `/dashboard` (HTML) HTTP handlers served
on the MCP port. Every other domain package (resp handlers, `internal/mcp`,
`internal/storage/memstore`) reports into the single `*observability.Metrics` instance
built once in `internal/server/server.go`; this package never reads from those packages,
only gets called into. Shipped in v0.5.0.

## Key types/contracts

- **`Metrics`** (`metrics.go`): all fields are `atomic.Int64`/mutex-guarded maps, safe
  for concurrent use from any goroutine (RESP connection handlers, MCP tool handlers,
  the TTL reaper). One process-wide instance, constructed via `NewMetrics()`,
  threaded everywhere via `resp.Deps.Metrics` and `mcp.New(...)` — same "one shared
  instance, no adapters" discipline as the domain stores. Never construct a second
  `Metrics` for a subsystem.
- **`Metrics.Snapshot()`** is the only way to read current values — it copies the
  `mcpCallsByTool` map and `latency` map under their mutexes, so a `Snapshot` is safe to
  hold and range over without further locking. `MetricsHandler`/`StatsHandler`/
  `DashboardHandler` all build their output purely from one `Snapshot()` call, never
  touching `Metrics` fields directly.
- **`latencyAccumulator`** is deliberately count/sum/max only, not a histogram — this
  project's stated scope doesn't need bucketed histograms (see comment in
  `metrics.go`). Don't add histogram buckets without discussing the tradeoff; it mirrors
  the "bounded sampling over full scans" philosophy in the root AGENTS.md.
- **`InstrumentProvider(inner embed.Provider, m *Metrics, tracker *analytics.Tracker) embed.Provider`**
  and **`InstrumentCompletionProvider(inner llm.CompletionProvider, m *Metrics, tracker *analytics.Tracker) llm.CompletionProvider`**:
  decorators wrapping the real embed/completion provider exactly once in
  `server.go`, before it's handed to any consumer (`internal/semantic`,
  `internal/memory`, `internal/consolidate`, `internal/graph`). This is why none of
  those packages call into `observability` themselves — one wrap covers every current
  and future caller. If you add a new embedding/completion consumer, it gets
  instrumentation for free as long as it's handed the already-wrapped provider from
  `server.go` — never wrap a provider a second time.
- **Optional-capability forwarding is load-bearing, not incidental.** `InstrumentProvider`
  type-asserts `inner` against `embed.UsageEmbedder` (only the real OpenAI provider
  implements it, not `embed.NewMock`) and returns a different wrapper type
  (`instrumentedUsageProvider`) only if the assertion succeeds — Go doesn't forward
  optional interfaces through a wrapper automatically, hence the embedding trick in
  `instrumentedUsageProvider` (embeds `*instrumentedProvider` for promoted methods,
  implements `EmbedBatchWithUsage` explicitly). If you add a new optional
  provider capability, follow this exact pattern rather than making `UsageEmbedder`
  mandatory on the base interface.
- **`tracker *analytics.Tracker` may always be nil** in both instrument functions and in
  `StatsHandler`/`DashboardHandler` — every call site nil-checks before touching it
  (`analyticsSnapshot` helper). Don't remove those nil checks; tests rely on passing
  `nil` when analytics isn't under test.
- **`Metrics.KeyEvicted` is a plain no-arg callback**, passed to
  `memstore.WithOnEvict(s.metrics.KeyEvicted)` in `server.go` — `internal/storage/memstore`
  never imports this package. Keep new storage-side instrumentation hooks shaped this
  way (callback in, no reverse dependency) rather than having memstore import
  observability directly.
- **Zero counts are honest, not bugs.** `EntitiesExtracted(0)`/`RelationsExtracted(0)`
  reflect the mock `CompletionProvider`'s real "no entities extracted" behavior (see
  `internal/graph`); `MemoriesDeduped(0)` reflects a consolidation pass that found no
  near-duplicates. Don't treat a zero snapshot value as a wiring bug without checking
  which provider is configured.

## Conventions/gotchas specific to this package

- No metrics client library, no Prometheus SDK — `MetricsHandler` hand-rolls the
  Prometheus text exposition format directly in `http.go` (matches this project's
  stdlib-only precedent, e.g. the OpenAI embeddings provider). If you add a counter,
  add both a `Metrics` field/method AND a corresponding `# HELP`/`# TYPE`/value line in
  `MetricsHandler`, AND a JSON field in `statsResponse`/`buildDashboardView` if it
  belongs on `/stats`/`/dashboard` too — there's no reflection-based auto-export.
  `metrics_test.go`'s `TestMetricsHandlerRendersPrometheusText`/`TestStatsHandlerRendersJSON`
  assert on literal substrings, so a renamed metric needs the test updated too.
  `metrics_test.go` line count and `http.go` line count will make an omission obvious.
- `DashboardHandler` deliberately does **not** show a "tokens avoided" figure — a
  `CACHE.SEMANTIC` hit still re-embeds the query prompt to compute similarity, so no
  embedding tokens are actually avoided by a hit. Showing a fabricated avoided-tokens
  number would violate the project's honesty policy; the dashboard only shows tokens
  *consumed* and dollars *saved* (from caller-reported `COST`). Don't add a token-savings
  stat without first checking whether it's actually measured anywhere.
- `dashboardHTML` is a single `html/template` string constant with package-level
  template funcs (`avgMillis`, `maxMillis`, `mulf`) registered via `template.FuncMap` —
  `html/template` funcs can't be methods on a type defined elsewhere, hence the
  free functions. No external CSS/JS.
- `NewLogger` is the *only* sanctioned way to construct a `slog.Logger` in this
  codebase — JSON handler on stdout, level configurable. Don't call
  `slog.New(slog.NewJSONHandler(...))` ad hoc elsewhere.

## Testing

```bash
go test ./internal/observability/...
```

`-race` is worth running here specifically (`TestConcurrentCounters` spawns 200
goroutines hammering the same `Metrics`) — it's part of the standard root
`go test ./... -race` but this package is exactly the kind of concurrent-counter code
that benefits from being raced directly:

```bash
go test ./internal/observability/... -race
```

Test doubles: `fakeProvider`/`fakeUsageProvider` (minimal `embed.Provider`/
`embed.UsageEmbedder` doubles) and `fakeCompletionProvider` (`llm.CompletionProvider`
double) — all hand-written in the `_test.go` files, no mocking library, no dependency on
`internal/embed`'s or `internal/llm`'s real `mock.go`. HTTP handlers are tested via
`net/http/httptest` directly against `MetricsHandler`/`StatsHandler`, asserting on
literal output substrings.

## Limitations

- No histograms/percentiles — only count/avg/max per command family. If you need p99
  latency, this package doesn't have it; that's a real gap, not an oversight (see the
  `latencyAccumulator` comment).
- `/dashboard` re-renders from a fresh `Snapshot()` on every request with no caching or
  auto-refresh — it's an operator/debug view, not a monitoring product (documented
  in its own doc comment).
