// Package vector implements Cache-Pot's native vector store: a flat
// (brute-force) index of embeddings, partitioned by namespace, backing the
// VECTOR.UPSERT/VECTOR.SEARCH/VECTOR.DELETE RESP commands (see
// internal/server/resp/handlers_vector.go). It also declares Index, a
// generic single-collection seam a future ANN implementation could satisfy;
// Store (in store.go) is today's flat, namespace-partitioned implementation
// and predates any per-namespace Index split -- see store.go's doc comment.
package vector

import "context"

// DistanceMetric selects how similarity is measured between vectors.
type DistanceMetric int

const (
	Cosine DistanceMetric = iota
	Dot
	Euclidean
)

// Index is the vector-index seam: upsert an embedding under an ID,
// search for nearest neighbors, and delete by ID. It describes a single
// collection; Store is the concrete, namespace-partitioned flat
// implementation actually wired up to VECTOR.* today (see store.go). A
// future ANN backend could implement Index per namespace without changing
// this interface.
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
