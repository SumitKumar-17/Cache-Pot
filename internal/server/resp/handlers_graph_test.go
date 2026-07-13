package resp

import (
	"strings"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/graph"
)

// TestGraphExtractWithMockDegradesGracefully is the RESP-layer analogue of
// internal/graph's own TestExtractWithMockDegradesGracefully: with the mock
// CompletionProvider (newTestDeps's default, see handlers_string_test.go),
// GRAPH.EXTRACT on a real memory must return [0, 0] -- two RESP integers,
// not an error -- because the mock cannot produce the JSON
// internal/graph.Extract asks for. This documents that expectation
// explicitly: a caller reading [0, 0] back should NOT conclude GRAPH.EXTRACT
// is broken -- it's the honest result of not having a real LLM configured.
func TestGraphExtractWithMockDegradesGracefully(t *testing.T) {
	cs := newTestClientState(t)

	memID := string(execCommand(t, cs, "MEMORY.PUT", "agent-1", "Redis is used by Project A, which is maintained by Alice."))
	id := bulkPayload(t, memID)

	out := execCommand(t, cs, "GRAPH.EXTRACT", "default", id)
	want := "*2\r\n:0\r\n:0\r\n"
	if string(out) != want {
		t.Fatalf("GRAPH.EXTRACT (mock provider) reply = %q, want %q ([0, 0] -- honest \"nothing extracted\", not an error)", out, want)
	}
}

// TestGraphExtractNoSuchMemoryIsError confirms GRAPH.EXTRACT on a memory id
// that doesn't exist in the workspace is a real error -- unlike MEMORY.GET's
// legitimate nil-array "not found" reply, GRAPH.EXTRACT has nothing to
// extract from without a real memory.
func TestGraphExtractNoSuchMemoryIsError(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "GRAPH.EXTRACT", "default", "does-not-exist")
	want := "-" + ErrNoSuchMemoryMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("GRAPH.EXTRACT (no such memory) reply = %q, want %q", out, want)
	}
}

// TestGraphExtractWrongArity checks arity validation on GRAPH.EXTRACT.
func TestGraphExtractWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "GRAPH.EXTRACT", "default")
	want := "-" + ErrWrongNumberOfArgs("graph.extract") + "\r\n"
	if string(out) != want {
		t.Fatalf("GRAPH.EXTRACT wrong arity reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "GRAPH.EXTRACT", "default", "id", "extra")
	if string(out) != want {
		t.Fatalf("GRAPH.EXTRACT too many args reply = %q, want %q", out, want)
	}
}

// TestGraphExtractMetrics confirms a successful GRAPH.EXTRACT call (even one
// that extracts nothing, per the mock) records the graph-extraction and
// entities/relations-extracted counters.
func TestGraphExtractMetrics(t *testing.T) {
	cs := newTestClientState(t)

	memID := string(execCommand(t, cs, "MEMORY.PUT", "agent-1", "some content"))
	id := bulkPayload(t, memID)
	execCommand(t, cs, "GRAPH.EXTRACT", "default", id)

	snap := cs.Deps.Metrics.Snapshot()
	if snap.GraphExtractionsTotal != 1 {
		t.Fatalf("GraphExtractionsTotal = %d, want 1", snap.GraphExtractionsTotal)
	}
	if snap.EntitiesExtractedTotal != 0 || snap.RelationsExtractedTotal != 0 {
		t.Fatalf("EntitiesExtractedTotal/RelationsExtractedTotal = %d/%d, want 0/0 (mock provider extracts nothing)",
			snap.EntitiesExtractedTotal, snap.RelationsExtractedTotal)
	}
}

// TestGraphRelatedRoundTrip populates cs.Deps.GraphStore directly (bypassing
// the mock CompletionProvider's inability to produce a real extraction --
// see TestGraphExtractWithMockDegradesGracefully above) so GRAPH.RELATED's
// actual BFS traversal can be exercised end-to-end through the RESP layer.
func TestGraphRelatedRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	cs.Deps.GraphStore.UpsertNode("default", graph.Node{ID: "redis", Label: "Redis"})
	cs.Deps.GraphStore.UpsertNode("default", graph.Node{ID: "project_a", Label: "Project A"})
	cs.Deps.GraphStore.UpsertNode("default", graph.Node{ID: "alice", Label: "Alice"})
	cs.Deps.GraphStore.UpsertEdge("default", graph.Edge{FromID: "redis", ToID: "project_a", Label: "used_by"})
	cs.Deps.GraphStore.UpsertEdge("default", graph.Edge{FromID: "project_a", ToID: "alice", Label: "maintained_by"})

	// Default depth (1): only the immediate neighbor.
	out := execCommand(t, cs, "GRAPH.RELATED", "default", "redis")
	want := "*1\r\n$9\r\nproject_a\r\n"
	if string(out) != want {
		t.Fatalf("GRAPH.RELATED (default depth) reply = %q, want %q", out, want)
	}

	// Explicit DEPTH 2: two hops away.
	out = execCommand(t, cs, "GRAPH.RELATED", "default", "redis", "DEPTH", "2")
	want = "*2\r\n$9\r\nproject_a\r\n$5\r\nalice\r\n"
	if string(out) != want {
		t.Fatalf("GRAPH.RELATED DEPTH 2 reply = %q, want %q", out, want)
	}

	// Undirected: starting from alice must find project_a even though the
	// stored edge points the other way.
	out = execCommand(t, cs, "GRAPH.RELATED", "default", "alice")
	want = "*1\r\n$9\r\nproject_a\r\n"
	if string(out) != want {
		t.Fatalf("GRAPH.RELATED(alice) reply = %q, want %q (undirected traversal)", out, want)
	}
}

