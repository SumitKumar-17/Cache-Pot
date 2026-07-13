package resp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
)

// newTestClientStateWithAuth is newTestClientState's counterpart for tests
// that need a configured server password: it builds a *Deps with the given
// password (empty means auth disabled, matching newTestDeps' default) and a
// ClientState that starts unauthenticated whenever a password is required,
// mirroring NewClientState's real zero-value behavior.
func newTestClientStateWithAuth(t *testing.T, password string) *ClientState {
	t.Helper()
	deps := newTestDeps(t)
	deps.Auth = auth.New(password)
	return &ClientState{
		Deps:          deps,
		Writer:        NewWriter(&bytes.Buffer{}),
		Workspace:     defaultWorkspace,
		Authenticated: password == "",
	}
}

func TestPing(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "PING")
	if want := "+PONG\r\n"; string(out) != want {
		t.Fatalf("PING = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "PING", "hello")
	if want := "$5\r\nhello\r\n"; string(out) != want {
		t.Fatalf("PING hello = %q, want %q", out, want)
	}
}

func TestEcho(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "ECHO", "hi there")
	if want := "$8\r\nhi there\r\n"; string(out) != want {
		t.Fatalf("ECHO = %q, want %q", out, want)
	}
}

func TestEchoWrongArity(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "ECHO")
	want := "-" + ErrWrongNumberOfArgs("echo") + "\r\n"
	if string(out) != want {
		t.Fatalf("ECHO (no arg) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "ECHO", "a", "b")
	if string(out) != want {
		t.Fatalf("ECHO (2 args) = %q, want %q", out, want)
	}
}

func TestSelect(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SELECT", "0")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("SELECT 0 = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SELECT", "1")
	want := "-" + ErrWrongDBMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("SELECT 1 = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SELECT", "notanumber")
	want = "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("SELECT notanumber = %q, want %q", out, want)
	}
}

func TestHelloNoArgsAndVersion2(t *testing.T) {
	cs := newTestClientState(t)

	for _, args := range [][]string{{"HELLO"}, {"HELLO", "2"}} {
		out := execCommand(t, cs, args...)
		s := string(out)
		if !strings.HasPrefix(s, "*14\r\n") {
			t.Fatalf("%v reply = %q, want a *14 array", args, s)
		}
		if !strings.Contains(s, "cachepot") {
			t.Fatalf("%v reply missing server name: %q", args, s)
		}
	}
}

func TestHello3RejectedWithNoProto(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "HELLO", "3")
	want := "-" + ErrNoProtoMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("HELLO 3 = %q, want %q", out, want)
	}
}

func TestHelloUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "HELLO", "2", "FROB")
	want := "-" + ErrSyntaxMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("HELLO 2 FROB = %q, want %q", out, want)
	}
}

func TestHelloSetName(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HELLO", "2", "SETNAME", "myconn")
	if cs.Name != "myconn" {
		t.Fatalf("cs.Name after HELLO SETNAME = %q, want %q", cs.Name, "myconn")
	}
}

func TestHelloAuthOption(t *testing.T) {
	cs := newTestClientStateWithAuth(t, "secret")
	cs.Authenticated = false

	out := execCommand(t, cs, "HELLO", "2", "AUTH", "default", "wrong")
	want := "-" + ErrInvalidPasswordMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("HELLO AUTH (wrong password) = %q, want %q", out, want)
	}
	if cs.Authenticated {
		t.Fatalf("cs.Authenticated after a failed HELLO AUTH = true, want false")
	}

	out = execCommand(t, cs, "HELLO", "2", "AUTH", "default", "secret")
	if !strings.HasPrefix(string(out), "*14\r\n") {
		t.Fatalf("HELLO AUTH (correct password) = %q, want a *14 array", out)
	}
	if !cs.Authenticated {
		t.Fatalf("cs.Authenticated after a successful HELLO AUTH = false, want true")
	}
}

func TestAuthNoPasswordConfigured(t *testing.T) {
	cs := newTestClientState(t) // newTestDeps configures auth.New("")
	out := execCommand(t, cs, "AUTH", "anything")
	want := "-ERR Client sent AUTH, but no password is set. Did you mean AUTH <username> <password>?\r\n"
	if string(out) != want {
		t.Fatalf("AUTH with no password configured = %q, want %q", out, want)
	}
}

