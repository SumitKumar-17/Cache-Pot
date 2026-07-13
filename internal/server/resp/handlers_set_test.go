package resp

import (
	"sort"
	"strings"
	"testing"
)

// sortedMemberStrings decodes a RESP2 array-of-bulk-strings reply and
// returns its elements sorted, so tests can assert on set membership
// without depending on the (unordered) iteration order sets are returned in.
func sortedMemberStrings(t *testing.T, out []byte) []string {
	t.Helper()
	s := string(out)
	if !strings.HasPrefix(s, "*") {
		t.Fatalf("expected an array reply, got %q", s)
	}
	parts := strings.Split(strings.TrimSuffix(s, "\r\n"), "\r\n")
	var members []string
	// parts[0] is the array header ("*N"); thereafter pairs of ("$len","payload").
	for i := 1; i+1 < len(parts); i += 2 {
		members = append(members, parts[i+1])
	}
	sort.Strings(members)
	return members
}

func TestSAddSRemSCard(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SADD", "s", "a", "b", "c")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("SADD (new set) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SADD", "s", "a", "d")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("SADD (1 new + 1 dup) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SCARD", "s")
	if want := ":4\r\n"; string(out) != want {
		t.Fatalf("SCARD = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "SREM", "s", "a", "nope")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("SREM (1 existing + 1 missing) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SCARD", "s")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("SCARD after SREM = %q, want %q", out, want)
	}
}

func TestSMembersRoundTrip(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SADD", "s", "a", "b", "c")

	out := execCommand(t, cs, "SMEMBERS", "s")
	got := sortedMemberStrings(t, out)
	want := []string{"a", "b", "c"}
	if !equalStrings(got, want) {
		t.Fatalf("SMEMBERS = %v, want %v", got, want)
	}
}

func TestSMembersMissingKeyEmptyArray(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "SMEMBERS", "nokey")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("SMEMBERS missing key = %q, want %q", out, want)
	}
}

func TestSIsMember(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SADD", "s", "a")

	out := execCommand(t, cs, "SISMEMBER", "s", "a")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("SISMEMBER (present) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SISMEMBER", "s", "nope")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("SISMEMBER (absent) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SISMEMBER", "nokey", "a")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("SISMEMBER (missing key) = %q, want %q", out, want)
	}
}

func TestSInterSUnionSDiffSetTheory(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SADD", "s1", "a", "b", "c")
	execCommand(t, cs, "SADD", "s2", "b", "c", "d")

	inter := sortedMemberStrings(t, execCommand(t, cs, "SINTER", "s1", "s2"))
	if !equalStrings(inter, []string{"b", "c"}) {
		t.Fatalf("SINTER s1 s2 = %v, want [b c]", inter)
	}

	union := sortedMemberStrings(t, execCommand(t, cs, "SUNION", "s1", "s2"))
	if !equalStrings(union, []string{"a", "b", "c", "d"}) {
		t.Fatalf("SUNION s1 s2 = %v, want [a b c d]", union)
	}

	diff12 := sortedMemberStrings(t, execCommand(t, cs, "SDIFF", "s1", "s2"))
	if !equalStrings(diff12, []string{"a"}) {
		t.Fatalf("SDIFF s1 s2 = %v, want [a]", diff12)
	}

	diff21 := sortedMemberStrings(t, execCommand(t, cs, "SDIFF", "s2", "s1"))
	if !equalStrings(diff21, []string{"d"}) {
		t.Fatalf("SDIFF s2 s1 = %v, want [d]", diff21)
	}
}

func TestSInterDisjointSetsEmpty(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SADD", "s1", "a", "b")
	execCommand(t, cs, "SADD", "s2", "c", "d")

	out := execCommand(t, cs, "SINTER", "s1", "s2")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("SINTER on disjoint sets = %q, want %q", out, want)
	}
}

func TestSInterWithMissingKeyIsEmptySet(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SADD", "s1", "a", "b")

	// s1 INTER a-missing-key: the missing key contributes an empty set, so
	// the whole intersection must be empty.
	out := execCommand(t, cs, "SINTER", "s1", "nokey")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("SINTER with a missing key = %q, want %q", out, want)
	}

	union := sortedMemberStrings(t, execCommand(t, cs, "SUNION", "s1", "nokey"))
	if !equalStrings(union, []string{"a", "b"}) {
		t.Fatalf("SUNION with a missing key = %v, want [a b]", union)
	}
}

func TestSetCommandsWrongType(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "str", "v")

	cases := []struct {
		name string
		args []string
	}{
		{"SADD", []string{"SADD", "str", "x"}},
		{"SREM", []string{"SREM", "str", "x"}},
		{"SMEMBERS", []string{"SMEMBERS", "str"}},
		{"SISMEMBER", []string{"SISMEMBER", "str", "x"}},
		{"SCARD", []string{"SCARD", "str"}},
		{"SINTER", []string{"SINTER", "str"}},
		{"SUNION", []string{"SUNION", "str"}},
		{"SDIFF", []string{"SDIFF", "str"}},
	}
	want := "-" + ErrWrongTypeMsg + "\r\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := execCommand(t, cs, tc.args...)
			if string(out) != want {
				t.Fatalf("%s on string key = %q, want %q", tc.name, out, want)
			}
		})
	}
}

func TestSetCommandsWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	cases := []struct {
		cmd  string
		args []string
	}{
		{"sadd", []string{"SADD", "s"}},
		{"srem", []string{"SREM", "s"}},
		{"smembers", []string{"SMEMBERS"}},
		{"sismember", []string{"SISMEMBER", "s"}},
		{"scard", []string{"SCARD"}},
		{"sinter", []string{"SINTER"}},
		{"sunion", []string{"SUNION"}},
		{"sdiff", []string{"SDIFF"}},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			out := execCommand(t, cs, tc.args...)
			want := "-" + ErrWrongNumberOfArgs(tc.cmd) + "\r\n"
			if string(out) != want {
				t.Fatalf("%v reply = %q, want %q", tc.args, out, want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
