// Package graph implements Phase 6's third and final engineering piece: a
// real, workspace-partitioned knowledge graph over stored memories and the
// entities/relationships extracted from them (GRAPH.EXTRACT/GRAPH.RELATED,
// see internal/server/resp/handlers_graph.go, and the mirroring
// extract_entities/find_related MCP tools, see internal/mcp/server.go).
//
// internal/graph is deliberately a leaf package, mirroring internal/vector's
// own "pure data structure, no upstream dependency" shape: Store itself
// knows nothing about internal/memory or internal/llm. The LLM-calling
// extraction orchestration in extract.go depends on internal/llm (to call
// CompletionProvider.Complete) but still not on internal/memory -- callers
// (the RESP/MCP layers) fetch a memory's id/content themselves and pass
// those in as plain strings, so internal/graph never needs to know what a
// memory.Memory is.
package graph

import "sync"

// Node is a graph vertex, typically corresponding to a memory (id prefixed
// "memory:", see extract.go) or an entity extracted from one.
type Node struct {
	ID       string
	Label    string
	Metadata map[string]string
}

// Edge is a directed, labeled relationship between two nodes. Two edges
// with the same (FromID, ToID, Label) are considered the same edge for
// UpsertEdge's replace-in-place semantics (see UpsertEdge's doc comment) --
// Weight is not part of that identity, so upserting an edge again with a
// different Weight replaces the stored Weight rather than creating a
// second, parallel edge.
type Edge struct {
	FromID string
	ToID   string
	Label  string
	Weight float64
}

// edgeKey identifies an edge for UpsertEdge's upsert-by-(from,to,label)
// semantics, deliberately excluding Weight (see Edge's doc comment).
type edgeKey struct {
	from  string
	to    string
	label string
}

func keyOf(e Edge) edgeKey {
	return edgeKey{from: e.FromID, to: e.ToID, label: e.Label}
}

// workspaceGraph holds one workspace's nodes and edges.
type workspaceGraph struct {
	nodes map[string]Node
	edges map[edgeKey]Edge
}

// Store is a workspace-partitioned, in-memory directed labeled graph --
// nodes and edges never cross workspace boundaries, mirroring how
// internal/vector.Store and internal/memory.Store are partitioned by
// workspace/namespace. It is safe for concurrent use: a single RWMutex
// guards all state. This is not a hot path (see this package's design
// notes), so a single coarse mutex -- rather than per-workspace locking or
// a lock-free structure -- is the right amount of machinery.
type Store struct {
	mu   sync.RWMutex
	byWS map[string]*workspaceGraph
}

// New builds an empty Store.
func New() *Store {
	return &Store{byWS: make(map[string]*workspaceGraph)}
}

// getOrCreate returns workspace's workspaceGraph, creating it if this is the
// first node/edge ever upserted into it. Callers must hold s.mu for writing.
func (s *Store) getOrCreate(workspace string) *workspaceGraph {
	ws, ok := s.byWS[workspace]
	if !ok {
		ws = &workspaceGraph{nodes: make(map[string]Node), edges: make(map[edgeKey]Edge)}
		s.byWS[workspace] = ws
	}
	return ws
}

// UpsertNode adds n to workspace, or replaces the existing node with id
// n.ID if one already exists there. Non-destructive to everything else in
// the graph: replacing a node's Label/Metadata never touches its edges.
func (s *Store) UpsertNode(workspace string, n Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := s.getOrCreate(workspace)
	ws.nodes[n.ID] = n
}

// UpsertEdge adds e to workspace, or replaces the existing edge with the
// same (FromID, ToID, Label) if one already exists there (see Edge's doc
// comment on what identifies an edge). It does not require FromID/ToID to
// already exist as nodes -- Related still traverses such edges, treating
// the far endpoint as absent from results only if it truly has no
// corresponding node (see Related's doc comment).
func (s *Store) UpsertEdge(workspace string, e Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := s.getOrCreate(workspace)
	ws.edges[keyOf(e)] = e
}

// GetNode looks up a node by (workspace, id), reporting found=false if
// workspace or id doesn't exist.
func (s *Store) GetNode(workspace, id string) (Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws, ok := s.byWS[workspace]
	if !ok {
		return Node{}, false
	}
	n, ok := ws.nodes[id]
	return n, ok
}

// RelatedNode is one node discovered by Related: the node itself, the edge
// through which it was first reached, and how many hops away it is from the
// traversal's start node.
type RelatedNode struct {
	Node Node
	Edge Edge
	Hops int
}

// Related does a breadth-first traversal from nodeID up to depth hops,
// treating every edge as undirected for reachability purposes -- a real
// "Redis -[used_by]-> Project A -[maintained_by]-> Alice" relationship is
// meaningful to traverse in either direction when asking "what's related to
// X" (e.g. "what's related to Alice" should surface Project A even though
// the stored edge points the other way). depth <= 0 defaults to 1.
//
// Returns every distinct reachable node (excluding nodeID itself), each
// paired with the edge that connected it at the hop where it was *first*
// discovered -- if a node is reachable by more than one path, only the
// shortest (fewest-hops) discovery is reported, matching plain BFS
// semantics. Returns nil (not an error) if workspace or nodeID doesn't
// exist, or nodeID has no related nodes within depth hops.
func (s *Store) Related(workspace, nodeID string, depth int) []RelatedNode {
	if depth <= 0 {
		depth = 1
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	ws, ok := s.byWS[workspace]
	if !ok {
		return nil
	}
	if _, ok := ws.nodes[nodeID]; !ok {
		return nil
	}

	visited := map[string]bool{nodeID: true}
	frontier := []string{nodeID}
	var out []RelatedNode

	for hop := 1; hop <= depth && len(frontier) > 0; hop++ {
		var next []string
		for _, e := range ws.edges {
			for _, id := range frontier {
				var other string
				switch {
				case e.FromID == id:
					other = e.ToID
				case e.ToID == id:
					other = e.FromID
				default:
					continue
				}
				if visited[other] {
					continue
				}
				visited[other] = true
				n, ok := ws.nodes[other]
				if !ok {
					// A dangling edge referencing a node that was never
					// (or no longer is) upserted. Mark visited so it isn't
					// re-considered on a later hop, but don't report it --
					// Related only ever returns real nodes.
					continue
				}
				out = append(out, RelatedNode{Node: n, Edge: e, Hops: hop})
				next = append(next, other)
			}
		}
		frontier = next
	}
	return out
}
