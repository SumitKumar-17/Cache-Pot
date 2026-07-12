// Package vector is a Phase 3 skeleton: a vector index seam (Upsert/Search/
// Delete) for semantic/similarity search over embeddings. No implementation
// exists yet in Phase 1.
package vector

import "context"

// DistanceMetric selects how similarity is measured between vectors.
type DistanceMetric int

const (
	Cosine DistanceMetric = iota
	Dot
	Euclidean
)

// Index is the Phase 3 vector-index seam: upsert an embedding under an ID,
// search for nearest neighbors, and delete by ID. Not implemented in
// Phase 1.
type Index interface {
	Upsert(ctx context.Context, id string, vector []float32, metadata map[string]string) error
	Search(ctx context.Context, vector []float32, k int, metric DistanceMetric) ([]Match, error)
	Delete(ctx context.Context, id string) error
}

// Match is a single nearest-neighbor search result.
type Match struct {
	ID       string
	Score    float64
	Metadata map[string]string
}
