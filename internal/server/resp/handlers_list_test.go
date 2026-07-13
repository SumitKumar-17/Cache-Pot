package resp

import "testing"

func TestLPushPrependsInReverseArgOrder(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "LPUSH", "l", "a", "b", "c")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("LPUSH reply = %q, want %q", out, want)
	}
	// Each pushed value goes to the head in turn, so the final order is the
	// reverse of the pushed argument order: c, b, a.
	out = execCommand(t, cs, "LRANGE", "l", "0", "-1")
	want := "*3\r\n$1\r\nc\r\n$1\r\nb\r\n$1\r\na\r\n"
	if string(out) != want {
		t.Fatalf("LRANGE after LPUSH a b c = %q, want %q", out, want)
	}
}

func TestRPushAppendsInArgOrder(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "RPUSH", "l", "a", "b", "c")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("RPUSH reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "LRANGE", "l", "0", "-1")
	want := "*3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n"
	if string(out) != want {
		t.Fatalf("LRANGE after RPUSH a b c = %q, want %q", out, want)
	}
}

func TestLPopRPopOrderAndDraining(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "RPUSH", "l", "a", "b", "c")

	out := execCommand(t, cs, "LPOP", "l")
	if want := "$1\r\na\r\n"; string(out) != want {
		t.Fatalf("LPOP (1) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "RPOP", "l")
	if want := "$1\r\nc\r\n"; string(out) != want {
		t.Fatalf("RPOP (1) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "LLEN", "l")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("LLEN after 1 LPOP + 1 RPOP = %q, want %q", out, want)
	}

	// Draining the last element should remove the key entirely.
	execCommand(t, cs, "LPOP", "l")
	out = execCommand(t, cs, "EXISTS", "l")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("EXISTS after draining the list = %q, want %q", out, want)
	}
}

func TestLPopRPopOnMissingKeyIsNilNotError(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "LPOP", "nokey")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("LPOP on missing key = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "RPOP", "nokey")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("RPOP on missing key = %q, want %q", out, want)
	}
}

func TestLRangeNegativeIndices(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "RPUSH", "l", "a", "b", "c", "d", "e")

	out := execCommand(t, cs, "LRANGE", "l", "-3", "-1")
	want := "*3\r\n$1\r\nc\r\n$1\r\nd\r\n$1\r\ne\r\n"
	if string(out) != want {
		t.Fatalf("LRANGE -3 -1 = %q, want %q", out, want)
	}

	// A start index further negative than the list's length clamps to 0.
	out = execCommand(t, cs, "LRANGE", "l", "-100", "-1")
	want = "*5\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n$1\r\nd\r\n$1\r\ne\r\n"
	if string(out) != want {
		t.Fatalf("LRANGE -100 -1 = %q, want %q", out, want)
	}

	// start > stop (after normalization) yields an empty array, not an error.
	out = execCommand(t, cs, "LRANGE", "l", "3", "1")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("LRANGE with start>stop = %q, want %q", out, want)
	}
}

func TestLIndexNegativeAndOutOfRange(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "RPUSH", "l", "a", "b", "c")

	out := execCommand(t, cs, "LINDEX", "l", "-1")
	if want := "$1\r\nc\r\n"; string(out) != want {
		t.Fatalf("LINDEX -1 = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "LINDEX", "l", "0")
	if want := "$1\r\na\r\n"; string(out) != want {
		t.Fatalf("LINDEX 0 = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "LINDEX", "l", "100")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("LINDEX out of range = %q, want %q", out, want)
	}
}

func TestLSetRoundTripAndOutOfRange(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "RPUSH", "l", "a", "b", "c")

	out := execCommand(t, cs, "LSET", "l", "1", "B")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("LSET reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "LINDEX", "l", "1")
	if want := "$1\r\nB\r\n"; string(out) != want {
		t.Fatalf("LINDEX after LSET = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "LSET", "l", "100", "x")
	if want := "-ERR index out of range\r\n"; string(out) != want {
		t.Fatalf("LSET out of range = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "LSET", "nokey", "0", "x")
	if want := "-" + ErrNoSuchKeyMsg + "\r\n"; string(out) != want {
		t.Fatalf("LSET on missing key = %q, want %q", out, want)
	}
}

func TestLRemPositiveNegativeAndZeroCount(t *testing.T) {
	cs := newTestClientState(t)

	// Positive count: remove from head towards tail, up to count occurrences.
	execCommand(t, cs, "RPUSH", "pos", "a", "b", "a", "c", "a")
	out := execCommand(t, cs, "LREM", "pos", "2", "a")
	if want := ":2\r\n"; string(out) != want {
		t.Fatalf("LREM count=2 removed count = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "LRANGE", "pos", "0", "-1")
	want := "*3\r\n$1\r\nb\r\n$1\r\nc\r\n$1\r\na\r\n"
	if string(out) != want {
		t.Fatalf("LRANGE after LREM count=2 = %q, want %q", out, want)
	}

	// Negative count: remove from tail towards head, up to |count| occurrences.
	execCommand(t, cs, "RPUSH", "neg", "a", "b", "a", "c", "a")
	out = execCommand(t, cs, "LREM", "neg", "-2", "a")
	if want := ":2\r\n"; string(out) != want {
		t.Fatalf("LREM count=-2 removed count = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "LRANGE", "neg", "0", "-1")
	want = "*3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n"
	if string(out) != want {
		t.Fatalf("LRANGE after LREM count=-2 = %q, want %q", out, want)
	}

	// Zero count: remove all occurrences.
	execCommand(t, cs, "RPUSH", "zero", "a", "b", "a", "c", "a")
	out = execCommand(t, cs, "LREM", "zero", "0", "a")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("LREM count=0 removed count = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "LRANGE", "zero", "0", "-1")
	want = "*2\r\n$1\r\nb\r\n$1\r\nc\r\n"
	if string(out) != want {
		t.Fatalf("LRANGE after LREM count=0 = %q, want %q", out, want)
	}
}

func TestListCommandsWrongType(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "s", "v")

	cases := []struct {
		name string
		args []string
	}{
		{"LPUSH", []string{"LPUSH", "s", "x"}},
		{"RPUSH", []string{"RPUSH", "s", "x"}},
		{"LPOP", []string{"LPOP", "s"}},
		{"RPOP", []string{"RPOP", "s"}},
		{"LRANGE", []string{"LRANGE", "s", "0", "-1"}},
		{"LLEN", []string{"LLEN", "s"}},
		{"LINDEX", []string{"LINDEX", "s", "0"}},
		{"LSET", []string{"LSET", "s", "0", "x"}},
		{"LREM", []string{"LREM", "s", "0", "x"}},
	}
	want := "-" + ErrWrongTypeMsg + "\r\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := execCommand(t, cs, tc.args...)
			if string(out) != want {
				t.Fatalf("%s on string key = %q, want %q", tc.name, out, want)
			}
		})
	}
}

func TestListCommandsWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	cases := []struct {
		cmd  string
		args []string
	}{
		{"lpush", []string{"LPUSH", "l"}},
		{"rpush", []string{"RPUSH", "l"}},
		{"lpop", []string{"LPOP"}},
		{"lpop", []string{"LPOP", "l", "extra"}},
		{"rpop", []string{"RPOP"}},
		{"lrange", []string{"LRANGE", "l", "0"}},
		{"llen", []string{"LLEN"}},
		{"lindex", []string{"LINDEX", "l"}},
		{"lset", []string{"LSET", "l", "0"}},
		{"lrem", []string{"LREM", "l", "0"}},
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
