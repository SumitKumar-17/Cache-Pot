# AGENTS.md — internal/llm

Read the root `AGENTS.md` first; this file only covers what's specific to this package.

## Role

`internal/llm` is the `CompletionProvider` abstraction: chat-style text *generation*
(system+user prompt in, generated text out) — as opposed to `internal/embed`'s
embeddings. It's Cache-Pot's first text-generation capability, shipped in v0.6.0; before
it, every provider in the codebase only ever turned text into vectors. It backs
`internal/consolidate` (`SUMMARY.CREATE` summarization) and `internal/graph`
(`GRAPH.EXTRACT` entity/relationship extraction).

## Key types and invariants

- `CompletionProvider` (`provider.go`): `Complete(ctx, systemPrompt, userPrompt) (string,
  TokenUsage, error)` and `Name() string`. That's the whole interface — deliberately
  small, mirrors `embed.Provider`'s shape. Implementations must be concurrency-safe.
  Unlike `embed`, there's no separate "optional usage" capability interface here —
  `TokenUsage` is returned directly from `Complete` every time (zero value when unknown).
- **`internal/llm` imports nothing from `internal/embed`, and vice versa.** They are
  deliberately independent sibling packages, each owning one side of the "text in"
  (embeddings) / "text out" (completions) boundary — see `provider.go`'s package doc.
  Don't add a cross-import or try to merge them into one "AI provider" interface; that
  would break the "embeddings/completions are two orthogonal capabilities" model the
  whole downstream (`analytics`, `observability`) is built on.
- `NewMock()` (`mock.go`): deterministic, dependency-free, zero network calls, zero
  fabricated `TokenUsage` (always the zero value — no real call was made, so nothing
  real to bill). It is **task-agnostic** — it has no idea if it's being asked to
  summarize, extract JSON, or answer a question. It always returns
  `"[mock completion, no real generation] " + truncate(userPrompt, 200)`, ignoring
  `systemPrompt` entirely. This means it **never produces valid JSON** even when a
  caller's system prompt demands strict JSON output.
  - This is the load-bearing case the root `AGENTS.md`'s "golden rule" section cites by
    name: any caller built on top of `CompletionProvider` (`internal/consolidate`,
    `internal/graph`) **must** treat a non-JSON/unparseable mock response as "nothing
    extracted, parse failed" and degrade gracefully — never panic, never treat the
    echoed text as a real answer. `internal/graph.Extract` against the mock is verified
    (not just asserted) to return zero entities and zero relations; see
    `internal/graph/extract_test.go`.
- `NewOpenAI(apiKey, model, baseURL)` (`openai.go`): real HTTP calls to OpenAI's
  `/v1/chat/completions`, stdlib-only. Default model `gpt-4o-mini`. Same
  empty-string-defaults-to-OpenAI `baseURL` convention as `embed.NewOpenAI`. Errors out
  explicitly if the response has zero `choices` (`TestOpenAICompleteNoChoicesError`) —
  never returns an empty string silently in that case.

## Package-specific gotchas

- `mockEchoLimit` (200 runes) and `mockPrefix` are constants specifically designed so a
  mock completion can never be mistaken for real model output in logs/dashboards even if
  a caller forgets to check. If you touch the mock, keep some unmistakable marker string
  in its output — don't make it "cleaner" by dropping the prefix.
- `truncate` operates on runes, not bytes, specifically to avoid slicing multi-byte UTF-8
  input mid-rune. Keep that if you touch it.
- There is no `EmbedBatch`-equivalent batching here — `Complete` is always one
  system/user pair per call, matching how a chat-completions API actually works (no
  native multi-prompt batching endpoint on OpenAI's side either).

## Testing

```bash
go test ./internal/llm/...
```

No `-race` requirement beyond the repo-wide sweep — stateless providers, no shared
mutable state. `openai_test.go` uses `httptest.NewServer` exclusively; no real network
access or API key needed to run this package's tests.

## Honest limitations

- The mock provider does no real language understanding whatsoever — not "limited
  understanding," literally none. It cannot summarize, cannot extract structured data,
  cannot follow the system prompt at all. This is the documented, permanent behavior of
  `--completion-provider mock` (the default), not a stub awaiting improvement. Real
  extraction/summarization quality requires `--completion-provider openai`.
