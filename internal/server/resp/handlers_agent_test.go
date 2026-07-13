package resp

import (
	"strconv"
	"strings"
	"testing"
)

func TestAgentRememberThenMemoryGetRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "the user prefers dark mode",
		"KIND", "episodic", "METADATA", `{"source":"chat"}`)
	if !strings.HasPrefix(string(out), "$") {
		t.Fatalf("AGENT.REMEMBER reply = %q, want a bulk string id", out)
	}
	if strings.Contains(string(out), "$-1") {
		t.Fatalf("AGENT.REMEMBER should never return a null id, got %q", out)
	}

	// Parse the bulk string id out of the raw RESP reply: "$<len>\r\n<id>\r\n".
	parts := strings.SplitN(string(out), "\r\n", 3)
	if len(parts) < 2 {
		t.Fatalf("AGENT.REMEMBER reply malformed: %q", out)
	}
	id := parts[1]

	got := execCommand(t, cs, "MEMORY.GET", "default", id)
	s := string(got)
	if !strings.Contains(s, "agent-1") {
		t.Fatalf("MEMORY.GET after AGENT.REMEMBER missing agent_id value: %q", s)
	}
	if !strings.Contains(s, "the user prefers dark mode") {
		t.Fatalf("MEMORY.GET after AGENT.REMEMBER missing content: %q", s)
	}
	if !strings.Contains(s, "episodic") {
		t.Fatalf("MEMORY.GET after AGENT.REMEMBER missing kind episodic: %q", s)
	}
	if !strings.Contains(s, `"source":"chat"`) {
		t.Fatalf("MEMORY.GET after AGENT.REMEMBER missing metadata JSON: %q", s)
	}
}

func TestAgentRememberRecallMetrics(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "the user prefers dark mode")
	execCommand(t, cs, "AGENT.RECALL", "agent-1", "dark mode preference")

	snap := cs.Deps.Metrics.Snapshot()
	if snap.MemoryWritesTotal != 1 {
		t.Fatalf("MemoryWritesTotal = %d, want 1", snap.MemoryWritesTotal)
	}
	if snap.MemoryReadsTotal != 1 {
		t.Fatalf("MemoryReadsTotal = %d, want 1", snap.MemoryReadsTotal)
	}
}

func TestAgentRememberDefaultsMatchMemoryPut(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "some content")
	parts := strings.SplitN(string(out), "\r\n", 3)
	if len(parts) < 2 {
		t.Fatalf("AGENT.REMEMBER reply malformed: %q", out)
	}
	id := parts[1]

	got := execCommand(t, cs, "MEMORY.GET", "default", id)
	s := string(got)
	if !strings.Contains(s, "long_term") {
		t.Fatalf("AGENT.REMEMBER default kind should be long_term: %q", s)
	}
}

func TestAgentRememberHasNoIDOption(t *testing.T) {
	cs := newTestClientState(t)

	// ID isn't a recognized option for AGENT.REMEMBER -- it should be
	// rejected the same way any other unknown option is.
	out := execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "content", "ID", "explicit-id")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("AGENT.REMEMBER with ID option reply = %q, want a syntax error", out)
	}
}

func TestAgentRememberWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "AGENT.REMEMBER", "agent-1")
	want := "-" + ErrWrongNumberOfArgs("agent.remember") + "\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.REMEMBER wrong arity reply = %q, want %q", out, want)
	}
}

func TestAgentRememberInvalidKind(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "content", "KIND", "not_a_kind")
	want := "-" + ErrSyntaxMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.REMEMBER invalid KIND reply = %q, want %q", out, want)
	}
}

func TestAgentRememberInvalidMetadataJSON(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "content", "METADATA", "not json")
	want := "-" + ErrInvalidMetadataJSONMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.REMEMBER invalid metadata JSON reply = %q, want %q", out, want)
	}
}

func TestAgentRecallOnlyReturnsOwnMemories(t *testing.T) {
	cs := newTestClientState(t)

	// Two different agents' near-identical memories in the same workspace.
	execCommand(t, cs, "MEMORY.PUT", "agent-a", "database migration notes", "ID", "a1")
	execCommand(t, cs, "AGENT.REMEMBER", "agent-b", "database migration notes")

	out := execCommand(t, cs, "AGENT.RECALL", "agent-a", "database migration notes")
	want := "*1\r\n$2\r\na1\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.RECALL agent-a reply = %q, want %q", out, want)
	}
	if strings.Contains(string(out), "a1") == false {
		t.Fatalf("AGENT.RECALL agent-a should include its own memory a1: %q", out)
	}
}

