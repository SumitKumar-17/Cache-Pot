package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
)

func TestParseWorkspaceCredentialsEmpty(t *testing.T) {
	creds, err := parseWorkspaceCredentials("")
	if err != nil {
		t.Fatalf("parseWorkspaceCredentials(\"\") error = %v, want nil", err)
	}
	if creds != nil {
		t.Fatalf("parseWorkspaceCredentials(\"\") = %v, want nil", creds)
	}
}

func TestParseWorkspaceCredentialsValid(t *testing.T) {
	creds, err := parseWorkspaceCredentials("acme:secret1,other:secret2")
	if err != nil {
		t.Fatalf("parseWorkspaceCredentials error = %v, want nil", err)
	}
	want := []auth.Credential{
		{Workspace: "acme", Password: "secret1"},
		{Workspace: "other", Password: "secret2"},
	}
	if len(creds) != len(want) {
		t.Fatalf("parseWorkspaceCredentials = %+v, want %+v", creds, want)
	}
	for i := range want {
		if creds[i] != want[i] {
			t.Fatalf("parseWorkspaceCredentials[%d] = %+v, want %+v", i, creds[i], want[i])
		}
	}
}

func TestParseWorkspaceCredentialsMalformed(t *testing.T) {
	cases := []string{
		"nopasswordfield",      // no ':' separator at all
		"acme:secret1,invalid", // second entry missing ':'
		":secret1",             // empty workspace
		"acme:",                // empty password
	}
	for _, s := range cases {
		if _, err := parseWorkspaceCredentials(s); err == nil {
			t.Errorf("parseWorkspaceCredentials(%q) error = nil, want a startup error", s)
		}
	}
}

func TestLoadDotEnvSetsUnsetVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "" +
		"# a comment, and a blank line follow\n" +
		"\n" +
		"OPENAI_API_KEY=\"sk-from-dotenv\"\n" +
		"OPENAI_API_BASE=https://example.test/v1\n" +
		"MALFORMED_LINE_NO_EQUALS\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env fixture: %v", err)
	}

	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("OPENAI_API_BASE")
	t.Cleanup(func() {
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("OPENAI_API_BASE")
	})

	loadDotEnv(path)

	if got := os.Getenv("OPENAI_API_KEY"); got != "sk-from-dotenv" {
		t.Fatalf("OPENAI_API_KEY = %q, want %q (quotes should be stripped)", got, "sk-from-dotenv")
	}
	if got := os.Getenv("OPENAI_API_BASE"); got != "https://example.test/v1" {
		t.Fatalf("OPENAI_API_BASE = %q, want %q", got, "https://example.test/v1")
	}
}

func TestLoadDotEnvDoesNotOverrideRealEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("OPENAI_API_KEY=from-dotenv\n"), 0o600); err != nil {
		t.Fatalf("write .env fixture: %v", err)
	}

	t.Setenv("OPENAI_API_KEY", "already-set-in-real-env")

	loadDotEnv(path)

	if got := os.Getenv("OPENAI_API_KEY"); got != "already-set-in-real-env" {
		t.Fatalf("OPENAI_API_KEY = %q, want the real env var to win over .env", got)
	}
}

func TestLoadDotEnvMissingFileIsSilentNoOp(t *testing.T) {
	// Must not panic or error; cachepotd runs fine with no .env present.
	loadDotEnv(filepath.Join(t.TempDir(), "does-not-exist.env"))
}
