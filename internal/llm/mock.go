package llm

import "context"

// mockEchoLimit caps how much of the user prompt the mock provider echoes
// back, so a huge input prompt doesn't produce a huge "completion".
const mockEchoLimit = 200

// mockPrefix marks every mock completion unmistakably as not a real
// generation, so it can never be confused with real model output in logs,
// dashboards, or (if a caller forgets to check) a downstream feature.
const mockPrefix = "[mock completion, no real generation] "

// mockProvider is a deterministic, dependency-free CompletionProvider for
// tests and offline/local development that don't have (or want) an API
// key.
//
// IMPORTANT: this performs NO real language understanding or generation.
// It has no idea whether it is being asked to summarize a conversation,
// extract entities and relationships, answer a question, or anything
// else -- it is entirely task-agnostic and simply echoes back a truncated,
// clearly-marked slice of the user prompt it was given. Callers built on
// top of CompletionProvider (the consolidation/summarization code in
// internal/consolidate and the knowledge-graph extraction code in
// internal/graph) MUST be written to degrade gracefully when wired to this
// mock: a structured-JSON-expecting caller must treat a non-JSON mock
// response as "nothing extracted, parse failed" and move on, never panic or
// treat the mock's echoed text as a real answer.
type mockProvider struct{}

// NewMock returns a deterministic, dependency-free mock CompletionProvider.
// Same input always yields the same output; it never fabricates token
// usage (TokenUsage is always the zero value, since it makes no real API
// call and so has no real cost to report), matching embed.NewMock's
// "no fabricated usage" precedent.
func NewMock() CompletionProvider {
	return &mockProvider{}
}

func (m *mockProvider) Name() string { return "mock" }

// Complete returns a short, clearly-marked deterministic string derived
// from userPrompt (systemPrompt is accepted but ignored -- the mock has no
// notion of "following instructions"). It reports a zero TokenUsage: no
// real API call was made, so there is nothing real to bill.
func (m *mockProvider) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, TokenUsage, error) {
	if err := ctx.Err(); err != nil {
		return "", TokenUsage{}, err
	}
	return mockPrefix + truncate(userPrompt, mockEchoLimit), TokenUsage{}, nil
}

// truncate returns s, or its first n runes plus a "..." marker if s is
// longer than n. Operates on runes rather than bytes so multi-byte UTF-8
// input isn't sliced mid-rune.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
