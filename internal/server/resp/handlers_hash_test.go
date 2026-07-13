package resp

import "testing"

func TestHSetHGetRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "HSET", "h", "f1", "v1", "f2", "v2")
	if want := ":2\r\n"; string(out) != want {
		t.Fatalf("HSET (new hash) reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "HGET", "h", "f1")
	if want := "$2\r\nv1\r\n"; string(out) != want {
		t.Fatalf("HGET f1 = %q, want %q", out, want)
	}

	// Re-setting an existing field is not counted as "added".
	out = execCommand(t, cs, "HSET", "h", "f1", "v1-new", "f3", "v3")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("HSET (1 new field, 1 overwrite) reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "HGET", "h", "f1")
	if want := "$6\r\nv1-new\r\n"; string(out) != want {
		t.Fatalf("HGET f1 after overwrite = %q, want %q", out, want)
	}
}

func TestHGetMissingFieldAndKey(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HSET", "h", "f1", "v1")

	out := execCommand(t, cs, "HGET", "h", "nope")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("HGET missing field = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "HGET", "nokey", "f1")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("HGET missing key = %q, want %q", out, want)
	}
}

func TestHSetOddArgsSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "HSET", "h", "f1", "v1", "f2")
	want := "-" + ErrWrongNumberOfArgs("hset") + "\r\n"
	if string(out) != want {
		t.Fatalf("HSET with an odd number of field/value args = %q, want %q", out, want)
	}
}

func TestHGetAllRoundTripSortedByField(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HSET", "h", "z", "26", "a", "1", "m", "13")

	out := execCommand(t, cs, "HGETALL", "h")
	want := "*6\r\n$1\r\na\r\n$1\r\n1\r\n$1\r\nm\r\n$2\r\n13\r\n$1\r\nz\r\n$2\r\n26\r\n"
	if string(out) != want {
		t.Fatalf("HGETALL reply = %q, want %q", out, want)
	}
}

func TestHGetAllMissingKeyEmptyArray(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "HGETALL", "nokey")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("HGETALL missing key = %q, want %q", out, want)
	}
}

func TestHDelRemovesOnlyGivenFields(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HSET", "h", "f1", "v1", "f2", "v2", "f3", "v3")

	out := execCommand(t, cs, "HDEL", "h", "f1", "f3", "nope")
	if want := ":2\r\n"; string(out) != want {
		t.Fatalf("HDEL (2 existing + 1 missing) reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "HGETALL", "h")
	want := "*2\r\n$2\r\nf2\r\n$2\r\nv2\r\n"
	if string(out) != want {
		t.Fatalf("HGETALL after HDEL = %q, want %q", out, want)
	}
}

func TestHExists(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HSET", "h", "f1", "v1")

	out := execCommand(t, cs, "HEXISTS", "h", "f1")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("HEXISTS (present) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "HEXISTS", "h", "nope")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("HEXISTS (missing field) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "HEXISTS", "nokey", "f1")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("HEXISTS (missing key) = %q, want %q", out, want)
	}
}

func TestHKeysHVals(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HSET", "h", "z", "26", "a", "1")

	out := execCommand(t, cs, "HKEYS", "h")
	if want := "*2\r\n$1\r\na\r\n$1\r\nz\r\n"; string(out) != want {
		t.Fatalf("HKEYS reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "HVALS", "h")
	if want := "*2\r\n$1\r\n1\r\n$2\r\n26\r\n"; string(out) != want {
		t.Fatalf("HVALS reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "HKEYS", "nokey")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("HKEYS missing key = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "HVALS", "nokey")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("HVALS missing key = %q, want %q", out, want)
	}
}

func TestHLen(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "HLEN", "nokey")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("HLEN missing key = %q, want %q", out, want)
	}
	execCommand(t, cs, "HSET", "h", "f1", "v1", "f2", "v2")
	out = execCommand(t, cs, "HLEN", "h")
	if want := ":2\r\n"; string(out) != want {
		t.Fatalf("HLEN = %q, want %q", out, want)
	}
}

func TestHMGetMixedPresentAndMissing(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HSET", "h", "f1", "v1", "f2", "v2")

	out := execCommand(t, cs, "HMGET", "h", "f1", "nope", "f2")
	want := "*3\r\n$2\r\nv1\r\n$-1\r\n$2\r\nv2\r\n"
	if string(out) != want {
		t.Fatalf("HMGET reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "HMGET", "nokey", "f1", "f2")
	want = "*2\r\n$-1\r\n$-1\r\n"
	if string(out) != want {
		t.Fatalf("HMGET on missing key = %q, want %q", out, want)
	}
}

func TestHIncrBy(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "HINCRBY", "h", "counter", "5")
	if want := ":5\r\n"; string(out) != want {
		t.Fatalf("HINCRBY (new field) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "HINCRBY", "h", "counter", "-2")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("HINCRBY (negative delta) = %q, want %q", out, want)
	}
}

func TestHIncrByNonIntegerField(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "HSET", "h", "f", "notanumber")

	out := execCommand(t, cs, "HINCRBY", "h", "f", "1")
	want := "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("HINCRBY on non-integer field = %q, want %q", out, want)
	}
}

func TestHIncrByNonIntegerDelta(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "HINCRBY", "h", "f", "notanumber")
	want := "-" + ErrNotIntegerMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("HINCRBY with a non-integer delta = %q, want %q", out, want)
	}
}

func TestHashCommandsWrongType(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "s", "v")

	cases := []struct {
		name string
		args []string
	}{
		{"HGET", []string{"HGET", "s", "f"}},
		{"HGETALL", []string{"HGETALL", "s"}},
		{"HDEL", []string{"HDEL", "s", "f"}},
		{"HEXISTS", []string{"HEXISTS", "s", "f"}},
		{"HKEYS", []string{"HKEYS", "s"}},
		{"HVALS", []string{"HVALS", "s"}},
		{"HLEN", []string{"HLEN", "s"}},
		{"HMGET", []string{"HMGET", "s", "f"}},
		{"HINCRBY", []string{"HINCRBY", "s", "f", "1"}},
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

func TestHashCommandsWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	cases := []struct {
		cmd  string
		args []string
	}{
		{"hset", []string{"HSET", "h"}},
		{"hget", []string{"HGET", "h"}},
		{"hget", []string{"HGET", "h", "f", "extra"}},
		{"hgetall", []string{"HGETALL"}},
		{"hdel", []string{"HDEL", "h"}},
		{"hexists", []string{"HEXISTS", "h"}},
		{"hkeys", []string{"HKEYS"}},
		{"hvals", []string{"HVALS"}},
		{"hlen", []string{"HLEN"}},
		{"hmget", []string{"HMGET", "h"}},
		{"hincrby", []string{"HINCRBY", "h", "f"}},
		{"hincrby", []string{"HINCRBY", "h", "f", "1", "extra"}},
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
