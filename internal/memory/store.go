package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/embed"
	"github.com/SumitKumar-17/cache-pot/internal/vector"
)

// maxMemoryHistoryPerRecord bounds how many prior versions Store keeps per
// (workspace, id). Without a cap, an id that's Put to very frequently (e.g.
// a running log/counter-style memory updated on every agent turn) could
// grow its version-history log without bound. Once the cap is exceeded, the
// oldest prior version is dropped, mirroring this codebase's established
// pattern of bounding anything that could otherwise grow unbounded rather
// than maintaining an always-exact, always-growing structure -- see
// internal/analytics.Tracker's maxTopEntries and the TTL reaper's
// sampleSize for the same tradeoff applied elsewhere. 100 is generous for
// History's intended use ("what did this memory look like over time") while
// keeping a single hot id's footprint bounded.
const maxMemoryHistoryPerRecord = 100

// defaultSearchK is Search's fallback result cap when SearchOptions.K is
// omitted (<= 0), matching MEMORY.SEARCH's own documented default.
const defaultSearchK = 10

// SearchOptions configures Store.Search's ranking and filtering.
type SearchOptions struct {
	// AgentID, if non-empty, scopes the search to that agent's own
	// memories. Empty means "every agent in the workspace" -- the shared-
	// memory pillar's payoff: no artificial per-agent silo by default.
	AgentID string

	// Kind, if non-nil, restricts results to that memory kind.
	Kind *Kind

	// K caps the number of results. <= 0 defaults to 10.
	K int

	// Threshold, if non-nil, is a minimum cosine-similarity score a result
	// must meet to be included. nil means no minimum: return the best K
	// matches regardless of score (see Search's doc comment for why this
	// differs from internal/semantic's hard threshold gate).
	Threshold *float64
}

// SearchResult pairs a matched Memory's id with its cosine-similarity score
// against the search query.
type SearchResult struct {
	ID    string
	Score float64
}

// ListOptions configures Store.List's filtering.
type ListOptions struct {
	// AgentID, if non-empty, restricts the result to that agent's own
	// memories. Empty means every agent in the workspace.
	AgentID string

	// Kind, if non-nil, restricts the result to that memory kind.
	Kind *Kind
}

// MemoryStore is the seam for shared agent memory: put/get a
// memory, search over memories by embedding similarity (optionally scoped
// to an agent and/or kind), and fetch a memory's version history.
type MemoryStore interface {
	// Put stores m, embedding m.Content via the configured embed.Provider.
	// Only m.ID (optional), m.AgentID, m.WorkspaceID, m.Kind, m.Content, and
	// m.Metadata are read from the input; m.Embedding, m.CreatedAt, and
	// m.Version are always computed by Put, not taken from the caller.
	// If m.ID is empty, a new id is generated and returned. If m.ID names
	// an existing memory in m.WorkspaceID, that memory's Version is
	// bumped and its content/embedding/metadata are replaced in place (see
	// this package's doc comment on versioning scope). ttl <= 0 means the
	// memory never expires.
	Put(ctx context.Context, m Memory, ttl time.Duration) (id string, err error)

	// Get looks up a memory by (workspaceID, id), reporting found=false if
	// it doesn't exist or has expired (lazily evicting it in that case).
	Get(ctx context.Context, workspaceID, id string) (Memory, bool, error)

	// Search embeds query and ranks stored memories in workspaceID by
	// cosine similarity, applying opts's agent/kind/threshold filters and
	// K cap. Results are sorted best-match-first. An empty/no-match result
	// returns a nil slice, not an error.
	Search(ctx context.Context, workspaceID, query string, opts SearchOptions) ([]SearchResult, error)

	// History returns every version of (workspaceID, id), oldest first,
	// ending with the current/latest version -- the full lineage in
	// chronological order. Returns (nil, nil), not an error, if id doesn't
	// exist in workspaceID or its current version has already expired
	// (same lazy-expiry rule Get uses): an unknown/expired id is not a
	// caller mistake, the same convention this package already uses for
	// Get's found=false and Search/List's nil-slice "nothing to report"
	// results. See Store's doc comment for how many prior versions are
	// actually kept.
	History(ctx context.Context, workspaceID, id string) ([]Memory, error)

	// List returns every memory in workspaceID matching opts's AgentID/Kind
	// filters, as full Memory records including each one's stored
	// Embedding. Unlike Search, this needs no query string to embed and
	// rank against -- it's the entry point consolidation
	// (internal/consolidate) uses to gather every memory matching
	// (workspace, agent, kind) so it can compare their embeddings directly
	// for deduplication, rather than re-embedding everything or going
	// through Search's query-based ranking path. Order is unspecified;
	// callers that need a particular order (e.g. by CreatedAt) must sort
	// the result themselves. An empty/no-match result returns a nil slice,
	// not an error.
	List(ctx context.Context, workspaceID string, opts ListOptions) ([]Memory, error)
}

