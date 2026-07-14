// Package embed provides a pluggable text-embedding provider abstraction
// used by internal/semantic (the semantic cache) and internal/vector (the
// vector store) to turn text into vectors.
//
// The Provider interface is intentionally small: implementations range from
// a dependency-free deterministic mock (see mock.go, useful for tests and
// offline/local dev) to real HTTP-backed providers such as OpenAI (see
// openai.go). Callers should depend only on the Provider interface, never
// on a concrete implementation type, so the backing embedding model can be
// swapped without touching call sites.
package embed

import "context"

// Provider turns text into a fixed-length vector embedding.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type Provider interface {
	// Embed returns the embedding vector for a single piece of text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns the embedding vectors for multiple texts, in the
	// same order as the input slice. Implementations that can't batch
	// natively may simply call Embed in a loop.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions reports the length of the vectors this provider produces.
	Dimensions() int

	// Name identifies the provider (e.g. "mock", "openai:text-embedding-3-small"),
	// useful for logging/metrics and for namespacing cached vectors by the
	// model that produced them.
	Name() string
}

// TokenUsage reports how many tokens an embedding call consumed, when the
// underlying provider can report it.
type TokenUsage struct {
	TotalTokens int
}

// UsageEmbedder is an optional Provider capability: a provider may
// implement it to report token usage for a batch embed call, returned
// alongside the vectors rather than via a stateful side channel (which
// would be racy under concurrent use). The deterministic mock provider
// does NOT implement this -- it makes no real API call, so it has no real
// notion of token cost; callers must type-assert and treat its absence as
// "usage unknown," never fabricate a number.
type UsageEmbedder interface {
	EmbedBatchWithUsage(ctx context.Context, texts []string) ([][]float32, TokenUsage, error)
}
