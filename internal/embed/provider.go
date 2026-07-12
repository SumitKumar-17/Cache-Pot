// Package embed is a Phase 2 skeleton: a pluggable text-embedding provider
// seam used by internal/semantic (and later internal/vector /
// internal/memory) to turn text into vectors. No implementation exists yet
// in Phase 1.
package embed

import "context"

// Provider embeds text into a vector. Concrete implementations (a local
// model, an external API, etc.) arrive in Phase 2.
type Provider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}
