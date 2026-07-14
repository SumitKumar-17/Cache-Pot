package resp

import "testing"

// TestGlobMatchCharacterClass exercises the '[...]' branch of globMatch
// (including matchClass/indexByte, which nothing else in this package's
// test suite reaches) since PSUBSCRIBE patterns support Redis-style
// character classes, not just '*'/'?'.
func TestGlobMatchCharacterClass(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"h[ae]llo", "hello", true},
		{"h[ae]llo", "hallo", true},
		{"h[ae]llo", "hillo", false},
		{"h[^ae]llo", "hillo", true},
		{"h[^ae]llo", "hello", false},
		{"[a-c]at", "bat", true},
		{"[a-c]at", "zat", false},
		{"news.[0-9]", "news.5", true},
		{"news.[0-9]", "news.x", false},
		// Unclosed '[' is treated as a literal '[', matching Redis's own
		// leniency here (see globMatchBytes' "end < 0" branch).
		{"a[b", "a[b", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

// TestPSubscribeCharacterClassPattern proves the character-class branch
// works end to end through PSUBSCRIBE/PUBLISH, not just at the globMatch
// unit level.
func TestPSubscribeCharacterClassPattern(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "PSUBSCRIBE", "news.[0-9]")

	if n := cs.Deps.PubSub.Publish("news.5", []byte("payload")); n != 1 {
		t.Fatalf("Publish(news.5) delivered to %d subscribers, want 1", n)
	}
	if n := cs.Deps.PubSub.Publish("news.x", []byte("payload")); n != 0 {
		t.Fatalf("Publish(news.x) delivered to %d subscribers, want 0", n)
	}
}
