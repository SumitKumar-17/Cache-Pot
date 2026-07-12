// Package memory implements Phase 4's shared agent-memory domain layer:
// short-term, long-term, episodic, and semantic memories keyed by
// agent/workspace, searchable by embedding similarity across every agent in
// a workspace (or scoped to one agent). It backs the MEMORY.PUT/MEMORY.GET/
// MEMORY.SEARCH RESP commands; see
// internal/server/resp/handlers_memory.go.
//
// Versioning scope (deliberate Phase 4 simplification, mirroring how
// internal/semantic and internal/toolcache document their own
// simplifications): each Put to an existing (workspace, id) bumps Version
// and replaces the stored record's content/embedding/metadata in place. No
// version history log is kept -- Store.History always reports
// ErrHistoryNotImplemented. Full "what did the agent know yesterday"
// version-history retrieval is explicitly Phase 7 scope (see
// api/commands.yaml's MEMORY.HISTORY entry, phase: 7, status: planned).
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

	// ExpiresAt is the absolute time this memory should stop being
	// returned by Get/Search, or nil for "never expires". Expiry is lazy
	// (checked on read, entry evicted when encountered), the same
	// convention internal/semantic and internal/toolcache use for their
	// own TTLs -- see Store's doc comment in store.go.
	ExpiresAt *time.Time
}

// expired reports whether m should be treated as gone as of now.
func (m *Memory) expired(now time.Time) bool {
	return m.ExpiresAt != nil && !m.ExpiresAt.After(now)
}
