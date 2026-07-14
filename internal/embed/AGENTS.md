# AGENTS.md — internal/embed

Read the root `AGENTS.md` first; this file only covers what's specific to this package.

## Role

`internal/embed` is the embedding-`Provider` abstraction: turns text into a fixed-length
`[]float32` vector. It backs `internal/semantic` (similarity cache), `internal/vector`
(native vector index), and `internal/memory` (agent memory ranking) — anything in the
codebase that needs "how similar is this text to that text" goes through here. Shipped
in v0.2.0, one of the two earliest domain packages alongside `internal/semantic`.

## Key types and invariants

- `Provider` (`provider.go`): `Embed`, `EmbedBatch`, `Dimensions`, `Name`. Callers must
  depend on this interface, never a concrete type — the whole point is the model can be
  swapped. Implementations must be concurrency-safe.
- `UsageEmbedder` is an *optional* capability interface (`EmbedBatchWithUsage`) a
  `Provider` may additionally implement to report real token cost. **Never fabricate a
  usage number** — if a provider can't report real usage (the mock never can), don't
  type-assert and guess; treat its absence as "unknown." `internal/analytics` consumes
  this for real cost tracking.
- `NewMock(dims)` (`mock.go`): deterministic, dependency-free, makes zero network calls.
  Same text always produces the same vector; different words produce dissimilar
  vectors (cosine < 0.5 for unrelated text); same words with different case/whitespace
  produce *close but not bit-identical* vectors (small perturbation seeded off the raw
  text) — this is deliberate, it's what lets `internal/semantic`'s similarity-threshold
  tests exercise near-duplicate matching without a real model. It does **not** implement
  `UsageEmbedder`. This is not a toy left over from early development — it's the
  documented, permanent default for offline/local dev and every test in the repo that
  needs a `Provider` without an API key. See `mock_test.go` for the behavioral contract
  (`TestMockDeterministic`, `TestMockNearDuplicatesAreCloseButNotIdentical`,
  `TestMockExactDuplicatesAreIdentical`, `TestMockRespectsCanceledContext`).
- `NewOpenAI(apiKey, model, baseURL)` (`openai.go`): real HTTP calls to OpenAI's
  `/v1/embeddings`, stdlib-only (no SDK). Maps response `index` back onto the input
  slice explicitly — never assumes response order matches request order (see
  `TestOpenAIEmbedSuccess`, which deliberately returns data out of order). Empty
  `baseURL`/`model` fall back to OpenAI defaults; a non-empty `baseURL` lets you point at
  any OpenAI-compatible endpoint (Azure, self-hosted gateway).
- `similarity.go`: `Cosine`, `Dot`, `Euclidean` — free functions, not methods, used
  directly by `internal/vector` and `internal/semantic` too. **All three return
  `math.NaN()`** on dimension mismatch, empty input, or (for Cosine) a zero-magnitude
  vector — never a fabricated 0 or panic. Callers must check `math.IsNaN` rather than
  treat NaN as "no similarity."

## Package-specific gotchas

- `internal/embed` and `internal/llm` are strictly separate abstractions with no
  cross-import in either direction — see `internal/llm/AGENTS.md`. Don't be tempted to
  unify "provider" here; the split is intentional (text-in vs text-out).
- The mock's "bag of words + raw-text perturbation" scheme is the *only* thing giving
  it near-duplicate-vs-exact-duplicate discrimination. If you change
  `deterministicEmbedding`, re-run `TestMockNearDuplicatesAreCloseButNotIdentical` and
  `TestMockExactDuplicatesAreIdentical` — both encode real product behavior other
  packages' tests depend on transitively (e.g. `semantic.TestSemanticCacheCaseWhitespaceVariantHit`).
- `openAIModelDimensions` is a hardcoded lookup table (3 known models, default 1536 for
  anything else). Adding support for a new OpenAI embedding model means adding a case
  here, not just changing the default string.

## Testing

```bash
go test ./internal/embed/...
```

No `-race` requirement beyond the repo-wide `go test ./... -race` sweep — nothing in
this package has package-level mutable state; `Provider` implementations are stateless
apart from immutable config set at construction. `openai_test.go` uses
`httptest.NewServer` exclusively — no real network access, no real API key, ever
required to run this package's tests.

## Honest limitations

- The mock provider's similarity is driven entirely by lexical token overlap, not
  meaning — documented in `deterministicEmbedding`'s doc comment. Two paraphrases with
  no shared words will not be judged similar by the mock, unlike a real embedding model.
- `openAIModelDimensions` only recognizes three specific OpenAI model strings; any other
  model name (a future OpenAI model, a third-party OpenAI-compatible model) silently
  gets the 1536 fallback, which will be wrong if that model's real dimensionality
  differs — there's no way to override `Dimensions()` independently of `model`.
