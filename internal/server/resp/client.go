package resp

import (
	"log/slog"
	"net"
	"sync"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/consolidate"
	"github.com/SumitKumar-17/cache-pot/internal/llm"
	"github.com/SumitKumar-17/cache-pot/internal/memory"
	"github.com/SumitKumar-17/cache-pot/internal/observability"
	"github.com/SumitKumar-17/cache-pot/internal/semantic"
	"github.com/SumitKumar-17/cache-pot/internal/storage"
	"github.com/SumitKumar-17/cache-pot/internal/toolcache"
	"github.com/SumitKumar-17/cache-pot/internal/vector"
)

// defaultWorkspace is the single workspace Phase 1 operates in. The
// Engine/Entry seam already threads a workspace parameter through every
// storage call (see internal/storage/engine.go's doc comment); Phase 1
// simply always passes this constant, so Phase 7 multi-tenancy can
// introduce real per-workspace routing without changing call sites here.
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
	// (Phase 3's native flat vector index, partitioned by namespace);
	// MemoryStore backs MEMORY.PUT/MEMORY.GET/MEMORY.SEARCH (Phase 4's
	// shared agent-memory domain layer, internally using its own
	// *vector.Store instance as a search index -- a separate concern from
	// this VectorStore, which is exposed directly to RESP clients).
	SemanticCache *semantic.SemanticCache
	PromptCache   *semantic.PromptCache
	ToolCache     *toolcache.ToolCache
	VectorStore   *vector.Store
	MemoryStore   *memory.Store

	// Analytics tracks embedding token/cost usage and cache-hit money
	// savings (Phase 5's cost-analytics layer, see internal/analytics),
	// fed by CACHE.SEMANTIC/CACHE.PROMPT's optional COST argument and by
	// the instrumented embed.Provider. It is a separate concern from
	// Metrics, which owns hit/miss counting and hit-rate math.
	Analytics *analytics.Tracker

	// CompletionProvider is Cache-Pot's first text-*generation* provider
	// (Phase 6, see internal/llm): chat-style completions, as opposed to
	// the embeddings-only providers above. It backs Consolidator below
	// (constructed with this exact instance in internal/server/server.go);
	// knowledge-graph (real entity/relationship extraction), landing in a
	// later commit, will be Phase 6's other consumer.
	CompletionProvider llm.CompletionProvider

	// Consolidator backs SUMMARY.CREATE (see handlers_consolidate.go):
	// Phase 6's memory-consolidation entry point, built once in
	// internal/server/server.go from the same shared MemoryStore and
	// CompletionProvider instances above.
	Consolidator *consolidate.Consolidator
}

// ClientState is per-connection state: authentication, the selected
// "workspace" (Phase 1: always defaultWorkspace), transaction/MULTI queueing,
// WATCHed key versions, and pub/sub subscriptions.
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