// TestGraphRelatedEmptyNodeID confirms an empty node_id is an empty array,
// not an error -- consistent with VECTOR.SEARCH/MEMORY.SEARCH's "empty
// result is not an error" convention.
func TestGraphRelatedEmptyNodeID(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "GRAPH.RELATED", "default", "")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("GRAPH.RELATED (empty node_id) reply = %q, want %q", out, want)
	}
}

// TestGraphRelatedUnknownNodeEmpty confirms an unknown node_id is an empty
// array, not an error.
func TestGraphRelatedUnknownNodeEmpty(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "GRAPH.RELATED", "default", "does-not-exist")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("GRAPH.RELATED (unknown node) reply = %q, want %q", out, want)
	}
}

// TestGraphRelatedDepthNonPositiveIsSyntaxError confirms an explicit
// non-positive DEPTH is rejected as a syntax error at the RESP layer, unlike
// internal/graph.Store.Related's own "depth <= 0 defaults to 1" behavior
// when called directly from Go.
func TestGraphRelatedDepthNonPositiveIsSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	cs.Deps.GraphStore.UpsertNode("default", graph.Node{ID: "a"})

	out := execCommand(t, cs, "GRAPH.RELATED", "default", "a", "DEPTH", "0")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("GRAPH.RELATED DEPTH 0 reply = %q, want a syntax error", out)
	}

	out = execCommand(t, cs, "GRAPH.RELATED", "default", "a", "DEPTH", "-1")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("GRAPH.RELATED DEPTH -1 reply = %q, want a syntax error", out)
	}
}

// TestGraphRelatedDepthNonNumericIsError confirms a non-numeric DEPTH is
// rejected using the existing not-an-integer error helper.
func TestGraphRelatedDepthNonNumericIsError(t *testing.T) {
	cs := newTestClientState(t)
	cs.Deps.GraphStore.UpsertNode("default", graph.Node{ID: "a"})

	out := execCommand(t, cs, "GRAPH.RELATED", "default", "a", "DEPTH", "not_a_number")
	want := "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("GRAPH.RELATED DEPTH not_a_number reply = %q, want %q", out, want)
	}
}

// TestGraphRelatedUnknownOptionSyntaxError confirms an unrecognized option
// keyword is a syntax error.
func TestGraphRelatedUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	cs.Deps.GraphStore.UpsertNode("default", graph.Node{ID: "a"})

	out := execCommand(t, cs, "GRAPH.RELATED", "default", "a", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("GRAPH.RELATED unknown option reply = %q, want a syntax error", out)
	}
}

