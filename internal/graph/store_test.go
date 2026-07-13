package graph

import (
	"sort"
	"testing"
)

func relatedIDs(rs []RelatedNode) []string {
	ids := make([]string, len(rs))
	for i, r := range rs {
		ids[i] = r.Node.ID
	}
	sort.Strings(ids)
	return ids
}

func TestUpsertNodeIdempotentReplace(t *testing.T) {
	s := New()
	s.UpsertNode("ws", Node{ID: "a", Label: "first"})
	s.UpsertNode("ws", Node{ID: "a", Label: "second"})

	n, ok := s.GetNode("ws", "a")
	if !ok {
		t.Fatal("GetNode(a) not found")
	}
	if n.Label != "second" {
		t.Fatalf("Label = %q, want %q (replaced, not duplicated)", n.Label, "second")
	}
}

func TestUpsertEdgeIdempotentReplace(t *testing.T) {
	s := New()
	s.UpsertNode("ws", Node{ID: "a"})
	s.UpsertNode("ws", Node{ID: "b"})
	s.UpsertEdge("ws", Edge{FromID: "a", ToID: "b", Label: "knows", Weight: 1})
	s.UpsertEdge("ws", Edge{FromID: "a", ToID: "b", Label: "knows", Weight: 5})

	related := s.Related("ws", "a", 1)
	if len(related) != 1 {
		t.Fatalf("Related returned %d nodes, want 1 (edge replaced, not duplicated)", len(related))
	}
	if related[0].Edge.Weight != 5 {
		t.Fatalf("Edge.Weight = %v, want 5 (the replacement)", related[0].Edge.Weight)
	}
}

func TestWorkspaceIsolation(t *testing.T) {
	s := New()
	s.UpsertNode("ws1", Node{ID: "a"})
	s.UpsertNode("ws1", Node{ID: "b"})
	s.UpsertEdge("ws1", Edge{FromID: "a", ToID: "b", Label: "rel"})

	if _, ok := s.GetNode("ws2", "a"); ok {
		t.Fatal("GetNode found node from ws1 while looking in ws2")
	}
	if related := s.Related("ws2", "a", 1); related != nil {
		t.Fatalf("Related(ws2, a) = %v, want nil (a doesn't exist in ws2)", related)
	}
	// ws1's own traversal is unaffected.
	if related := s.Related("ws1", "a", 1); len(related) != 1 {
		t.Fatalf("Related(ws1, a) = %v, want 1 node", related)
	}
}

// buildChain constructs the A-B-C-D chain the BFS-depth tests use: A-[to]->B,
// B-[to]->C, C-[to]->D.
func buildChain(s *Store, workspace string) {
	for _, id := range []string{"a", "b", "c", "d"} {
		s.UpsertNode(workspace, Node{ID: id, Label: id})
	}
	s.UpsertEdge(workspace, Edge{FromID: "a", ToID: "b", Label: "to"})
	s.UpsertEdge(workspace, Edge{FromID: "b", ToID: "c", Label: "to"})
	s.UpsertEdge(workspace, Edge{FromID: "c", ToID: "d", Label: "to"})
}

func TestRelatedDepth1FindsOnlyImmediateNeighbor(t *testing.T) {
	s := New()
	buildChain(s, "ws")

	related := s.Related("ws", "a", 1)
	if got := relatedIDs(related); len(got) != 1 || got[0] != "b" {
		t.Fatalf("Related(a, depth=1) = %v, want [b]", got)
	}
	if related[0].Hops != 1 {
		t.Fatalf("Hops = %d, want 1", related[0].Hops)
	}
}

func TestRelatedDepth2FindsTwoHops(t *testing.T) {
	s := New()
	buildChain(s, "ws")

	related := s.Related("ws", "a", 2)
	got := relatedIDs(related)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("Related(a, depth=2) = %v, want [b c]", got)
	}
	for _, r := range related {
		wantHops := 1
		if r.Node.ID == "c" {
			wantHops = 2
		}
		if r.Hops != wantHops {
			t.Errorf("Related(a, depth=2)[%s].Hops = %d, want %d", r.Node.ID, r.Hops, wantHops)
		}
	}
}

func TestRelatedDepthNonPositiveDefaultsToOne(t *testing.T) {
	s := New()
	buildChain(s, "ws")

	related := s.Related("ws", "a", 0)
	if got := relatedIDs(related); len(got) != 1 || got[0] != "b" {
		t.Fatalf("Related(a, depth=0) = %v, want [b] (default depth 1)", got)
	}

	related = s.Related("ws", "a", -5)
	if got := relatedIDs(related); len(got) != 1 || got[0] != "b" {
		t.Fatalf("Related(a, depth=-5) = %v, want [b] (default depth 1)", got)
	}
}

func TestRelatedIsUndirected(t *testing.T) {
	s := New()
	buildChain(s, "ws")

	// The stored edge is a->b (directed), but starting the traversal from
	// b must still discover a: edges are undirected for reachability.
	related := s.Related("ws", "b", 1)
	got := relatedIDs(related)
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("Related(b, depth=1) = %v, want [a c] (undirected)", got)
	}
}

func TestRelatedExcludesStartNode(t *testing.T) {
	s := New()
	s.UpsertNode("ws", Node{ID: "a"})
	s.UpsertNode("ws", Node{ID: "b"})
	s.UpsertEdge("ws", Edge{FromID: "a", ToID: "a", Label: "self"})
	s.UpsertEdge("ws", Edge{FromID: "a", ToID: "b", Label: "to"})

	related := s.Related("ws", "a", 3)
	for _, r := range related {
		if r.Node.ID == "a" {
			t.Fatalf("Related(a) included the start node itself: %+v", related)
		}
	}
	if got := relatedIDs(related); len(got) != 1 || got[0] != "b" {
		t.Fatalf("Related(a) = %v, want [b]", got)
	}
}

func TestRelatedUnknownOrEmptyNodeReturnsEmptyNotError(t *testing.T) {
	s := New()
	s.UpsertNode("ws", Node{ID: "a"})

	if related := s.Related("ws", "does-not-exist", 1); related != nil {
		t.Fatalf("Related(unknown node) = %v, want nil", related)
	}
	if related := s.Related("ws", "", 1); related != nil {
		t.Fatalf("Related(empty node id) = %v, want nil", related)
	}
	if related := s.Related("no-such-workspace", "a", 1); related != nil {
		t.Fatalf("Related(unknown workspace) = %v, want nil", related)
	}
}

func TestGetNodeMissing(t *testing.T) {
	s := New()
	if _, ok := s.GetNode("ws", "nope"); ok {
		t.Fatal("GetNode(missing) reported found=true")
	}
	if _, ok := s.GetNode("no-such-workspace", "nope"); ok {
		t.Fatal("GetNode(missing workspace) reported found=true")
	}
}
