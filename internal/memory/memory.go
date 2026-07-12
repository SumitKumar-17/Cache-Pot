// Package memory is a Phase 4 skeleton: shared agent memory (short-term,
// long-term, episodic, and semantic memories keyed by agent/workspace). No
// implementation exists yet in Phase 1 — these types define the shape
// later phases will fill in, so Phase 1's storage/RESP layers don't need
// restructuring when Phase 4 lands.
package memory

import "time"

// Memory is a single stored memory item belonging to an agent within a
// workspace.
type Memory struct {
	ID          string
	AgentID     string
	WorkspaceID string
	Kind        Kind
	Content     string
	Embedding   []float32
	Metadata    map[string]string
	CreatedAt   time.Time
	Version     int
}
