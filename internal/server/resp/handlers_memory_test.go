package resp

import (
	"strconv"
	"strings"
	"testing"
)

func TestMemoryPutGetRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.PUT", "agent-1", "the user prefers dark mode",
		"ID", "mem-1", "METADATA", `{"source":"chat"}`)
	want := "$5\r\nmem-1\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.PUT reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "MEMORY.GET", "default", "mem-1")
	s := string(out)
	if !strings.Contains(s, "id") || !strings.Contains(s, "mem-1") {
		t.Fatalf("MEMORY.GET reply missing id field: %q", s)
	}
	if !strings.Contains(s, "agent-1") {
		t.Fatalf("MEMORY.GET reply missing agent_id value: %q", s)
	}
	if !strings.Contains(s, "the user prefers dark mode") {
		t.Fatalf("MEMORY.GET reply missing content: %q", s)
	}
	if !strings.Contains(s, `"source":"chat"`) {
		t.Fatalf("MEMORY.GET reply missing metadata JSON: %q", s)
	}
	if !strings.Contains(s, "long_term") {
		t.Fatalf("MEMORY.GET reply missing default kind long_term: %q", s)
	}
	// 14 fields (7 pairs) in a flat array.
	if !strings.HasPrefix(s, "*14\r\n") {
		t.Fatalf("MEMORY.GET reply array header = %q, want *14 prefix", s)
	}
}

func TestMemoryReadWriteMetrics(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "the user prefers dark mode", "ID", "mem-1")
	execCommand(t, cs, "MEMORY.GET", "default", "mem-1")
	execCommand(t, cs, "MEMORY.SEARCH", "default", "dark mode preference")

	snap := cs.Deps.Metrics.Snapshot()
	if snap.MemoryWritesTotal != 1 {
		t.Fatalf("MemoryWritesTotal = %d, want 1", snap.MemoryWritesTotal)
	}
	if snap.MemoryReadsTotal != 2 {
		t.Fatalf("MemoryReadsTotal = %d, want 2 (1 GET + 1 SEARCH)", snap.MemoryReadsTotal)
	}
}

func TestMemoryPutGeneratesIDWhenOmitted(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.PUT", "agent-1", "some content")
	if !strings.HasPrefix(string(out), "$") {
		t.Fatalf("MEMORY.PUT reply = %q, want a bulk string id", out)
	}
	if strings.Contains(string(out), "$-1") {
		t.Fatalf("MEMORY.PUT should never return a null id, got %q", out)
	}
}

func TestMemoryPutBumpsVersionOnRepeatID(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "first content", "ID", "mem-1")
	out := execCommand(t, cs, "MEMORY.GET", "default", "mem-1")
	if !strings.Contains(string(out), "version\r\n$1\r\n1\r\n") {
		t.Fatalf("MEMORY.GET after first PUT should show version=1: %q", out)
	}

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "second content", "ID", "mem-1")
	out = execCommand(t, cs, "MEMORY.GET", "default", "mem-1")
	s := string(out)
	if !strings.Contains(s, "second content") {
		t.Fatalf("MEMORY.GET after second PUT should show replaced content: %q", s)
	}
	// version field's value bulk-string is "2" after the second Put.
	if !strings.Contains(s, "version\r\n$1\r\n2\r\n") {
		t.Fatalf("MEMORY.GET after second PUT should show version=2: %q", s)
	}
}

func TestMemoryGetMissingIDReturnsNilArray(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.GET", "default", "nope")
	want := "*-1\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.GET (missing id) reply = %q, want %q (nil array)", out, want)
	}
}

func TestMemoryPutInvalidMetadataJSON(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.PUT", "agent-1", "content", "METADATA", "not json")
	want := "-" + ErrInvalidMetadataJSONMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.PUT invalid metadata JSON reply = %q, want %q", out, want)
	}
}

func TestMemoryPutInvalidKind(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.PUT", "agent-1", "content", "KIND", "not_a_kind")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("MEMORY.PUT invalid KIND reply = %q, want a syntax error", out)
	}
}

func TestMemoryPutWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.PUT", "agent-1")
	want := "-" + ErrWrongNumberOfArgs("memory.put") + "\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.PUT wrong arity reply = %q, want %q", out, want)
	}
}

func TestMemoryGetWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.GET", "default")
	want := "-" + ErrWrongNumberOfArgs("memory.get") + "\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.GET wrong arity reply = %q, want %q", out, want)
	}
}

func TestMemorySearchWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.SEARCH", "default")
	want := "-" + ErrWrongNumberOfArgs("memory.search") + "\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.SEARCH wrong arity reply = %q, want %q", out, want)
	}
}

func TestMemorySearchSharedAcrossAgentsAndScopedByAgent(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-a", "database migration notes", "ID", "a1")
	execCommand(t, cs, "MEMORY.PUT", "agent-b", "database migration notes", "ID", "b1")

	// No AGENT filter: both show up.
	out := execCommand(t, cs, "MEMORY.SEARCH", "default", "database migration notes")
	s := string(out)
	if !strings.Contains(s, "a1") || !strings.Contains(s, "b1") {
		t.Fatalf("MEMORY.SEARCH without AGENT should find both agents' memories: %q", s)
	}

	// AGENT filter: only that agent's own memory.
	out = execCommand(t, cs, "MEMORY.SEARCH", "default", "database migration notes", "AGENT", "agent-a")
	want := "*1\r\n$2\r\na1\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.SEARCH AGENT=agent-a reply = %q, want %q", out, want)
	}
}

func TestMemorySearchKindFilter(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "onboarding notes", "ID", "long1", "KIND", "long_term")
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "onboarding notes", "ID", "epi1", "KIND", "episodic")

	out := execCommand(t, cs, "MEMORY.SEARCH", "default", "onboarding notes", "KIND", "episodic")
	want := "*1\r\n$4\r\nepi1\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.SEARCH KIND=episodic reply = %q, want %q", out, want)
	}
}

func TestMemorySearchKCapsResults(t *testing.T) {
	cs := newTestClientState(t)

	for i := 0; i < 5; i++ {
		execCommand(t, cs, "MEMORY.PUT", "agent-1", "topic note", "ID", "id"+strconv.Itoa(i))
	}

	out := execCommand(t, cs, "MEMORY.SEARCH", "default", "topic note", "K", "2")
	want := "*2\r\n"
	if !strings.HasPrefix(string(out), want) {
		t.Fatalf("MEMORY.SEARCH K=2 reply = %q, want prefix %q", out, want)
	}
}

func TestMemorySearchWithScores(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "hello world", "ID", "m1")

	out := execCommand(t, cs, "MEMORY.SEARCH", "default", "hello world", "WITHSCORES")
	want := "*2\r\n$2\r\nm1\r\n$1\r\n1\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.SEARCH WITHSCORES reply = %q, want %q", out, want)
	}
}

func TestMemorySearchNoMatchEmptyArray(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.SEARCH", "default", "anything")
	want := "*0\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.SEARCH (no memories) reply = %q, want %q", out, want)
	}
}

func TestMemorySearchThreshold(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "completely unrelated filler text", "ID", "far1")

	out := execCommand(t, cs, "MEMORY.SEARCH", "default", "hello world", "THRESHOLD", "0.99")
	want := "*0\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.SEARCH with a strict THRESHOLD reply = %q, want %q (nothing clears it)", out, want)
	}
}

func TestMemoryPutTTLExpires(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "ephemeral", "ID", "ttl1", "TTL", "1")
	out := execCommand(t, cs, "MEMORY.GET", "default", "ttl1")
	if strings.HasPrefix(string(out), "*-1") {
		t.Fatalf("MEMORY.GET (before TTL expiry) reply = %q, want the memory to still be present", out)
	}
}

func TestMemorySearchUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "MEMORY.SEARCH", "default", "query", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("MEMORY.SEARCH unknown option reply = %q, want a syntax error", out)
	}
}

func TestMemoryPutUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "MEMORY.PUT", "agent-1", "content", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("MEMORY.PUT unknown option reply = %q, want a syntax error", out)
	}
}
