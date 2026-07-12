// Package graph is a Phase 6b skeleton: a knowledge graph over stored
// memories/entities (GRAPH.RELATED and friends), built on top of the
// Phase 4 memory store. No implementation exists yet in Phase 1.
package graph

// Node is a graph vertex, typically corresponding to a memory or an
// extracted entity.
type Node struct {
	ID       string
	Label    string
	Metadata map[string]string
}

// Edge is a directed, labeled relationship between two nodes.
type Edge struct {
	FromID string
	ToID   string
	Label  string
	Weight float64
}
