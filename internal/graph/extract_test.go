package graph

import (
	"context"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/llm"
)

// fakeCompletionProvider is a small test double -- NOT internal/llm.NewMock
// -- that returns a fixed, well-formed response regardless of input, so
// Extract's JSON-parsing and graph-building path can be tested end-to-end
// without depending on a real LLM.
type fakeCompletionProvider struct {
	response string
	err      error
}

func (f *fakeCompletionProvider) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, llm.TokenUsage, error) {
	if f.err != nil {
		return "", llm.TokenUsage{}, f.err
	}
	return f.response, llm.TokenUsage{}, nil
}

func (f *fakeCompletionProvider) Name() string { return "fake" }

// TestExtractWithMockDegradesGracefully is the single most important test
// in this package: internal/llm's mock CompletionProvider (see
// internal/llm/mock.go) is task-agnostic and cannot produce the JSON shape
// Extract asks for -- it just echoes back a truncated slice of the input
// text. This confirms Extract treats that failure as an honest "nothing
// extracted" (zero counts, nil error), not a panic, not a fabricated graph,
// and leaves the graph store completely untouched.
func TestExtractWithMockDegradesGracefully(t *testing.T) {
	store := New()
	mock := llm.NewMock()

	entities, relations, err := Extract(context.Background(), mock, store, "ws", "mem-1",
		"Redis is used by Project A, which is maintained by Alice.")
	if err != nil {
		t.Fatalf("Extract with mock provider returned an error: %v -- want nil error (graceful degradation)", err)
	}
	if entities != 0 || relations != 0 {
		t.Fatalf("Extract with mock provider = (%d, %d), want (0, 0)", entities, relations)
	}

	// The graph store must be completely untouched: no memory-provenance
	// node, no stray entities.
	if _, ok := store.GetNode("ws", memoryNodeID("mem-1")); ok {
		t.Fatal("Extract with mock provider created a memory-provenance node despite extracting nothing")
	}
	if related := store.Related("ws", memoryNodeID("mem-1"), 1); related != nil {
		t.Fatalf("Related on the (nonexistent) memory node = %v, want nil", related)
	}
}

// TestExtractWithFakeProviderPopulatesGraph confirms that, given a
// well-formed JSON response matching Extract's exact schema, entities and
// relations land in the graph store correctly, including the
// memory-provenance node and its "mentions" edges to every extracted
// entity.
func TestExtractWithFakeProviderPopulatesGraph(t *testing.T) {
	store := New()
	fake := &fakeCompletionProvider{response: `{
		"entities": [
			{"id": "redis", "label": "Redis"},
			{"id": "project_a", "label": "Project A"},
			{"id": "alice", "label": "Alice"}
		],
		"relations": [
			{"from": "redis", "to": "project_a", "label": "used_by"},
			{"from": "project_a", "to": "alice", "label": "maintained_by"}
		]
	}`}

	entities, relations, err := Extract(context.Background(), fake, store, "ws", "mem-1",
		"Redis is used by Project A, which is maintained by Alice.")
	if err != nil {
		t.Fatalf("Extract: unexpected error: %v", err)
	}
	if entities != 3 {
		t.Fatalf("entitiesAdded = %d, want 3", entities)
	}
	if relations != 2 {
		t.Fatalf("relationsAdded = %d, want 2", relations)
	}

	for _, id := range []string{"redis", "project_a", "alice"} {
		if _, ok := store.GetNode("ws", id); !ok {
			t.Errorf("entity node %q missing from graph store", id)
		}
	}

	redisNode, ok := store.GetNode("ws", "redis")
	if !ok || redisNode.Label != "Redis" {
		t.Fatalf("redis node = %+v, ok=%v, want Label=Redis", redisNode, ok)
	}

	// The extracted relation redis -[used_by]-> project_a must be
	// traversable (Related treats edges as undirected).
	related := store.Related("ws", "redis", 1)
	found := false
	for _, r := range related {
		if r.Node.ID == "project_a" && r.Edge.Label == "used_by" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Related(redis, 1) = %+v, want to include project_a via a used_by edge", related)
	}

	// Memory-provenance node and its "mentions" edges to every extracted
	// entity.
	memNode, ok := store.GetNode("ws", "memory:mem-1")
	if !ok || memNode.Label != "memory" {
		t.Fatalf("memory-provenance node = %+v, ok=%v, want Label=memory", memNode, ok)
	}
	mentioned := store.Related("ws", "memory:mem-1", 1)
	mentionedIDs := map[string]bool{}
	for _, r := range mentioned {
		if r.Edge.Label == "mentions" && r.Edge.FromID == "memory:mem-1" {
			mentionedIDs[r.Node.ID] = true
		}
	}
	for _, id := range []string{"redis", "project_a", "alice"} {
		if !mentionedIDs[id] {
			t.Errorf("memory:mem-1 has no mentions edge to %q; mentioned=%v", id, mentionedIDs)
		}
	}
}

// TestExtractInvalidJSONGracefulZero confirms non-JSON completion output
// (a stand-in for any misbehaving provider, not just the mock) degrades the
// same way the mock does: zero counts, nil error.
func TestExtractInvalidJSONGracefulZero(t *testing.T) {
	store := New()
	fake := &fakeCompletionProvider{response: "this is not json at all"}

	entities, relations, err := Extract(context.Background(), fake, store, "ws", "mem-1", "some content")
	if err != nil {
		t.Fatalf("Extract with non-JSON response returned an error: %v, want nil", err)
	}
	if entities != 0 || relations != 0 {
		t.Fatalf("Extract with non-JSON response = (%d, %d), want (0, 0)", entities, relations)
	}
}

// TestExtractEmptyEntitiesGracefulZero confirms a well-formed but empty
// response ({"entities":[],"relations":[]}) also results in zero counts
// and no memory-provenance node -- there is nothing for it to mention.
func TestExtractEmptyEntitiesGracefulZero(t *testing.T) {
	store := New()
	fake := &fakeCompletionProvider{response: `{"entities":[],"relations":[]}`}

	entities, relations, err := Extract(context.Background(), fake, store, "ws", "mem-1", "some content")
	if err != nil {
		t.Fatalf("Extract with empty entities returned an error: %v, want nil", err)
	}
	if entities != 0 || relations != 0 {
		t.Fatalf("Extract with empty entities = (%d, %d), want (0, 0)", entities, relations)
	}
	if _, ok := store.GetNode("ws", memoryNodeID("mem-1")); ok {
		t.Fatal("Extract with empty entities created a memory-provenance node")
	}
}

// TestExtractCompletionErrorPropagates confirms a genuine failure to call
// the completion provider at all (as opposed to a successful call that
// returns non-JSON text) is reported as a real error, not swallowed.
func TestExtractCompletionErrorPropagates(t *testing.T) {
	store := New()
	wantErr := context.Canceled
	fake := &fakeCompletionProvider{err: wantErr}

	_, _, err := Extract(context.Background(), fake, store, "ws", "mem-1", "some content")
	if err == nil {
		t.Fatal("Extract with a failing completion provider returned nil error, want a real error")
	}
}
