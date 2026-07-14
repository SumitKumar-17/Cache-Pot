package resp

import "testing"

// TestRegistryLookup exercises Registry.Lookup directly. Handle (via
// execCommand in every other handler test) never calls Lookup itself -- it
// indexes r.commands directly -- so Lookup had no coverage anywhere despite
// being a real, reasonable piece of Registry's public API (the natural
// read-only counterpart to Register).
func TestRegistryLookup(t *testing.T) {
	r := NewRegistry()
	cmd := &Command{Name: "PING", MinArgs: 1, MaxArgs: 2, Handler: handlePing}
	r.Register(cmd)

	got, ok := r.Lookup("PING")
	if !ok || got != cmd {
		t.Fatalf("Lookup(%q) = (%v, %v), want (%v, true)", "PING", got, ok, cmd)
	}

	// Case-insensitive, matching Register's own case-insensitive storage.
	if _, ok := r.Lookup("ping"); !ok {
		t.Fatalf("Lookup(%q) ok = false, want true (case-insensitive)", "ping")
	}

	if _, ok := r.Lookup("NOSUCHCOMMAND"); ok {
		t.Fatalf("Lookup(%q) ok = true, want false", "NOSUCHCOMMAND")
	}
}
