package resp

import (
	"bytes"
	"testing"
)

func TestMultiQueuesAndExecRunsInOrder(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "MULTI")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("MULTI = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "SET", "k", "1")
	if want := "+QUEUED\r\n"; string(out) != want {
		t.Fatalf("SET while queuing = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "INCR", "k")
	if want := "+QUEUED\r\n"; string(out) != want {
		t.Fatalf("INCR while queuing = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "GET", "k")
	if want := "+QUEUED\r\n"; string(out) != want {
		t.Fatalf("GET while queuing = %q, want %q", out, want)
	}

	// Nothing should have executed yet.
	if cs.Deps.Engine.Exists(cs.Workspace, "k") != 0 {
		t.Fatalf("key 'k' exists before EXEC, want queued commands to not have run yet")
	}

	out = execCommand(t, cs, "EXEC")
	want := "*3\r\n+OK\r\n:2\r\n$1\r\n2\r\n"
	if string(out) != want {
		t.Fatalf("EXEC reply = %q, want %q", out, want)
	}

	if cs.InMulti {
		t.Fatalf("cs.InMulti after EXEC = true, want false")
	}
}

func TestDiscardCancelsQueueWithoutExecuting(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "MULTI")
	out := execCommand(t, cs, "SET", "k", "1")
	if want := "+QUEUED\r\n"; string(out) != want {
		t.Fatalf("SET while queuing = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "DISCARD")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("DISCARD = %q, want %q", out, want)
	}

	if cs.InMulti {
		t.Fatalf("cs.InMulti after DISCARD = true, want false")
	}
	out = execCommand(t, cs, "GET", "k")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("GET k after DISCARD = %q, want %q (queued SET must not have run)", out, want)
	}

	// The queue is really gone: EXEC right after DISCARD is "without MULTI".
	out = execCommand(t, cs, "EXEC")
	if want := "-ERR EXEC without MULTI\r\n"; string(out) != want {
		t.Fatalf("EXEC after DISCARD = %q, want %q", out, want)
	}
}

func TestExecWithoutMulti(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "EXEC")
	if want := "-ERR EXEC without MULTI\r\n"; string(out) != want {
		t.Fatalf("EXEC (no MULTI) = %q, want %q", out, want)
	}
}

func TestDiscardWithoutMulti(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "DISCARD")
	if want := "-ERR DISCARD without MULTI\r\n"; string(out) != want {
		t.Fatalf("DISCARD (no MULTI) = %q, want %q", out, want)
	}
}

func TestMultiNestedRejected(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MULTI")
	out := execCommand(t, cs, "MULTI")
	want := "-ERR MULTI calls can not be nested\r\n"
	if string(out) != want {
		t.Fatalf("nested MULTI = %q, want %q", out, want)
	}
}

func TestWatchInsideMultiRejected(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MULTI")
	out := execCommand(t, cs, "WATCH", "k")
	want := "-ERR WATCH inside MULTI is not allowed\r\n"
	if string(out) != want {
		t.Fatalf("WATCH inside MULTI = %q, want %q", out, want)
	}
}

func TestUnwatchClearsWatchedKeys(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "k", "v0")
	execCommand(t, cs, "WATCH", "k")

	out := execCommand(t, cs, "UNWATCH")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("UNWATCH = %q, want %q", out, want)
	}
	if cs.Watched != nil {
		t.Fatalf("cs.Watched after UNWATCH = %v, want nil", cs.Watched)
	}

	// Mutate the (now-unwatched) key, then confirm EXEC does not abort.
	execCommand(t, cs, "SET", "k", "changed-by-someone-else")
	execCommand(t, cs, "MULTI")
	execCommand(t, cs, "SET", "k", "v1")
	out = execCommand(t, cs, "EXEC")
	want := "*1\r\n+OK\r\n"
	if string(out) != want {
		t.Fatalf("EXEC after UNWATCH = %q, want %q (should run, not abort)", out, want)
	}
}

