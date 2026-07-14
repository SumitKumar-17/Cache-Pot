package resp

import (
	"log/slog"
	"net"
	"sync"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/consolidate"
	"github.com/SumitKumar-17/cache-pot/internal/graph"
	"github.com/SumitKumar-17/cache-pot/internal/llm"
	"github.com/SumitKumar-17/cache-pot/internal/memory"
	"github.com/SumitKumar-17/cache-pot/internal/observability"
	"github.com/SumitKumar-17/cache-pot/internal/semantic"
	"github.com/SumitKumar-17/cache-pot/internal/storage"
	"github.com/SumitKumar-17/cache-pot/internal/toolcache"
	"github.com/SumitKumar-17/cache-pot/internal/vector"
)

// defaultWorkspace is the workspace used when a connection isn't
// authenticated into a specific one (single-password/no-auth mode, or before
// AUTH in multi-workspace mode). The Engine/Entry seam threads a workspace
// parameter through every storage call (see internal/storage/engine.go's doc
// comment); real per-workspace auth (internal/auth, authorizedForWorkspace)
// builds on that existing routing.
const defaultWorkspace = "default"

// Deps bundles the shared, connection-independent dependencies every
// handler may need.
type Deps struct {
	Engine   storage.Engine
	Auth     *auth.Authenticator
	Metrics  *observability.Metrics
	Logger   *slog.Logger
	PubSub   *PubSub
	Registry *Registry

	// SemanticCache backs CACHE.SEMANTIC (similarity-based LLM response
	// cache); PromptCache backs CACHE.PROMPT (exact-match template cache);
	// ToolCache backs TOOL.CACHE (exact-match agent tool-call result
	// cache); VectorStore backs VECTOR.UPSERT/VECTOR.SEARCH/VECTOR.DELETE
	// (the native flat vector index, partitioned by namespace);
	// MemoryStore backs MEMORY.PUT/MEMORY.GET/MEMORY.SEARCH (the shared
	// agent-memory domain layer, internally using its own *vector.Store
	// instance as a search index -- a separate concern from this
	// VectorStore, which is exposed directly to RESP clients).
	SemanticCache *semantic.SemanticCache
	PromptCache   *semantic.PromptCache
	ToolCache     *toolcache.ToolCache
	VectorStore   *vector.Store
	MemoryStore   *memory.Store

	// Analytics tracks embedding token/cost usage and cache-hit money
	// savings (see internal/analytics), fed by CACHE.SEMANTIC/CACHE.PROMPT's
	// optional COST argument and by the instrumented embed.Provider. It is
	// a separate concern from Metrics, which owns hit/miss counting and
	// hit-rate math.
	Analytics *analytics.Tracker

	// CompletionProvider is Cache-Pot's text-*generation* provider (see
	// internal/llm): chat-style completions, as opposed to the
	// embeddings-only providers above. It backs both Consolidator and
	// GraphStore below (constructed with this exact instance in
	// internal/server/server.go).
	CompletionProvider llm.CompletionProvider

	// Consolidator backs SUMMARY.CREATE (see handlers_consolidate.go): the
	// memory-consolidation entry point, built once in
	// internal/server/server.go from the same shared MemoryStore and
	// CompletionProvider instances above.
	Consolidator *consolidate.Consolidator

	// GraphStore backs GRAPH.EXTRACT/GRAPH.RELATED (see handlers_graph.go):
	// a workspace-partitioned knowledge graph of entities/relationships
	// extracted (via CompletionProvider above and internal/graph.Extract)
	// from memories in MemoryStore. Constructed once in
	// internal/server/server.go and shared with the MCP
	// extract_entities/find_related tools, the same "construct once, pass
	// shared instances in" discipline every other store above follows.
	GraphStore *graph.Store
}

// ClientState is per-connection state: authentication, the connection's
// workspace (defaultWorkspace, unless multi-workspace AUTH set it to
// something else), transaction/MULTI queueing, WATCHed key versions, and
// pub/sub subscriptions.
type ClientState struct {
	Deps   *Deps
	Conn   net.Conn
	Writer *Writer

	// writeMu serializes writes to Writer between the main command loop
	// and the pub/sub forwarder goroutine (started once a client
	// SUBSCRIBEs), since both can write to the same connection.
	writeMu sync.Mutex

	Authenticated bool
	Name          string
	Workspace     string

	Quit bool

	// MULTI/EXEC transaction state.
	InMulti    bool
	MultiError bool
	Queued     [][]string

	// WATCH state: key -> version observed at WATCH time.
	Watched map[string]uint64

	// Pub/Sub state.
	Subscriptions  map[string]struct{}
	PSubscriptions map[string]struct{}
	subCh          chan Message
}

// authorizedForWorkspace reports whether cs is permitted to operate against
// the given workspace. In single-password mode (the default), every
// workspace is permitted: there is no enforcement here, so existing
// single-password deployments are unaffected. In multi-workspace mode, only
// the connection's own authenticated cs.Workspace is permitted -- this is
// the actual isolation boundary, checked by every command that takes an
// explicit workspace/namespace argument (see handlers_memory.go,
// handlers_agent.go, handlers_vector.go, handlers_graph.go).
func (cs *ClientState) authorizedForWorkspace(workspace string) bool {
	if !cs.Deps.Auth.MultiWorkspace() {
		return true
	}
	return workspace == cs.Workspace
}

// NewClientState builds the initial state for a freshly accepted connection.
func NewClientState(deps *Deps, conn net.Conn, w *Writer) *ClientState {
	return &ClientState{
		Deps:      deps,
		Conn:      conn,
		Writer:    w,
		Workspace: defaultWorkspace,
	}
}

// writeReply writes a single reply, holding writeMu so it can't interleave
// with a concurrent pub/sub push to the same connection.
func (cs *ClientState) writeReply(r Reply) error {
	if r == nil {
		return nil
	}
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()
	return r(cs.Writer)
}

// flush flushes the writer, holding writeMu for the same reason as
// writeReply.
func (cs *ClientState) flush() error {
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()
	return cs.Writer.Flush()
}
