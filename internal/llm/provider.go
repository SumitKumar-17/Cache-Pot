// Package llm provides a pluggable text-*generation* provider abstraction --
// chat-style completions, as opposed to internal/embed's embeddings-only
// Provider. This is Cache-Pot's first text-generation capability: prior
// phases only ever turned text into vectors (internal/embed), never
// generated new text from a prompt.
//
// The CompletionProvider interface is intentionally small and mirrors
// internal/embed.Provider's shape: implementations range from a
// dependency-free deterministic mock (see mock.go, useful for tests and
// offline/local dev -- explicitly NOT a real completion) to a real
// HTTP-backed provider such as OpenAI (see openai.go). Callers should depend
// only on the CompletionProvider interface, never on a concrete
// implementation type, so the backing chat model can be swapped without
// touching call sites.
//
// internal/llm deliberately has no dependency on internal/embed (or vice
// versa) -- the two packages are siblings, each owning one side of the
// "text in" (embeddings) / "text out" (completions) boundary.
package llm

import "context"

// TokenUsage reports how many tokens a completion call consumed, when the
// underlying provider can report it. Mirrors embed.TokenUsage's shape, but
// is independently defined -- internal/llm imports nothing from
// internal/embed.
type TokenUsage struct {
	TotalTokens int
}

// CompletionProvider generates text from a system/user prompt pair --
// chat-style completion, not embedding. Implementations must be safe for
// concurrent use by multiple goroutines.
type CompletionProvider interface {
	// Complete generates a completion for the given system/user prompt
	// pair, returning the generated text, whatever token usage the
	// provider can report (zero value if unknown/unavailable), and an
	// error if the call failed.
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, TokenUsage, error)

	// Name identifies the provider (e.g. "mock", "openai:gpt-4o-mini"),
	// useful for logging/metrics and for cost tracking by the model that
	// produced a completion.
	Name() string
}
