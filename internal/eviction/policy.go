// Package eviction defines eviction-scoring policies used to decide which
// keys to reclaim under memory pressure. Phase 1 wires up entry-level
// last-access tracking (internal/storage/memstore.Entry.LastAccess) and a
// default LRU policy; an actual maxmemory-triggered evictor that calls into
// this package is out of Phase 1's scope (no memory-limit config exists
// yet) but the scoring seam is in place so later phases can add one without
// restructuring storage.
package eviction

import "time"

// Policy scores an entry for eviction eligibility. Higher scores are
// evicted first. Implementations deliberately take primitive values (not a
// concrete storage.Entry) to avoid a dependency between internal/eviction
// and internal/storage/memstore.
type Policy interface {
	// Score returns the eviction score for an entry, given its last access
	// time and the current time.
	Score(lastAccess time.Time, now time.Time) float64
}
