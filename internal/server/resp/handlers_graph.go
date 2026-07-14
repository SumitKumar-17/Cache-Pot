package resp

import (
	"context"
	"strconv"
	"strings"

	"github.com/SumitKumar-17/cache-pot/internal/graph"
)

// defaultGraphDepth is GRAPH.RELATED's default DEPTH when omitted, matching
// internal/graph.Store.Related's own "depth <= 0 defaults to 1" behavior.
const defaultGraphDepth = 1

// RegisterGraph adds the knowledge-graph commands GRAPH.EXTRACT and
// GRAPH.RELATED, backed by internal/graph's workspace-partitioned in-memory
// graph store and (for GRAPH.EXTRACT) the shared llm.CompletionProvider.
func RegisterGraph(r *Registry) {
	r.Register(&Command{Name: "GRAPH.EXTRACT", MinArgs: 3, MaxArgs: 3, Handler: handleGraphExtract})
	r.Register(&Command{Name: "GRAPH.RELATED", MinArgs: 3, MaxArgs: -1, Handler: handleGraphRelated})
}

// handleGraphExtract implements:
//
//	GRAPH.EXTRACT <workspace> <memory_id>
//
// Fetches memory_id from workspace via MemoryStore.Get, then runs
// internal/graph.Extract against its content: asks the shared
// CompletionProvider to extract entities/relationships and records them
// (plus a memory-provenance node/edges) into GraphStore. Returns
// [entities_added, relations_added] as a 2-element array of RESP integers.
//
// Returns ErrNoSuchMemoryMsg if memory_id doesn't exist (or has expired) in
// workspace -- unlike MEMORY.GET's legitimate nil-array "not found" reply,
// GRAPH.EXTRACT has nothing to extract from without a real memory, so a
// missing id is a genuine error here.
//
// IMPORTANT: with the mock CompletionProvider (the default unless
// --completion-provider=openai is configured), this always returns [0, 0]
// -- not an error. The mock cannot produce the JSON internal/graph.Extract
// asks for (see internal/llm/mock.go and internal/graph/extract.go's doc
// comments), and Extract treats that as an honest "nothing extracted"
// rather than fabricating a graph or erroring out.
func handleGraphExtract(cs *ClientState, args []string) Reply {
	workspace := args[1]
	memoryID := args[2]

	if !cs.authorizedForWorkspace(workspace) {
		return Err(ErrWorkspaceNotAuthorized(workspace))
	}

	mem, found, err := cs.Deps.MemoryStore.Get(context.Background(), workspace, memoryID)
	if err != nil {
		return Err("ERR " + err.Error())
	}
	if !found {
		return Err(ErrNoSuchMemoryMsg)
	}

	entities, relations, err := graph.Extract(context.Background(), cs.Deps.CompletionProvider, cs.Deps.GraphStore, workspace, mem.ID, mem.Content)
	if err != nil {
		return Err("ERR " + err.Error())
	}
	cs.Deps.Metrics.GraphExtractionPerformed()
	cs.Deps.Metrics.EntitiesExtracted(int64(entities))
	cs.Deps.Metrics.RelationsExtracted(int64(relations))

	return Array(Int(int64(entities)), Int(int64(relations)))
}

// handleGraphRelated implements:
//
//	GRAPH.RELATED <workspace> <node_id> [DEPTH <n>]
//
// Returns a RESP array of related node ids, via internal/graph.Store.Related
// (BFS up to DEPTH hops, edges treated as undirected, start node excluded --
// see that method's doc comment). DEPTH defaults to 1 if omitted. A
// non-numeric DEPTH is ErrNotIntegerMsg; a numeric but non-positive DEPTH is
// ErrSyntaxMsg -- both existing error helpers, since GRAPH.RELATED treats an
// explicit non-positive DEPTH as caller error rather than silently falling
// back to the default the way the underlying Related method does when
// called directly from Go.
//
// An unknown/empty node_id, or a node with no related nodes within DEPTH
// hops, returns an empty array, never an error -- consistent with
// VECTOR.SEARCH/MEMORY.SEARCH's "empty result is not an error" convention.
func handleGraphRelated(cs *ClientState, args []string) Reply {
	workspace := args[1]
	nodeID := args[2]
	depth := defaultGraphDepth

	if !cs.authorizedForWorkspace(workspace) {
		return Err(ErrWorkspaceNotAuthorized(workspace))
	}

	for i := 3; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return Err(ErrSyntaxMsg)
		}
		switch strings.ToUpper(args[i]) {
		case "DEPTH":
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return Err(ErrNotIntegerMsg)
			}
			if n <= 0 {
				return Err(ErrSyntaxMsg)
			}
			depth = n
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	related := cs.Deps.GraphStore.Related(workspace, nodeID, depth)
	items := make([]Reply, 0, len(related))
	for _, r := range related {
		items = append(items, BulkString(r.Node.ID))
	}
	return ArraySlice(items)
}
