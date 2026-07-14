package resp

import (
	"strconv"
	"strings"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
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

// TestMemoryCommandsUnrestrictedInSinglePasswordMode is the regression test
// proving the multi-workspace enforcement did NOT change today's
// default (single-password/no-auth) behavior: MEMORY.PUT/GET/SEARCH against
// a workspace other than "default" still work completely unrestricted.
func TestMemoryCommandsUnrestrictedInSinglePasswordMode(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.PUT", "agent-1", "cross-workspace note", "ID", "m1", "WORKSPACE", "some-other-workspace")
	if want := "$2\r\nm1\r\n"; string(out) != want {
		t.Fatalf("MEMORY.PUT (other workspace, single-password mode) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "MEMORY.GET", "some-other-workspace", "m1")
	if !strings.Contains(string(out), "cross-workspace note") {
		t.Fatalf("MEMORY.GET (other workspace, single-password mode) = %q, want it to succeed unrestricted", out)
	}

	out = execCommand(t, cs, "MEMORY.SEARCH", "some-other-workspace", "cross-workspace note")
	if !strings.HasPrefix(string(out), "*1\r\n") {
		t.Fatalf("MEMORY.SEARCH (other workspace, single-password mode) = %q, want a 1-result array", out)
	}
}

func TestMemoryHistoryRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "first content", "ID", "mem-1")
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "second content", "ID", "mem-1")

	out := execCommand(t, cs, "MEMORY.HISTORY", "default", "mem-1")
	s := string(out)
	if !strings.HasPrefix(s, "*2\r\n") {
		t.Fatalf("MEMORY.HISTORY reply = %q, want a 2-element outer array (2 versions)", s)
	}

	firstIdx := strings.Index(s, "first content")
	secondIdx := strings.Index(s, "second content")
	if firstIdx < 0 || secondIdx < 0 {
		t.Fatalf("MEMORY.HISTORY reply missing content: %q", s)
	}
	if firstIdx > secondIdx {
		t.Fatalf("MEMORY.HISTORY reply has versions out of order (want oldest first): %q", s)
	}
	if !strings.Contains(s, "version\r\n$1\r\n1\r\n") || !strings.Contains(s, "version\r\n$1\r\n2\r\n") {
		t.Fatalf("MEMORY.HISTORY reply missing version=1 and version=2: %q", s)
	}
}

func TestMemoryHistoryLimitCapsToMostRecent(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "v1", "ID", "mem-1")
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "v2", "ID", "mem-1")
	execCommand(t, cs, "MEMORY.PUT", "agent-1", "v3", "ID", "mem-1")

	out := execCommand(t, cs, "MEMORY.HISTORY", "default", "mem-1", "LIMIT", "2")
	s := string(out)
	if !strings.HasPrefix(s, "*2\r\n") {
		t.Fatalf("MEMORY.HISTORY LIMIT 2 reply = %q, want a 2-element outer array", s)
	}
	if strings.Contains(s, "$2\r\nv1\r\n") {
		t.Fatalf("MEMORY.HISTORY LIMIT 2 reply should have dropped the oldest version v1: %q", s)
	}
	v2Idx := strings.Index(s, "$2\r\nv2\r\n")
	v3Idx := strings.Index(s, "$2\r\nv3\r\n")
	if v2Idx < 0 || v3Idx < 0 {
		t.Fatalf("MEMORY.HISTORY LIMIT 2 reply missing v2/v3: %q", s)
	}
	if v2Idx > v3Idx {
		t.Fatalf("MEMORY.HISTORY LIMIT 2 reply has the 2 most recent versions out of order (want oldest-of-the-two first): %q", s)
	}
}

func TestMemoryHistoryUnknownIDNilArray(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.HISTORY", "default", "nope")
	want := "*-1\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.HISTORY (unknown id) reply = %q, want %q (nil array)", out, want)
	}
}

func TestMemoryHistoryWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.HISTORY", "default")
	want := "-" + ErrWrongNumberOfArgs("memory.history") + "\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.HISTORY wrong arity reply = %q, want %q", out, want)
	}
}

func TestMemoryHistoryNonNumericLimit(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "content", "ID", "mem-1")
	out := execCommand(t, cs, "MEMORY.HISTORY", "default", "mem-1", "LIMIT", "notanumber")
	want := "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.HISTORY non-numeric LIMIT reply = %q, want %q", out, want)
	}
}