// Store is the concrete, in-memory MemoryStore implementation. It embeds
// content via a shared embed.Provider (the same provider construction
// pattern as internal/semantic.New) and reuses internal/vector.Store
// directly as its embedding-similarity search index (namespace = workspace)
// rather than reimplementing brute-force cosine search -- see
// internal/vector/store.go's own doc comment for why that's the right
// layer for the ranking part of Search.
//
// The vector store is purely a search index, not the source of truth: full
// Memory records (content, agent id, kind, metadata, timestamps) are held
// separately in a workspace -> id -> *Memory map, the same shape
// internal/semantic and internal/toolcache use for their own entries.
//
// history holds every version of a record made obsolete by a later Put,
// separately from records (the current/latest version), so Get/Search/List's
// existing performance and shape are untouched by version history -- it's
// purely additive. Keyed the same way as records (workspaceID -> id), with
// each id's slice oldest-first and bounded to maxMemoryHistoryPerRecord
// prior versions (see that const's doc comment).
//
// Store is safe for concurrent use: a single RWMutex guards the records and
// history maps. (internal/vector.Store guards its own state independently.)
type Store struct {
	provider embed.Provider
	vecStore *vector.Store

	mu      sync.RWMutex
	records map[string]map[string]*Memory  // workspaceID -> id -> current record
	history map[string]map[string][]Memory // workspaceID -> id -> prior versions, oldest first

	// now is overridable in tests so TTL-expiry tests don't need real
	// sleeps; production code always uses time.Now.
	now func() time.Time
}

// New builds an empty Store that uses provider to embed memory content and
// search queries.
func New(provider embed.Provider) *Store {
	return &Store{
		provider: provider,
		vecStore: vector.New(),
		records:  make(map[string]map[string]*Memory),
		history:  make(map[string]map[string][]Memory),
		now:      time.Now,
	}
}

