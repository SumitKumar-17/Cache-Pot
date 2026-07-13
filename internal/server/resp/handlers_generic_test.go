package resp

import (
	"bufio"
	"bytes"
	"io"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// respVal is a minimal parsed RESP2 value, used by tests (here and in
// handlers_pubsub_test.go) that need to inspect a nested reply -- e.g.
// SCAN's [cursor, keys] array or a pub/sub push -- rather than match exact
// wire bytes as execCommand-based tests do.
type respVal struct {
	kind  byte
	str   string
	items []respVal
}

func readRESPValue(t *testing.T, r *bufio.Reader) respVal {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("readRESPValue: read line: %v", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		t.Fatalf("readRESPValue: empty line")
	}
	switch line[0] {
	case '+', '-', ':':
		return respVal{kind: line[0], str: line[1:]}
	case '$':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			t.Fatalf("readRESPValue: bad bulk header %q: %v", line, err)
		}
		if n < 0 {
			return respVal{kind: '$', str: ""}
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			t.Fatalf("readRESPValue: read bulk payload: %v", err)
		}
		return respVal{kind: '$', str: string(buf[:n])}
	case '*':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			t.Fatalf("readRESPValue: bad array header %q: %v", line, err)
		}
		if n < 0 {
			return respVal{kind: '*'}
		}
		items := make([]respVal, n)
		for i := 0; i < n; i++ {
			items[i] = readRESPValue(t, r)
		}
		return respVal{kind: '*', items: items}
	default:
		t.Fatalf("readRESPValue: unexpected type byte in %q", line)
		return respVal{}
	}
}

// parseRESP parses a single already-received reply (as returned by
// execCommand) into a respVal, for handlers whose reply shape is easier to
// walk than to hand-encode byte-for-byte (SCAN's nested array).
func parseRESP(t *testing.T, out []byte) respVal {
	t.Helper()
	return readRESPValue(t, bufio.NewReader(bytes.NewReader(out)))
}

// parseInt parses a RESP2 integer reply (":<n>\r\n") and returns n, for
// assertions that need to tolerate a value (e.g. PTTL's real-time
// countdown) rather than match one exact wire-format string.
func parseInt(t *testing.T, out []byte) int {
	t.Helper()
	v := parseRESP(t, out)
	if v.kind != ':' {
		t.Fatalf("parseInt: reply = %q, want an integer reply", out)
	}
	n, err := strconv.Atoi(v.str)
	if err != nil {
		t.Fatalf("parseInt: %q is not an integer: %v", v.str, err)
	}
	return n
}

func TestDelExistsMultiKey(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "a", "1")
	execCommand(t, cs, "SET", "b", "2")

	out := execCommand(t, cs, "EXISTS", "a", "b", "nope", "a")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("EXISTS (a,b,nope,a) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "DEL", "a", "b", "nope")
	if want := ":2\r\n"; string(out) != want {
		t.Fatalf("DEL (a,b,nope) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "EXISTS", "a", "b")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("EXISTS after DEL = %q, want %q", out, want)
	}
}

func TestExpireThenTTL(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "k", "v")

	out := execCommand(t, cs, "EXPIRE", "k", "100")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("EXPIRE (existing key) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "TTL", "k")
	if want := ":100\r\n"; string(out) != want {
		t.Fatalf("TTL after EXPIRE 100 = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "EXPIRE", "nokey", "100")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("EXPIRE (missing key) = %q, want %q", out, want)
	}
}

func TestPExpireThenPTTL(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "k", "v")

	out := execCommand(t, cs, "PEXPIRE", "k", "100000")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("PEXPIRE = %q, want %q", out, want)
	}
	// PTTL counts down in real time from the moment PEXPIRE ran, so allow a
	// small amount of drift rather than asserting an exact millisecond value.
	out = execCommand(t, cs, "PTTL", "k")
	ms := parseInt(t, out)
	if ms <= 99000 || ms > 100000 {
		t.Fatalf("PTTL after PEXPIRE 100000 = %q, want a value just at or under 100000", out)
	}
}