func TestAgentRecallDoesNotLeakOtherAgentsMemory(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-a", "shared onboarding notes", "ID", "a1")
	execCommand(t, cs, "MEMORY.PUT", "agent-b", "shared onboarding notes", "ID", "b1")
	execCommand(t, cs, "MEMORY.PUT", "agent-c", "shared onboarding notes", "ID", "c1")

	out := execCommand(t, cs, "AGENT.RECALL", "agent-b", "shared onboarding notes")
	s := string(out)
	if strings.Contains(s, "a1") || strings.Contains(s, "c1") {
		t.Fatalf("AGENT.RECALL agent-b leaked another agent's memory id: %q", s)
	}
	if !strings.Contains(s, "b1") {
		t.Fatalf("AGENT.RECALL agent-b should still find its own memory: %q", s)
	}
}

func TestAgentRecallWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "AGENT.RECALL", "agent-1")
	want := "-" + ErrWrongNumberOfArgs("agent.recall") + "\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.RECALL wrong arity reply = %q, want %q", out, want)
	}
}

func TestAgentRecallKindFilter(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "onboarding notes", "KIND", "long_term")
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "onboarding notes", "ID", "epi1", "KIND", "episodic")

	out := execCommand(t, cs, "AGENT.RECALL", "agent-1", "onboarding notes", "KIND", "episodic")
	want := "*1\r\n$4\r\nepi1\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.RECALL KIND=episodic reply = %q, want %q", out, want)
	}
}

func TestAgentRecallKCapsResults(t *testing.T) {
	cs := newTestClientState(t)

	for i := 0; i < 5; i++ {
		execCommand(t, cs, "MEMORY.PUT", "agent-1", "topic note", "ID", "id"+strconv.Itoa(i))
	}

	out := execCommand(t, cs, "AGENT.RECALL", "agent-1", "topic note", "K", "2")
	want := "*2\r\n"
	if !strings.HasPrefix(string(out), want) {
		t.Fatalf("AGENT.RECALL K=2 reply = %q, want prefix %q", out, want)
	}
}

func TestAgentRecallWithScores(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "hello world", "ID", "m1")

	out := execCommand(t, cs, "AGENT.RECALL", "agent-1", "hello world", "WITHSCORES")
	want := "*2\r\n$2\r\nm1\r\n$1\r\n1\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.RECALL WITHSCORES reply = %q, want %q", out, want)
	}
}

func TestAgentRecallThreshold(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "completely unrelated filler text", "ID", "far1")

	out := execCommand(t, cs, "AGENT.RECALL", "agent-1", "hello world", "THRESHOLD", "0.99")
	want := "*0\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.RECALL with a strict THRESHOLD reply = %q, want %q (nothing clears it)", out, want)
	}
}

func TestAgentRecallNoMatchEmptyArray(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "AGENT.RECALL", "agent-1", "anything")
	want := "*0\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.RECALL (no memories) reply = %q, want %q", out, want)
	}
}

func TestAgentRecallWorkspaceOption(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "workspace scoped note", "ID", "w1", "WORKSPACE", "ws-a")

	// Default workspace shouldn't find it.
	out := execCommand(t, cs, "AGENT.RECALL", "agent-1", "workspace scoped note")
	if string(out) != "*0\r\n" {
		t.Fatalf("AGENT.RECALL default workspace reply = %q, want empty array", out)
	}

	// Explicit WORKSPACE ws-a should find it.
	out = execCommand(t, cs, "AGENT.RECALL", "agent-1", "workspace scoped note", "WORKSPACE", "ws-a")
	want := "*1\r\n$2\r\nw1\r\n"
	if string(out) != want {
		t.Fatalf("AGENT.RECALL WORKSPACE ws-a reply = %q, want %q", out, want)
	}
}

func TestAgentRecallUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "AGENT.RECALL", "agent-1", "query", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("AGENT.RECALL unknown option reply = %q, want a syntax error", out)
	}
}

func TestAgentRememberUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "AGENT.REMEMBER", "agent-1", "content", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("AGENT.REMEMBER unknown option reply = %q, want a syntax error", out)
	}
}