// TestMemoryHistoryUnrestrictedInSinglePasswordMode is the regression test
// proving the multi-workspace enforcement did NOT change today's
// default (single-password/no-auth) behavior for MEMORY.HISTORY, mirroring
// TestMemoryCommandsUnrestrictedInSinglePasswordMode.
func TestMemoryHistoryUnrestrictedInSinglePasswordMode(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MEMORY.PUT", "agent-1", "cross-workspace note", "ID", "m1", "WORKSPACE", "some-other-workspace")
	if want := "$2\r\nm1\r\n"; string(out) != want {
		t.Fatalf("MEMORY.PUT (other workspace, single-password mode) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "MEMORY.HISTORY", "some-other-workspace", "m1")
	if !strings.Contains(string(out), "cross-workspace note") {
		t.Fatalf("MEMORY.HISTORY (other workspace, single-password mode) = %q, want it to succeed unrestricted", out)
	}
}

// TestMemoryHistoryMultiWorkspaceIsolation mirrors
// TestMemoryCommandsMultiWorkspaceIsolation for MEMORY.HISTORY: a connection
// authenticated for workspace "acme" gets NOPERM against workspace "other".
func TestMemoryHistoryMultiWorkspaceIsolation(t *testing.T) {
	cs := newTestClientStateWithMultiWorkspaceAuth(t,
		auth.Credential{Workspace: "acme", Password: "pass1"},
		auth.Credential{Workspace: "other", Password: "pass2"},
	)
	execCommand(t, cs, "AUTH", "pass1")

	out := execCommand(t, cs, "MEMORY.HISTORY", "other", "m1")
	want := "-" + ErrWorkspaceNotAuthorized("other") + "\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.HISTORY other workspace (authed as acme) = %q, want %q", out, want)
	}

	execCommand(t, cs, "MEMORY.PUT", "agent-1", "note", "ID", "m1", "WORKSPACE", "acme")
	out = execCommand(t, cs, "MEMORY.HISTORY", "acme", "m1")
	if !strings.Contains(string(out), "note") {
		t.Fatalf("MEMORY.HISTORY own workspace (acme) = %q, want it to succeed", out)
	}
}

// TestMemoryCommandsMultiWorkspaceIsolation is the actual isolation
// test: a connection authenticated for workspace "acme" gets a real
// NOPERM-style rejection when it tries to use workspace "other", and
// succeeds when it uses its own workspace "acme".
func TestMemoryCommandsMultiWorkspaceIsolation(t *testing.T) {
	cs := newTestClientStateWithMultiWorkspaceAuth(t,
		auth.Credential{Workspace: "acme", Password: "pass1"},
		auth.Credential{Workspace: "other", Password: "pass2"},
	)
	execCommand(t, cs, "AUTH", "pass1")

	out := execCommand(t, cs, "MEMORY.PUT", "agent-1", "note", "ID", "m1", "WORKSPACE", "other")
	want := "-" + ErrWorkspaceNotAuthorized("other") + "\r\n"
	if string(out) != want {
		t.Fatalf("MEMORY.PUT other workspace (authed as acme) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "MEMORY.GET", "other", "m1")
	if string(out) != want {
		t.Fatalf("MEMORY.GET other workspace (authed as acme) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "MEMORY.SEARCH", "other", "note")
	if string(out) != want {
		t.Fatalf("MEMORY.SEARCH other workspace (authed as acme) = %q, want %q", out, want)
	}

	// Its own workspace works fine.
	out = execCommand(t, cs, "MEMORY.PUT", "agent-1", "note", "ID", "m1", "WORKSPACE", "acme")
	if want := "$2\r\nm1\r\n"; string(out) != want {
		t.Fatalf("MEMORY.PUT own workspace (acme) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "MEMORY.GET", "acme", "m1")
	if !strings.Contains(string(out), "note") {
		t.Fatalf("MEMORY.GET own workspace (acme) = %q, want it to succeed", out)
	}
	out = execCommand(t, cs, "MEMORY.SEARCH", "acme", "note")
	if !strings.HasPrefix(string(out), "*1\r\n") {
		t.Fatalf("MEMORY.SEARCH own workspace (acme) = %q, want a 1-result array", out)
	}
}
