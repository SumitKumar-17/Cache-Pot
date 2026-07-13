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

// TestSinglePasswordModeUnaffectedByMultiWorkspaceExisting re-proves
// single-password mode's behavior end to end (not just by inspection) now
// that Authenticator supports a second mode, so a regression in one mode
// can't silently leak into the other.
func TestSinglePasswordModeUnaffectedByMultiWorkspaceExisting(t *testing.T) {
	a := New("secret")
	if a.MultiWorkspace() {
		t.Fatal("MultiWorkspace() = true for an Authenticator built via New, want false")
	}
	if ws, ok := a.WorkspaceForPassword("secret"); ok || ws != "" {
		t.Fatalf("WorkspaceForPassword in single-password mode = (%q, %v), want (\"\", false)", ws, ok)
	}
	if !a.Required() {
		t.Fatal("Required() = false with a configured password, want true")
	}
	if !a.Check("secret") || a.Check("wrong") {
		t.Fatal("Check behavior changed for single-password mode")
	}
}

func TestNewMultiWorkspaceRequiredAlwaysTrue(t *testing.T) {
	a := NewMultiWorkspace(Credential{Workspace: "acme", Password: "pass1"})
	if !a.Required() {
		t.Fatal("Required() = false in multi-workspace mode, want true")
	}
	// Even with zero credentials configured, multi-workspace mode still
	// requires AUTH -- there is no unauthenticated default workspace.
	empty := NewMultiWorkspace()
	if !empty.Required() {
		t.Fatal("Required() = false for an empty multi-workspace Authenticator, want true")
	}
}

func TestNewMultiWorkspaceIsMultiWorkspace(t *testing.T) {
	a := NewMultiWorkspace(Credential{Workspace: "acme", Password: "pass1"})
	if !a.MultiWorkspace() {
		t.Fatal("MultiWorkspace() = false for an Authenticator built via NewMultiWorkspace, want true")
	}
	if New("secret").MultiWorkspace() {
		t.Fatal("MultiWorkspace() = true for an Authenticator built via New, want false")
	}
}

func TestWorkspaceForPasswordSuccessAndFailure(t *testing.T) {
	a := NewMultiWorkspace(
		Credential{Workspace: "acme", Password: "pass1"},
		Credential{Workspace: "other", Password: "pass2"},
	)

	if ws, ok := a.WorkspaceForPassword("pass1"); !ok || ws != "acme" {
		t.Fatalf("WorkspaceForPassword(pass1) = (%q, %v), want (\"acme\", true)", ws, ok)
	}
	if ws, ok := a.WorkspaceForPassword("pass2"); !ok || ws != "other" {
		t.Fatalf("WorkspaceForPassword(pass2) = (%q, %v), want (\"other\", true)", ws, ok)
	}
	if ws, ok := a.WorkspaceForPassword("nope"); ok || ws != "" {
		t.Fatalf("WorkspaceForPassword(nope) = (%q, %v), want (\"\", false)", ws, ok)
	}
}

func TestMultiWorkspaceCheckMatchesAnyCredential(t *testing.T) {
	a := NewMultiWorkspace(
		Credential{Workspace: "acme", Password: "pass1"},
		Credential{Workspace: "other", Password: "pass2"},
	)
	if !a.Check("pass1") || !a.Check("pass2") {
		t.Fatal("Check should succeed for any configured credential's password")
	}
	if a.Check("wrong") {
		t.Fatal("Check should fail for a password matching no credential")
	}
}

func TestNewMultiWorkspaceDuplicatePasswordLastWins(t *testing.T) {
	// Two different workspaces configured with the same password is
	// ambiguous; NewMultiWorkspace resolves it deterministically by
	// last-one-wins rather than erroring (see its doc comment).
	a := NewMultiWorkspace(
		Credential{Workspace: "first", Password: "shared"},
		Credential{Workspace: "second", Password: "shared"},
	)
	ws, ok := a.WorkspaceForPassword("shared")
	if !ok || ws != "second" {
		t.Fatalf("WorkspaceForPassword(shared) = (%q, %v), want (\"second\", true) [last-one-wins]", ws, ok)
	}
}
