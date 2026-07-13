package auth

import "testing"

func TestRequired(t *testing.T) {
	if New("").Required() {
		t.Fatal("Required() = true for an empty password, want false")
	}
	if !New("secret").Required() {
		t.Fatal("Required() = false for a configured password, want true")
	}
}

func TestCheckNoPasswordConfiguredAlwaysTrue(t *testing.T) {
	a := New("")
	// Matches Redis's own no-requirepass behavior: with no password
	// configured, any connection is already usable without AUTH, so Check
	// never rejects (even against an empty supplied password).
	if !a.Check("anything") {
		t.Fatal("Check(\"anything\") with no password configured = false, want true")
	}
	if !a.Check("") {
		t.Fatal("Check(\"\") with no password configured = false, want true")
	}
}

func TestCheckWithPasswordConfigured(t *testing.T) {
	a := New("secret")
	if !a.Check("secret") {
		t.Fatal("Check(correct password) = false, want true")
	}
	if a.Check("wrong") {
		t.Fatal("Check(wrong password) = true, want false")
	}
	if a.Check("") {
		t.Fatal("Check(\"\") with a password configured = true, want false")
	}
}