func TestTTLNoExpiryAndMissingKey(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "k", "v")

	out := execCommand(t, cs, "TTL", "k")
	if want := ":-1\r\n"; string(out) != want {
		t.Fatalf("TTL (no expiry set) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "TTL", "nokey")
	if want := ":-2\r\n"; string(out) != want {
		t.Fatalf("TTL (missing key) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "PTTL", "nokey")
	if want := ":-2\r\n"; string(out) != want {
		t.Fatalf("PTTL (missing key) = %q, want %q", out, want)
	}
}

func TestExpireZeroSecondsDeletesKey(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "k", "v")

	out := execCommand(t, cs, "EXPIRE", "k", "0")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("EXPIRE k 0 = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "EXISTS", "k")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("EXISTS after EXPIRE 0 = %q, want %q", out, want)
	}
}

func TestPersistRemovesExpiry(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "k", "v")
	execCommand(t, cs, "EXPIRE", "k", "100")

	out := execCommand(t, cs, "PERSIST", "k")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("PERSIST (had expiry) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "TTL", "k")
	if want := ":-1\r\n"; string(out) != want {
		t.Fatalf("TTL after PERSIST = %q, want %q", out, want)
	}

	// A second PERSIST (no expiry left to remove) reports no-op.
	out = execCommand(t, cs, "PERSIST", "k")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("PERSIST (no expiry left) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "PERSIST", "nokey")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("PERSIST (missing key) = %q, want %q", out, want)
	}
}

func TestTypeAcrossKinds(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "str", "v")
	execCommand(t, cs, "HSET", "h", "f", "v")
	execCommand(t, cs, "RPUSH", "l", "v")
	execCommand(t, cs, "SADD", "s", "v")
	execCommand(t, cs, "ZADD", "z", "1", "v")

	cases := map[string]string{
		"str": "string",
		"h":   "hash",
		"l":   "list",
		"s":   "set",
		"z":   "zset",
	}
	for key, kind := range cases {
		out := execCommand(t, cs, "TYPE", key)
		want := "+" + kind + "\r\n"
		if string(out) != want {
			t.Fatalf("TYPE %s = %q, want %q", key, out, want)
		}
	}
	out := execCommand(t, cs, "TYPE", "nokey")
	if want := "+none\r\n"; string(out) != want {
		t.Fatalf("TYPE (missing key) = %q, want %q", out, want)
	}
}

func TestKeysGlobPatternMatchesSubset(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "user:1", "a")
	execCommand(t, cs, "SET", "user:2", "b")
	execCommand(t, cs, "SET", "order:1", "c")

	out := execCommand(t, cs, "KEYS", "user:*")
	want := "*2\r\n$6\r\nuser:1\r\n$6\r\nuser:2\r\n"
	if string(out) != want {
		t.Fatalf("KEYS user:* = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "KEYS", "order:*")
	want = "*1\r\n$7\r\norder:1\r\n"
	if string(out) != want {
		t.Fatalf("KEYS order:* = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "KEYS", "nomatch:*")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("KEYS nomatch:* = %q, want %q", out, want)
	}
}

func TestScanCursorEventuallyTerminates(t *testing.T) {
	cs := newTestClientState(t)
	const n = 25
	var inserted []string
	for i := 0; i < n; i++ {
		key := "k" + strconv.Itoa(i)
		inserted = append(inserted, key)
		execCommand(t, cs, "SET", key, "v")
	}
	sort.Strings(inserted)

	seen := make(map[string]bool)
	cursor := "0"
	iterations := 0
	for {
		iterations++
		if iterations > n+2 {
			t.Fatalf("SCAN did not terminate within a reasonable number of calls")
		}
		out := execCommand(t, cs, "SCAN", cursor, "COUNT", "5")
		val := parseRESP(t, out)
		if val.kind != '*' || len(val.items) != 2 {
			t.Fatalf("SCAN reply shape = %+v, want a 2-element array", val)
		}
		cursor = val.items[0].str
		keysArr := val.items[1]
		if keysArr.kind != '*' {
			t.Fatalf("SCAN keys element kind = %q, want array", keysArr.kind)
		}
		for _, k := range keysArr.items {
			seen[k.str] = true
		}
		if cursor == "0" {
			break
		}
	}

	if len(seen) != n {
		t.Fatalf("SCAN visited %d distinct keys, want %d (seen=%v)", len(seen), n, seen)
	}
	for _, k := range inserted {
		if !seen[k] {
			t.Fatalf("SCAN never returned key %q", k)
		}
	}
}

func TestScanMatchOption(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "user:1", "a")
	execCommand(t, cs, "SET", "order:1", "b")

	out := execCommand(t, cs, "SCAN", "0", "MATCH", "user:*", "COUNT", "100")
	val := parseRESP(t, out)
	if val.items[0].str != "0" {
		t.Fatalf("SCAN with COUNT>=keyspace should return cursor 0 in one call, got %q", val.items[0].str)
	}
	keys := val.items[1].items
	if len(keys) != 1 || keys[0].str != "user:1" {
		t.Fatalf("SCAN MATCH user:* = %v, want [user:1]", keys)
	}
}

func TestScanInvalidArgs(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SCAN", "notanumber")
	want := "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("SCAN with a non-integer cursor = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "SCAN", "0", "MATCH")
	want = "-" + ErrSyntaxMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("SCAN MATCH with no value = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "SCAN", "0", "COUNT", "notanumber")
	want = "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("SCAN COUNT with a non-integer value = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "SCAN", "0", "FROB", "x")
	want = "-" + ErrSyntaxMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("SCAN with an unknown option = %q, want %q", out, want)
	}
}

