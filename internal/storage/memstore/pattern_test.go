package memstore

import "testing"

// TestGlobMatch exercises every branch of globMatch/globMatchBytes,
// including the '[...]' character-class path (matchClass/indexByte) and
// the '\\' escape path -- neither had a single direct test before, despite
// backing KEYS/SCAN MATCH's real pattern-matching behavior.
func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "anything", true},
		{"*", "", true},
		{"h?llo", "hello", true},
		{"h?llo", "hllo", false},
		{"h[ae]llo", "hello", true},
		{"h[ae]llo", "hallo", true},
		{"h[ae]llo", "hillo", false},
		{"h[^ae]llo", "hillo", true},
		{"h[^ae]llo", "hello", false},
		{"[a-c]at", "bat", true},
		{"[a-c]at", "zat", false},
		{"user:[0-9]:name", "user:5:name", true},
		{"user:[0-9]:name", "user:x:name", false},
		// Unclosed '[' is treated as a literal '[' (malformed-class fallback).
		{"a[b", "a[b", true},
		{"a[b", "ab", false},
		// '\\' escapes the following character literally.
		{`a\*b`, "a*b", true},
		{`a\*b`, "axb", false},
		// A trailing lone backslash matches a literal backslash.
		{`a\`, `a\`, true},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}