func TestAuthWithPasswordConfigured(t *testing.T) {
	cs := newTestClientStateWithAuth(t, "secret")
	cs.Authenticated = false

	out := execCommand(t, cs, "AUTH", "wrong")
	want := "-" + ErrInvalidPasswordMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("AUTH (wrong password) = %q, want %q", out, want)
	}
	if cs.Authenticated {
		t.Fatalf("cs.Authenticated after a failed AUTH = true, want false")
	}

	out = execCommand(t, cs, "AUTH", "secret")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("AUTH (correct password) = %q, want %q", out, want)
	}
	if !cs.Authenticated {
		t.Fatalf("cs.Authenticated after a successful AUTH = false, want true")
	}
}

func TestAuthUsernamePasswordForm(t *testing.T) {
	cs := newTestClientStateWithAuth(t, "secret")
	cs.Authenticated = false

	out := execCommand(t, cs, "AUTH", "default", "secret")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("AUTH default secret = %q, want %q", out, want)
	}
}

func TestUnauthenticatedClientRejectedExceptAllowedCommands(t *testing.T) {
	cs := newTestClientStateWithAuth(t, "secret")
	cs.Authenticated = false

	out := execCommand(t, cs, "GET", "k")
	want := "-" + ErrNoAuthMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("GET before AUTH = %q, want %q", out, want)
	}

	// PING is not on the AllowedNoAuth list, so it is rejected the same way.
	out = execCommand(t, cs, "PING")
	if string(out) != want {
		t.Fatalf("PING before AUTH = %q, want %q (PING is not AllowedNoAuth)", out, want)
	}

	// AUTH, HELLO, COMMAND, and INFO are on the AllowedNoAuth list, so they
	// work even before AUTH succeeds.
	if out := execCommand(t, cs, "COMMAND"); string(out) != "*0\r\n" {
		t.Fatalf("COMMAND before AUTH = %q, want *0 (AllowedNoAuth)", out)
	}
	if out := execCommand(t, cs, "INFO"); !strings.HasPrefix(string(out), "$") {
		t.Fatalf("INFO before AUTH = %q, want a bulk string (AllowedNoAuth)", out)
	}

	out = execCommand(t, cs, "AUTH", "secret")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("AUTH secret = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "GET", "k")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("GET after AUTH = %q, want %q", out, want)
	}
}

func TestClientGetNameSetName(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CLIENT", "GETNAME")
	if want := "$0\r\n\r\n"; string(out) != want {
		t.Fatalf("CLIENT GETNAME (default) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "CLIENT", "SETNAME", "myconn")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("CLIENT SETNAME = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "CLIENT", "GETNAME")
	if want := "$6\r\nmyconn\r\n"; string(out) != want {
		t.Fatalf("CLIENT GETNAME (after SETNAME) = %q, want %q", out, want)
	}
}

func TestClientUnknownSubcommand(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "CLIENT", "FROB")
	if !strings.HasPrefix(string(out), "-ERR Unknown subcommand") {
		t.Fatalf("CLIENT FROB = %q, want an unknown-subcommand error", out)
	}
}

func TestCommandCountAndBareCommand(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "COMMAND")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("COMMAND (bare) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "COMMAND", "COUNT")
	s := string(out)
	if !strings.HasPrefix(s, ":") {
		t.Fatalf("COMMAND COUNT = %q, want an integer reply", s)
	}
	if s == ":0\r\n" {
		t.Fatalf("COMMAND COUNT = %q, want a positive command count", s)
	}
}

func TestInfoBasicShape(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "INFO")
	s := string(out)
	if !strings.HasPrefix(s, "$") {
		t.Fatalf("INFO reply = %q, want a bulk string", s)
	}
	for _, want := range []string{"redis_version:", "cachepot_version:", "connected_clients:", "total_connections_received:", "total_commands_processed:"} {
		if !strings.Contains(s, want) {
			t.Fatalf("INFO reply missing %q: %q", want, s)
		}
	}
}

func TestQuitSetsFlagAndReturnsOK(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "QUIT")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("QUIT reply = %q, want %q", out, want)
	}
	if !cs.Quit {
		t.Fatalf("cs.Quit after QUIT = false, want true")
	}
}

func TestConnCommandsWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	cases := []struct {
		cmd  string
		args []string
	}{
		{"ping", []string{"PING", "a", "b"}},
		{"echo", []string{"ECHO"}},
		{"select", []string{"SELECT"}},
		{"auth", []string{"AUTH"}},
		{"client", []string{"CLIENT"}},
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