// TestGraphRelatedWrongArity checks arity validation on GRAPH.RELATED.
func TestGraphRelatedWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "GRAPH.RELATED", "default")
	want := "-" + ErrWrongNumberOfArgs("graph.related") + "\r\n"
	if string(out) != want {
		t.Fatalf("GRAPH.RELATED wrong arity reply = %q, want %q", out, want)
	}
}

// TestGraphCommandsUnrestrictedInSinglePasswordMode is the regression test
// proving Phase 7's multi-workspace enforcement did NOT change today's
// default (single-password/no-auth) behavior: GRAPH.EXTRACT/GRAPH.RELATED
// against a workspace other than "default" still work completely
// unrestricted.
func TestGraphCommandsUnrestrictedInSinglePasswordMode(t *testing.T) {
	cs := newTestClientState(t)

	memID := string(execCommand(t, cs, "MEMORY.PUT", "agent-1", "note", "WORKSPACE", "some-other-workspace"))
	id := bulkPayload(t, memID)

	out := execCommand(t, cs, "GRAPH.EXTRACT", "some-other-workspace", id)
	if want := "*2\r\n:0\r\n:0\r\n"; string(out) != want {
		t.Fatalf("GRAPH.EXTRACT (other workspace, single-password mode) = %q, want %q", out, want)
	}

	cs.Deps.GraphStore.UpsertNode("some-other-workspace", graph.Node{ID: "a"})
	out = execCommand(t, cs, "GRAPH.RELATED", "some-other-workspace", "a")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("GRAPH.RELATED (other workspace, single-password mode) = %q, want %q", out, want)
	}
}

// TestGraphCommandsMultiWorkspaceIsolation is Phase 7's actual isolation
// test: a connection authenticated for workspace "acme" gets a real
// NOPERM-style rejection when it tries to use workspace "other", and
// succeeds when it uses its own workspace "acme".
func TestGraphCommandsMultiWorkspaceIsolation(t *testing.T) {
	cs := newTestClientStateWithMultiWorkspaceAuth(t,
		auth.Credential{Workspace: "acme", Password: "pass1"},
		auth.Credential{Workspace: "other", Password: "pass2"},
	)
	execCommand(t, cs, "AUTH", "pass1")

	memID := string(execCommand(t, cs, "MEMORY.PUT", "agent-1", "note", "WORKSPACE", "acme"))
	id := bulkPayload(t, memID)

	want := "-" + ErrWorkspaceNotAuthorized("other") + "\r\n"
	out := execCommand(t, cs, "GRAPH.EXTRACT", "other", id)
	if string(out) != want {
		t.Fatalf("GRAPH.EXTRACT other workspace (authed as acme) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "GRAPH.RELATED", "other", "a")
	if string(out) != want {
		t.Fatalf("GRAPH.RELATED other workspace (authed as acme) = %q, want %q", out, want)
	}

	// Its own workspace works fine.
	out = execCommand(t, cs, "GRAPH.EXTRACT", "acme", id)
	if wantOK := "*2\r\n:0\r\n:0\r\n"; string(out) != wantOK {
		t.Fatalf("GRAPH.EXTRACT own workspace (acme) = %q, want %q", out, wantOK)
	}
	cs.Deps.GraphStore.UpsertNode("acme", graph.Node{ID: "a"})
	out = execCommand(t, cs, "GRAPH.RELATED", "acme", "a")
	if wantOK := "*0\r\n"; string(out) != wantOK {
		t.Fatalf("GRAPH.RELATED own workspace (acme) = %q, want %q", out, wantOK)
	}
}

// bulkPayload extracts the payload from a single RESP2 bulk-string reply's
// raw wire bytes (as returned by execCommand), e.g. "$3\r\nfoo\r\n" -> "foo".
func bulkPayload(t *testing.T, raw string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(raw, "\r\n"), "\r\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "$") {
		t.Fatalf("bulkPayload: unexpected bulk-string framing: %q", raw)
	}
	return lines[1]
}
