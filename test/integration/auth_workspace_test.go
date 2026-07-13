// This file drives Phase 7's real per-workspace AUTH/isolation enforcement
// end to end, over a real wire connection to a real server.Run instance --
// see AGENTS.md's "actually driven the real behavior over the wire" rule.
package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/server"
)

// startServerWithConfig is like startServer but lets the caller supply a
// full server.Config (e.g. WorkspaceCredentials) instead of the hardcoded
// MaxConnections-only default. cfg.Port/MCPPort's own listener binding is
// ignored -- like startServer, this always binds a fresh random RESP port
// via net.Listen(":0") and hands the listener straight to
// server.RunListener, so there's no bind-race.
func startServerWithConfig(t *testing.T, cfg server.Config) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.RunListener(ctx, cfg, ln); err != nil {
			t.Errorf("server.RunListener: %v", err)
		}
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shut down in time")
		}
	})

	return addr
}

// TestWorkspaceIsolationOverWire is Phase 7's real end-to-end case: a
// server started with --workspace-credentials-equivalent config
// (server.Config.WorkspaceCredentials), AUTH'd with one workspace's
// password over a real connection, confirming a command against that
// workspace succeeds and a command against a different workspace is
// rejected -- all over the real wire, not just in-process ClientState
// tests.
func TestWorkspaceIsolationOverWire(t *testing.T) {
	addr := startServerWithConfig(t, server.Config{
		MaxConnections: 1000,
		WorkspaceCredentials: []auth.Credential{
			{Workspace: "acme", Password: "pass1"},
			{Workspace: "other", Password: "pass2"},
		},
	})
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	// Before AUTH, any command (including one with no workspace concept at
	// all, like PING) is rejected NOAUTH -- multi-workspace mode requires
	// AUTH unconditionally (auth.Authenticator.Required()).
	if err := rdb.Do(ctx, "PING").Err(); err == nil {
		t.Fatal("PING before AUTH (multi-workspace mode) succeeded, want NOAUTH error")
	}

	if err := rdb.Do(ctx, "AUTH", "pass1").Err(); err != nil {
		t.Fatalf("AUTH pass1: %v", err)
	}

	// A command against this connection's own workspace (acme) succeeds.
	id, err := rdb.Do(ctx, "MEMORY.PUT", "agent-1", "acme's own note", "WORKSPACE", "acme").Text()
	if err != nil {
		t.Fatalf("MEMORY.PUT WORKSPACE acme (authed as acme): %v", err)
	}
	if id == "" {
		t.Fatal("MEMORY.PUT WORKSPACE acme returned an empty id")
	}
	fields, err := rdb.Do(ctx, "MEMORY.GET", "acme", id).Result()
	if err != nil {
		t.Fatalf("MEMORY.GET acme (authed as acme): %v", err)
	}
	if fields == nil {
		t.Fatal("MEMORY.GET acme (authed as acme) = nil, want the memory just written")
	}

	// A command against a different workspace (other) is rejected.
	if err := rdb.Do(ctx, "MEMORY.PUT", "agent-1", "should be rejected", "WORKSPACE", "other").Err(); err == nil {
		t.Fatal("MEMORY.PUT WORKSPACE other (authed as acme) succeeded, want NOPERM rejection")
	}
	if _, err := rdb.Do(ctx, "MEMORY.GET", "other", id).Result(); err == nil {
		t.Fatal("MEMORY.GET other (authed as acme) succeeded, want NOPERM rejection")
	}

	// Also verify VECTOR.* and GRAPH.* respect the same boundary, since
	// they enforce it independently (handlers_vector.go/handlers_graph.go).
	if err := rdb.Do(ctx, "VECTOR.UPSERT", "acme", "v1", "[1,0]").Err(); err != nil {
		t.Fatalf("VECTOR.UPSERT acme (authed as acme): %v", err)
	}
	if err := rdb.Do(ctx, "VECTOR.UPSERT", "other", "v1", "[1,0]").Err(); err == nil {
		t.Fatal("VECTOR.UPSERT other (authed as acme) succeeded, want NOPERM rejection")
	}
	if err := rdb.Do(ctx, "GRAPH.RELATED", "acme", "some-node").Err(); err != nil {
		t.Fatalf("GRAPH.RELATED acme (authed as acme): %v", err)
	}
	if err := rdb.Do(ctx, "GRAPH.RELATED", "other", "some-node").Err(); err == nil {
		t.Fatal("GRAPH.RELATED other (authed as acme) succeeded, want NOPERM rejection")
	}

	// Re-AUTHing with the other workspace's password switches which
	// workspace is authorized, symmetrically.
	if err := rdb.Do(ctx, "AUTH", "pass2").Err(); err != nil {
		t.Fatalf("AUTH pass2: %v", err)
	}
	if err := rdb.Do(ctx, "MEMORY.GET", "acme", id).Err(); err == nil {
		t.Fatal("MEMORY.GET acme (now authed as other) succeeded, want NOPERM rejection")
	}
	if err := rdb.Do(ctx, "MEMORY.PUT", "agent-1", "other's own note", "WORKSPACE", "other").Err(); err != nil {
		t.Fatalf("MEMORY.PUT WORKSPACE other (authed as other): %v", err)
	}
}

// TestWorkspaceCredentialsAndPasswordMutuallyExclusive confirms the startup
// error contract: configuring both Password and WorkspaceCredentials is a
// startup error, not silently one mode winning.
func TestWorkspaceCredentialsAndPasswordMutuallyExclusive(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	cfg := server.Config{
		Password:             "global-secret",
		WorkspaceCredentials: []auth.Credential{{Workspace: "acme", Password: "pass1"}},
	}
	err = server.RunListener(context.Background(), cfg, ln)
	if err == nil {
		t.Fatal("server.RunListener with both Password and WorkspaceCredentials set = nil error, want a startup error")
	}
}
