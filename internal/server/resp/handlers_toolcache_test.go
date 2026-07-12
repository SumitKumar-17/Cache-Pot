package resp

import (
	"strconv"
	"strings"
	"testing"
)

func TestToolCacheSetGetRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "TOOL.CACHE", "SET", "github.get_issue", `{"repo":"foo","number":42}`, `{"title":"a bug"}`)
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("TOOL.CACHE SET reply = %q, want %q", out, want)
	}

	// Same tool/args (different JSON key order): hit.
	out = execCommand(t, cs, "TOOL.CACHE", "GET", "github.get_issue", `{"number":42,"repo":"foo"}`)
	want := "$" + strconv.Itoa(len(`{"title":"a bug"}`)) + "\r\n" + `{"title":"a bug"}` + "\r\n"
	if string(out) != want {
		t.Fatalf("TOOL.CACHE GET (hit, reordered JSON keys) reply = %q, want %q", out, want)
	}

	// Different args: miss.
	out = execCommand(t, cs, "TOOL.CACHE", "GET", "github.get_issue", `{"repo":"foo","number":43}`)
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("TOOL.CACHE GET (different args) reply = %q, want %q", out, want)
	}

	// Different tool name, same args: miss.
	out = execCommand(t, cs, "TOOL.CACHE", "GET", "jira.get_issue", `{"repo":"foo","number":42}`)
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("TOOL.CACHE GET (different tool) reply = %q, want %q", out, want)
	}
}

func TestToolCacheGetEmptyCacheMiss(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "TOOL.CACHE", "GET", "github.get_issue", `{}`)
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("TOOL.CACHE GET (empty cache) reply = %q, want %q", out, want)
	}
}

func TestToolCacheInvalidJSON(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "TOOL.CACHE", "SET", "github.get_issue", "not json", "result")
	want := "-" + ErrInvalidToolArgsJSONMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("TOOL.CACHE SET invalid JSON = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "TOOL.CACHE", "GET", "github.get_issue", "not json")
	if string(out) != want {
		t.Fatalf("TOOL.CACHE GET invalid JSON = %q, want %q", out, want)
	}
}

func TestToolCacheWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "TOOL.CACHE", "SET", "github.get_issue", "{}")
	want := "-" + ErrWrongNumberOfArgs("tool.cache") + "\r\n"
	if string(out) != want {
		t.Fatalf("TOOL.CACHE SET wrong arity = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "TOOL.CACHE", "GET", "github.get_issue")
	want = "-" + ErrWrongNumberOfArgs("tool.cache") + "\r\n"
	if string(out) != want {
		t.Fatalf("TOOL.CACHE GET wrong arity = %q, want %q", out, want)
	}
}

func TestToolCacheUnknownSubcommand(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "TOOL.CACHE", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR") {
		t.Fatalf("TOOL.CACHE unknown subcommand reply = %q, want a RESP error", out)
	}
}

func TestToolCacheTTLExpires(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "TOOL.CACHE", "SET", "github.get_issue", `{"n":1}`, "result", "TTL", "1")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("TOOL.CACHE SET with TTL reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "TOOL.CACHE", "GET", "github.get_issue", `{"n":1}`)
	if want := "$6\r\nresult\r\n"; string(out) != want {
		t.Fatalf("TOOL.CACHE GET (before TTL expiry) reply = %q, want %q", out, want)
	}
}

