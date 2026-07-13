// Package memory implements Phase 4's shared agent-memory domain layer:
// short-term, long-term, episodic, and semantic memories keyed by
// agent/workspace, searchable by embedding similarity across every agent in
// a workspace (or scoped to one agent). It backs the MEMORY.PUT/MEMORY.GET/
// MEMORY.SEARCH RESP commands; see
// internal/server/resp/handlers_memory.go.
//
// Versioning: each Put to an existing (workspace, id) bumps Version and
// replaces the stored record's content/embedding/metadata in place as the
// current/latest version. Phase 7 additionally keeps a bounded log of every
// version a Put makes obsolete (see Store's doc comment on its history
// field and maxMemoryHistoryPerRecord), so Store.History can answer "what
// did the agent know at each point in time" for a given id -- see
// internal/server/resp/handlers_memory.go's MEMORY.HISTORY command.
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