func TestWatchSucceedsWithoutExternalModification(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "k", "v0")

	out := execCommand(t, cs, "WATCH", "k")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("WATCH = %q, want %q", out, want)
	}

	execCommand(t, cs, "MULTI")
	execCommand(t, cs, "SET", "k", "v1")
	out = execCommand(t, cs, "EXEC")
	want := "*1\r\n+OK\r\n"
	if string(out) != want {
		t.Fatalf("EXEC with no external modification of a WATCHed key = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "GET", "k")
	if want := "$2\r\nv1\r\n"; string(out) != want {
		t.Fatalf("GET k after successful EXEC = %q, want %q", out, want)
	}
}

// TestWatchAbortModifiedByAnotherClientState exercises the same abort path
// test/integration's TestWatchAbort proves at the raw wire level, but at the
// handler level with two ClientStates sharing one Deps (no real sockets
// needed for MULTI/WATCH/EXEC, which is purely ClientState + shared Engine
// state) -- included here mainly as the direct setup for the handler-level
// cases below it that integration doesn't cover.
func TestWatchAbortModifiedByAnotherClientState(t *testing.T) {
	cs1 := newTestClientState(t)
	execCommand(t, cs1, "SET", "k", "v0")

	cs2 := &ClientState{Deps: cs1.Deps, Writer: NewWriter(&bytes.Buffer{}), Workspace: defaultWorkspace, Authenticated: true}

	execCommand(t, cs1, "WATCH", "k")
	execCommand(t, cs1, "MULTI")
	execCommand(t, cs1, "SET", "k", "v1")

	execCommand(t, cs2, "SET", "k", "from-cs2")

	out := execCommand(t, cs1, "EXEC")
	if want := "*-1\r\n"; string(out) != want {
		t.Fatalf("EXEC after a WATCHed key changed = %q, want %q (null array abort)", out, want)
	}
	out = execCommand(t, cs1, "GET", "k")
	if want := "$8\r\nfrom-cs2\r\n"; string(out) != want {
		t.Fatalf("GET k after aborted EXEC = %q, want %q (cs2's write, not the aborted SET)", out, want)
	}
}

func TestNestedArityErrorInsideMultiAbortsExec(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MULTI")

	out := execCommand(t, cs, "SET", "k", "1")
	if want := "+QUEUED\r\n"; string(out) != want {
		t.Fatalf("SET while queuing = %q, want %q", out, want)
	}

	// GET with no key argument fails arity *at queue time*, so it is
	// reported immediately (not queued as +QUEUED) and marks the
	// transaction for abort.
	out = execCommand(t, cs, "GET")
	want := "-" + ErrWrongNumberOfArgs("get") + "\r\n"
	if string(out) != want {
		t.Fatalf("arity-invalid command while queuing = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "EXEC")
	want = "-EXECABORT Transaction discarded because of previous errors.\r\n"
	if string(out) != want {
		t.Fatalf("EXEC after a queue-time arity error = %q, want %q", out, want)
	}

	// The aborted transaction must not have run the earlier, validly-queued
	// SET either.
	out = execCommand(t, cs, "GET", "k")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("GET k after EXECABORT = %q, want %q (nothing in the aborted transaction should have run)", out, want)
	}
}

func TestUnknownCommandInsideMultiAbortsExec(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MULTI")

	out := execCommand(t, cs, "NOSUCHCOMMAND")
	if !isUnknownCommandError(out) {
		t.Fatalf("unknown command while queuing = %q, want an unknown-command error", out)
	}

	out = execCommand(t, cs, "EXEC")
	want := "-EXECABORT Transaction discarded because of previous errors.\r\n"
	if string(out) != want {
		t.Fatalf("EXEC after an unknown queued command = %q, want %q", out, want)
	}
}

func isUnknownCommandError(out []byte) bool {
	s := string(out)
	return len(s) > 0 && s[0] == '-' && len(s) > 5 && s[1:4] == "ERR"
}
