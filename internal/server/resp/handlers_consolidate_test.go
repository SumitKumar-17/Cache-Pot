package resp

import (
	"strings"
	"testing"
)

func TestSummaryCreateRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "user completed the onboarding flow", "KIND", "episodic")
	execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "User completed the onboarding flow", "KIND", "episodic")
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "the weather in paris is nice today", "KIND", "episodic")

	out := execCommand(t, cs, "SUMMARY.CREATE", "agent-1")
	if !strings.HasPrefix(string(out), "$") {
		t.Fatalf("SUMMARY.CREATE reply = %q, want a bulk string id", out)
	}
	if strings.Contains(string(out), "$-1") {
		t.Fatalf("SUMMARY.CREATE reply = %q, want a real id, not a null bulk string", out)
	}

	// Extract the returned id and confirm MEMORY.GET can fetch a real
	// long_term memory there.
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\r\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected bulk-string framing: %q", out)
	}
	id := lines[1]

	getOut := string(execCommand(t, cs, "MEMORY.GET", "default", id))
	if !strings.Contains(getOut, "long_term") {
		t.Fatalf("MEMORY.GET on SUMMARY.CREATE's id = %q, want it to report KIND long_term", getOut)
	}
	if !strings.Contains(getOut, "agent-1") {
		t.Fatalf("MEMORY.GET on SUMMARY.CREATE's id = %q, want agent_id agent-1", getOut)
	}
	if !strings.Contains(getOut, "consolidated_from_kind") {
		t.Fatalf("MEMORY.GET on SUMMARY.CREATE's id = %q, want provenance metadata", getOut)
	}

	// The original source memories must still all be present.
	searchOut := string(execCommand(t, cs, "MEMORY.SEARCH", "default", "onboarding flow", "AGENT", "agent-1", "KIND", "episodic", "K", "10"))
	if strings.Count(searchOut, "\r\n$") < 2 {
		t.Fatalf("MEMORY.SEARCH after SUMMARY.CREATE = %q, want the source episodic memories to still be findable", searchOut)
	}
}

func TestSummaryCreateNothingToSummarizeReturnsNullBulk(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SUMMARY.CREATE", "agent-with-no-memories")
	want := "$-1\r\n"
	if string(out) != want {
		t.Fatalf("SUMMARY.CREATE (no memories) reply = %q, want %q (null bulk string)", out, want)
	}
}

func TestSummaryCreateDefaultsToEpisodicKind(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "a long-term fact", "KIND", "long_term")

	// No episodic memories exist, only a long_term one -- SUMMARY.CREATE's
	// default KIND (episodic) should see nothing to summarize.
	out := execCommand(t, cs, "SUMMARY.CREATE", "agent-1")
	want := "$-1\r\n"
	if string(out) != want {
		t.Fatalf("SUMMARY.CREATE (default KIND=episodic, only a long_term memory exists) = %q, want %q", out, want)
	}

	// Explicitly asking for long_term should find it.
	out = execCommand(t, cs, "SUMMARY.CREATE", "agent-1", "KIND", "long_term")
	if strings.Contains(string(out), "$-1") {
		t.Fatalf("SUMMARY.CREATE KIND=long_term = %q, want a real summary id", out)
	}
}

func TestSummaryCreateInvalidKindSyntaxError(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SUMMARY.CREATE", "agent-1", "KIND", "not_a_kind")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("SUMMARY.CREATE invalid KIND reply = %q, want a syntax error", out)
	}
}

func TestSummaryCreateInvalidDedupThreshold(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SUMMARY.CREATE", "agent-1", "DEDUP_THRESHOLD", "not_a_float")
	want := "-" + ErrNotFloatMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("SUMMARY.CREATE invalid DEDUP_THRESHOLD reply = %q, want %q", out, want)
	}
}

func TestSummaryCreateWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SUMMARY.CREATE")
	want := "-" + ErrWrongNumberOfArgs("summary.create") + "\r\n"
	if string(out) != want {
		t.Fatalf("SUMMARY.CREATE wrong arity reply = %q, want %q", out, want)
	}
}

func TestSummaryCreateUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SUMMARY.CREATE", "agent-1", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("SUMMARY.CREATE unknown option reply = %q, want a syntax error", out)
	}
}

func TestSummaryCreateMetrics(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "user completed the onboarding flow", "KIND", "episodic")
	execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "User completed the onboarding flow", "KIND", "episodic")

	execCommand(t, cs, "SUMMARY.CREATE", "agent-1")

	snap := cs.Deps.Metrics.Snapshot()
	if snap.ConsolidationsTotal != 1 {
		t.Fatalf("ConsolidationsTotal = %d, want 1", snap.ConsolidationsTotal)
	}
	if snap.MemoriesDedupedTotal != 1 {
		t.Fatalf("MemoriesDedupedTotal = %d, want 1 (one of the two near-duplicates dropped)", snap.MemoriesDedupedTotal)
	}
}