func TestRenameMovesValueAndRemovesSource(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "old", "v")

	out := execCommand(t, cs, "RENAME", "old", "new")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("RENAME = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "GET", "new")
	if want := "$1\r\nv\r\n"; string(out) != want {
		t.Fatalf("GET new (after RENAME) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "EXISTS", "old")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("EXISTS old (after RENAME) = %q, want %q", out, want)
	}
}

func TestRenameMissingSourceErrors(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "RENAME", "nosuch", "new")
	want := "-" + ErrNoSuchKeyMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("RENAME (missing source) = %q, want %q", out, want)
	}
}

func TestFlushDBFlushAllClearKeys(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "a", "1")
	execCommand(t, cs, "SET", "b", "2")

	out := execCommand(t, cs, "FLUSHDB")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("FLUSHDB reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "EXISTS", "a", "b")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("EXISTS after FLUSHDB = %q, want %q", out, want)
	}

	execCommand(t, cs, "SET", "c", "3")
	out = execCommand(t, cs, "FLUSHALL")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("FLUSHALL reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "EXISTS", "c")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("EXISTS after FLUSHALL = %q, want %q", out, want)
	}
}

func TestGenericCommandsWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	cases := []struct {
		cmd  string
		args []string
	}{
		{"del", []string{"DEL"}},
		{"exists", []string{"EXISTS"}},
		{"expire", []string{"EXPIRE", "k"}},
		{"pexpire", []string{"PEXPIRE", "k"}},
		{"ttl", []string{"TTL"}},
		{"ttl", []string{"TTL", "k", "extra"}},
		{"pttl", []string{"PTTL"}},
		{"persist", []string{"PERSIST"}},
		{"type", []string{"TYPE"}},
		{"keys", []string{"KEYS"}},
		{"keys", []string{"KEYS", "a", "b"}},
		{"scan", []string{"SCAN"}},
		{"rename", []string{"RENAME", "a"}},
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

func TestExpirePExpireNonIntegerArg(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "k", "v")

	out := execCommand(t, cs, "EXPIRE", "k", "notanumber")
	want := "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("EXPIRE with a non-integer seconds arg = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "PEXPIRE", "k", "notanumber")
	if string(out) != want {
		t.Fatalf("PEXPIRE with a non-integer ms arg = %q, want %q", out, want)
	}
}