// newID generates a random, unique-enough memory id: 16 bytes of
// crypto/rand, hex-encoded. crypto/rand is used (rather than deriving the
// id from time.Now) purely so id generation itself needs no wall-clock
// mocking in tests -- it has nothing to do with cryptographic use.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("memory: generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Put implements MemoryStore.Put.
func (s *Store) Put(ctx context.Context, m Memory, ttl time.Duration) (string, error) {
	if m.WorkspaceID == "" {
		return "", errors.New("memory: workspace is required")
	}

	vec, err := s.provider.Embed(ctx, m.Content)
	if err != nil {
		return "", err
	}

	id := m.ID
	if id == "" {
		id, err = newID()
		if err != nil {
			return "", err
		}
	}

	now := s.now()
	var expiresAt *time.Time
	if ttl > 0 {
		t := now.Add(ttl)
		expiresAt = &t
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ws, ok := s.records[m.WorkspaceID]
	if !ok {
		ws = make(map[string]*Memory)
		s.records[m.WorkspaceID] = ws
	}

	// CreatedAt reflects the memory's original creation time, preserved
	// across version bumps (it's "created_at", not "updated_at" -- there's
	// no updated_at field on Memory). Version starts at 1 and increments
	// by one on every Put to the same id.
	version := 1
	createdAt := now
	if existing, ok := ws[id]; ok {
		version = existing.Version + 1
		createdAt = existing.CreatedAt
		// Capture the pre-overwrite record into history before it's
		// replaced below. *existing is a value copy of the struct being
		// made obsolete; its Embedding/Metadata are never mutated in
		// place by a later Put (each Put builds an entirely new *Memory
		// rather than mutating the existing one), so this copy stays a
		// faithful, un-aliased snapshot of what this id looked like at
		// that version.
		s.recordHistoryLocked(m.WorkspaceID, id, *existing)
	}

	rec := &Memory{
		ID:          id,
		AgentID:     m.AgentID,
		WorkspaceID: m.WorkspaceID,
		Kind:        m.Kind,
		Content:     m.Content,
		Embedding:   vec,
		Metadata:    m.Metadata,
		CreatedAt:   createdAt,
		Version:     version,
		ExpiresAt:   expiresAt,
	}
	ws[id] = rec

	// agent_id/kind are stored as vector-store metadata purely so Search
	// can push its AGENT/KIND filters down into vector.Store's own FILTER
	// mechanism, rather than post-filtering every candidate in Go.
	s.vecStore.Upsert(m.WorkspaceID, id, vec, map[string]string{
		"agent_id": m.AgentID,
		"kind":     m.Kind.String(),
	}, "")

	return id, nil
}

// recordHistoryLocked appends prior (the record a Put is about to overwrite)
// to id's history log in workspaceID, oldest-first, dropping the oldest
// entry once maxMemoryHistoryPerRecord is exceeded. Callers must hold s.mu.
func (s *Store) recordHistoryLocked(workspaceID, id string, prior Memory) {
	ws, ok := s.history[workspaceID]
	if !ok {
		ws = make(map[string][]Memory)
		s.history[workspaceID] = ws
	}
	h := append(ws[id], prior)
	if len(h) > maxMemoryHistoryPerRecord {
		h = h[len(h)-maxMemoryHistoryPerRecord:]
	}
	ws[id] = h
}

// deleteHistoryLocked drops id's entire history log in workspaceID, if any.
// Called whenever id's current record is deleted outright (TTL expiry --
// see Get/Search/List/History's own expiry handling), since History always
// reports (nil, nil) once the current version is gone, making any retained
// prior-version data permanently unreachable through this package's public
// surface. Deliberately dropping it there rather than leaving it to grow
// forever is this package's chosen answer to "does expiry also drop
// history" -- keeping data no caller can ever ask for again would be a pure
// memory leak, not a feature. Callers must hold s.mu.
func (s *Store) deleteHistoryLocked(workspaceID, id string) {
	if ws, ok := s.history[workspaceID]; ok {
		delete(ws, id)
	}
}

// Get implements MemoryStore.Get.
func (s *Store) Get(ctx context.Context, workspaceID, id string) (Memory, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws, ok := s.records[workspaceID]
	if !ok {
		return Memory{}, false, nil
	}
	rec, ok := ws[id]
	if !ok {
		return Memory{}, false, nil
	}
	if rec.expired(s.now()) {
		delete(ws, id)
		s.vecStore.Delete(workspaceID, id)
		s.deleteHistoryLocked(workspaceID, id)
		return Memory{}, false, nil
	}
	return *rec, true, nil
}

// Search implements MemoryStore.Search.
func (s *Store) Search(ctx context.Context, workspaceID, query string, opts SearchOptions) ([]SearchResult, error) {
	vec, err := s.provider.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	k := opts.K
	if k <= 0 {
		k = defaultSearchK
	}

	var filter map[string]string
	if opts.AgentID != "" || opts.Kind != nil {
		filter = make(map[string]string, 2)
		if opts.AgentID != "" {
			filter["agent_id"] = opts.AgentID
		}
		if opts.Kind != nil {
			filter["kind"] = opts.Kind.String()
		}
	}

	raw := s.vecStore.Search(workspaceID, vec, k, vector.Cosine, filter, nil)
	if len(raw) == 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.records[workspaceID]
	now := s.now()

	out := make([]SearchResult, 0, len(raw))
	for _, r := range raw {
		rec, ok := ws[r.ID]
		if !ok {
			// Shouldn't happen: the vector index and records map are kept
			// in sync by Put/Get. Skip defensively rather than panic.
			continue
		}
		if rec.expired(now) {
			delete(ws, r.ID)
			s.vecStore.Delete(workspaceID, r.ID)
			s.deleteHistoryLocked(workspaceID, r.ID)
			continue
		}
		if opts.Threshold != nil && r.Score < *opts.Threshold {
			// raw is sorted best-first, so once a result drops below
			// threshold every subsequent one will too; but we still
			// `continue` rather than `break` since an expired entry
			// could otherwise be skipped out of score order -- the loop
			// body's eviction work must run for every candidate.
			continue
		}
		out = append(out, SearchResult{ID: r.ID, Score: r.Score})
	}
	return out, nil
}

// History implements MemoryStore.History.
func (s *Store) History(ctx context.Context, workspaceID, id string) ([]Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws, ok := s.records[workspaceID]
	if !ok {
		return nil, nil
	}
	rec, ok := ws[id]
	if !ok {
		return nil, nil
	}
	if rec.expired(s.now()) {
		delete(ws, id)
		s.vecStore.Delete(workspaceID, id)
		s.deleteHistoryLocked(workspaceID, id)
		return nil, nil
	}

	prior := s.history[workspaceID][id]
	out := make([]Memory, 0, len(prior)+1)
	out = append(out, prior...)
	out = append(out, *rec)
	return out, nil
}

// List implements MemoryStore.List.
func (s *Store) List(ctx context.Context, workspaceID string, opts ListOptions) ([]Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws, ok := s.records[workspaceID]
	if !ok {
		return nil, nil
	}

	now := s.now()
	var out []Memory
	for id, rec := range ws {
		if rec.expired(now) {
			delete(ws, id)
			s.vecStore.Delete(workspaceID, id)
			s.deleteHistoryLocked(workspaceID, id)
			continue
		}
		if opts.AgentID != "" && rec.AgentID != opts.AgentID {
			continue
		}
		if opts.Kind != nil && rec.Kind != *opts.Kind {
			continue
		}
		out = append(out, *rec)
	}
	return out, nil
}
