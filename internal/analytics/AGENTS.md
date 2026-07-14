# internal/analytics

## Role

Dependency-free, in-memory cost/savings tracker (`Tracker`) backing the operator-facing
`/stats` and `/dashboard` endpoints (see `internal/observability`). It answers two
questions: "what has this process spent on embeddings/completions" and "how much money
has caching saved." Shipped in v0.5.0 alongside `internal/observability` and eviction
hardening. Deliberately kept separate from `observability.Metrics`: `Metrics` owns
hit/miss counting and hit-rate math; `Tracker` owns cost, token usage, and money saved.
Neither duplicates the other.

## Key types and contracts

- `Tracker` (in `tracker.go`) — safe for concurrent use (single `sync.Mutex`), holds only
  running totals plus a bounded top-N list. Not a time-series store or metrics warehouse
  — a point-in-time dashboard data source, nothing more.
- `RecordEmbeddingUsage(model string, tokens int)` / `RecordCompletionUsage(model string, tokens int)`
  — accumulate per-model token counts into **two separate maps** (`byModel` vs
  `completionByModel`), never merged, so embedding and completion cost can never be
  accidentally folded into one undifferentiated number. `Snapshot` exposes them under
  distinct field names (`EmbeddingByModel` / `CompletionByModel`).
- `RecordCacheHitSavings(cacheType, prompt string, cost float64)` — the "money saved"
  entry point. Scoped deliberately to `CACHE.SEMANTIC`/`CACHE.PROMPT` only —
  `TOOL.CACHE`'s cost model is different/unknowable and out of scope here.
- **Cost is only ever recorded from an explicit, caller-reported number, never a
  fabricated estimate.** `tokens <= 0` and `cost <= 0` are no-ops by design (nothing real
  to record) — don't "fix" these into clamping or defaulting behavior.
- Unknown models are still token-counted but `PricingKnown` stays `false` and `CostUSD`
  stays `0` — the pricing tables (`pricePerMillionTokensUSD`,
  `completionPricePerMillionTokensUSD`) are small, hand-maintained, and will drift from
  OpenAI's real published pricing over time; never treat a `CostUSD` figure as an
  authoritative bill. If you add a new supported model, add it to the relevant table —
  don't add fallback/guessed pricing for unrecognized models.
- Completion cost uses a single **blended** rate per model (not separate
  input/output rates) because `RecordCompletionUsage` only receives a single
  `TokenUsage.TotalTokens` figure from `internal/llm`. Splitting input/output would
  require widening `llm.CompletionProvider`'s return shape — don't do this without a real
  need.
- `normalizeModelName` accepts either a raw model name or a `Provider.Name()`-shaped
  `"provider:model"` string and strips the prefix — both `RecordEmbeddingUsage` and
  `RecordCompletionUsage` rely on this so callers never need to know which form they have.
- `maxTopEntries = 20` bounds the "most expensive cached prompts" list; when full, only
  the current cheapest surviving entry is displaced, and only by something more
  expensive. Entries are unique by `(CacheType, Prompt)` — a repeat hit increments `Hits`
  in place rather than duplicating a row.
- `Snapshot()` returns a fully independent copy (maps and slice are copied, not aliased)
  — mutating a returned `Snapshot` never affects `Tracker`'s internal state. Preserve this
  if you touch `Snapshot`.

## How it's wired in

`server.go` constructs exactly one `analytics.New()` and threads the same `*Tracker`
pointer into `resp.Deps.Analytics` (consumed directly by `CACHE.SEMANTIC`/`CACHE.PROMPT`
GET handlers in `internal/server/resp/handlers_semantic.go` — `RecordCacheHitSavings` is
called on a cache **hit**, using the cost that was recorded at `SET` time via the
optional `COST <dollars>` argument) and into
`observability.InstrumentProvider`/`InstrumentCompletionProvider`, which wrap the real
embed/completion providers to call `RecordEmbeddingUsage`/`RecordCompletionUsage` after
every real call. `internal/observability/http.go` reads `Tracker.Snapshot()` to build both
`/stats`'s JSON and `/dashboard`'s HTML. If you ever see code constructing a second
`analytics.New()` instead of reusing this one shared instance, that's a bug — same rule as
every other shared domain instance in this repo (root AGENTS.md).

## Testing

```
go test ./internal/analytics/...
```
`-race` matters and is worth running explicitly — `TestTrackerConcurrentUse` in
`tracker_test.go` fires 200 goroutines at a shared `Tracker` and checks totals, which is
exactly the kind of test `-race` is for:
```
go test ./internal/analytics/... -race
```
No mocks/test doubles — `Tracker` has no dependencies to fake.

## Limitations (specific to this package)

- Pricing tables are hand-maintained and will go stale as OpenAI changes prices; treat
  `CostUSD` as an estimate, not a bill (documented in-code, restated here so a bug-fixer
  doesn't "fix" a stale price into something that looks more precise than it is).
- Completion cost is a blended input+output rate, not exact — see above.
- All state is in-memory only, lost on restart, consistent with the repo-wide
  no-persistence gap in the root AGENTS.md.
