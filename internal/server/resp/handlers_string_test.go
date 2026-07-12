package resp

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/embed"
	"github.com/SumitKumar-17/cache-pot/internal/memory"
	"github.com/SumitKumar-17/cache-pot/internal/observability"
	"github.com/SumitKumar-17/cache-pot/internal/semantic"
	"github.com/SumitKumar-17/cache-pot/internal/storage/memstore"
	"github.com/SumitKumar-17/cache-pot/internal/toolcache"
	"github.com/SumitKumar-17/cache-pot/internal/vector"
)

func newTestDeps(t *testing.T) *Deps {
	t.Helper()
	engine := memstore.New(4)
	t.Cleanup(func() { _ = engine.Close() })
	registry := NewRegistry()
	RegisterAll(registry)
	return &Deps{
		Engine:        engine,
		Auth:          auth.New(""),
		Metrics:       observability.NewMetrics(),
		Logger:        observability.NewLogger(slog.LevelError),
		PubSub:        NewPubSub(),
		Registry:      registry,
		SemanticCache: semantic.New(embed.NewMock(8)),
		PromptCache:   semantic.NewPromptCache(),
		ToolCache:     toolcache.New(),
		VectorStore:   vector.New(),
		MemoryStore:   memory.New(embed.NewMock(8)),
	}
}

func newTestClientState(t *testing.T) *ClientState {
	t.Helper()
	deps := newTestDeps(t)
	return &ClientState{
		Deps:          deps,
		Writer:        NewWriter(&bytes.Buffer{}),
		Workspace:     defaultWorkspace,
		Authenticated: true,
	}
}

// execCommand runs args through cs's registry and returns the exact wire
// bytes the reply encodes to, so tests can assert on precise RESP framing.
func execCommand(t *testing.T, cs *ClientState, args ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf)
	reply := cs.Deps.Registry.Handle(cs, args)
	if reply != nil {
		if err := reply(w); err != nil {
			t.Fatalf("write reply: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return buf.Bytes()
}

func TestGetSetRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SET", "foo", "bar")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("SET reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "GET", "foo")
	if want := "$3\r\nbar\r\n"; string(out) != want {
		t.Fatalf("GET reply = %q, want %q", out, want)
	}
}

func TestGetMissingKey(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "GET", "nope")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("GET reply = %q, want %q", out, want)
	}
}

func TestGetWrongType(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HSET", "h", "f", "v")

	out := execCommand(t, cs, "GET", "h")
	want := "-" + ErrWrongTypeMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("GET on hash key = %q, want %q", out, want)
	}
}

func TestIncrDecr(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "INCR", "counter")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("INCR reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "INCRBY", "counter", "5")
	if want := ":6\r\n"; string(out) != want {
		t.Fatalf("INCRBY reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "DECR", "counter")
	if want := ":5\r\n"; string(out) != want {
		t.Fatalf("DECR reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "DECRBY", "counter", "2")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("DECRBY reply = %q, want %q", out, want)
	}
}

func TestIncrNotAnInteger(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "s", "notanumber")

	out := execCommand(t, cs, "INCR", "s")
	want := "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("INCR on non-integer = %q, want %q", out, want)
	}
}

func TestWrongNumberOfArguments(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "GET")
	want := "-ERR wrong number of arguments for 'get' command\r\n"
	if string(out) != want {
		t.Fatalf("GET with no key = %q, want %q", out, want)
	}
}

func TestMSetMGet(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "MSET", "a", "1", "b", "2")

	out := execCommand(t, cs, "MGET", "a", "b", "missing")
	want := "*3\r\n$1\r\n1\r\n$1\r\n2\r\n$-1\r\n"
	if string(out) != want {
		t.Fatalf("MGET reply = %q, want %q", out, want)
	}
}

func TestAppendStrlen(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "APPEND", "k", "Hello ")
	if want := ":6\r\n"; string(out) != want {
		t.Fatalf("APPEND (1) reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "APPEND", "k", "World")
	if want := ":11\r\n"; string(out) != want {
		t.Fatalf("APPEND (2) reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "STRLEN", "k")
	if want := ":11\r\n"; string(out) != want {
		t.Fatalf("STRLEN reply = %q, want %q", out, want)
	}
}

func TestSetNXThenXX(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SET", "k", "v1", "NX")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("SET NX (first) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SET", "k", "v2", "NX")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("SET NX (second, should fail) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "SET", "k", "v3", "XX", "GET")
	if want := "$2\r\nv1\r\n"; string(out) != want {
		t.Fatalf("SET XX GET = %q, want %q", out, want)
	}
}

func TestHDelWrongType(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "s", "v")

	out := execCommand(t, cs, "HDEL", "s", "f")
	want := "-" + ErrWrongTypeMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("HDEL on string key = %q, want %q", out, want)
	}
}
