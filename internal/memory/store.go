package memory

import "context"

// MemoryStore is the Phase 4 seam for shared agent memory: put/get a
// memory, search over memories (likely vector + metadata filtered, once
// internal/vector exists), and fetch a memory's version history (Phase 7).
// Not implemented in Phase 1.
type MemoryStore interface {
	Put(ctx context.Context, m Memory) error
	Get(ctx context.Context, workspaceID, id string) (Memory, bool, error)
	Search(ctx context.Context, workspaceID string, query string, limit int) ([]Memory, error)
	History(ctx context.Context, workspaceID, id string) ([]Memory, error)
}
