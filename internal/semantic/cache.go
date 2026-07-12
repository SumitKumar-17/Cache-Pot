// Package semantic is a Phase 2 skeleton: a semantic cache that answers
// "have we seen a sufficiently similar prompt before" using embeddings +
// vector search, rather than exact-key lookup. No implementation exists yet
// in Phase 1.
package semantic

import "context"

// SemanticCache is the Phase 2 seam for CACHE.SEMANTIC / CACHE.PROMPT style
// commands. Not implemented in Phase 1.
type SemanticCache interface {
	Lookup(ctx context.Context, workspace, prompt string, model string, temperature float64) (response string, hit bool, err error)
	Store(ctx context.Context, workspace, prompt, response string, model string, temperature float64) error
}
