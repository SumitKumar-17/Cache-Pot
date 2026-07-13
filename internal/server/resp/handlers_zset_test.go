package resp

import "testing"

func TestZAddZScoreZCard(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "ZADD", "z", "1", "a", "2", "b")
	if want := ":2\r\n"; string(out) != want {
		t.Fatalf("ZADD (2 new members) = %q, want %q", out, want)
	}
	// Re-adding an existing member (even with a new score) is not counted.
	out = execCommand(t, cs, "ZADD", "z", "5", "a", "3", "c")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("ZADD (1 new + 1 updated) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "ZSCORE", "z", "a")
	if want := "$1\r\n5\r\n"; string(out) != want {
		t.Fatalf("ZSCORE after score update = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "ZCARD", "z")
	if want := ":3\r\n"; string(out) != want {
		t.Fatalf("ZCARD = %q, want %q", out, want)
	}
}

func TestZAddOddPairsSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "ZADD", "z", "1", "a", "2")
	want := "-" + ErrSyntaxMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("ZADD with an odd number of score/member args = %q, want %q", out, want)
	}
}

func TestZAddInvalidScoreNotFloat(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "ZADD", "z", "notafloat", "a")
	want := "-" + ErrNotFloatMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("ZADD with a non-float score = %q, want %q", out, want)
	}
}

func TestZRangeAscendingOrderWithScores(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "ZADD", "z", "3", "c", "1", "a", "2", "b")

	out := execCommand(t, cs, "ZRANGE", "z", "0", "-1")
	want := "*3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n"
	if string(out) != want {
		t.Fatalf("ZRANGE (ascending, no scores) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "ZRANGE", "z", "0", "-1", "WITHSCORES")
	want = "*6\r\n$1\r\na\r\n$1\r\n1\r\n$1\r\nb\r\n$1\r\n2\r\n$1\r\nc\r\n$1\r\n3\r\n"
	if string(out) != want {
		t.Fatalf("ZRANGE WITHSCORES = %q, want %q", out, want)
	}
}

func TestZRevRangeDescendingOrder(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "ZADD", "z", "3", "c", "1", "a", "2", "b")

	out := execCommand(t, cs, "ZREVRANGE", "z", "0", "-1")
	want := "*3\r\n$1\r\nc\r\n$1\r\nb\r\n$1\r\na\r\n"
	if string(out) != want {
		t.Fatalf("ZREVRANGE (descending) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "ZREVRANGE", "z", "0", "0")
	if want := "*1\r\n$1\r\nc\r\n"; string(out) != want {
		t.Fatalf("ZREVRANGE top-1 = %q, want %q", out, want)
	}
}

func TestZRangeUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "ZADD", "z", "1", "a")
	out := execCommand(t, cs, "ZRANGE", "z", "0", "-1", "FROB")
	want := "-" + ErrSyntaxMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("ZRANGE with an unknown 5th arg = %q, want %q", out, want)
	}
}

func TestZRankAscendingAndMissing(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "ZADD", "z", "3", "c", "1", "a", "2", "b")

	out := execCommand(t, cs, "ZRANK", "z", "a")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("ZRANK a (lowest score) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "ZRANK", "z", "c")
	if want := ":2\r\n"; string(out) != want {
		t.Fatalf("ZRANK c (highest score) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "ZRANK", "z", "nope")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("ZRANK for a missing member = %q, want %q", out, want)
	}
}

func TestZScoreMissingMember(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "ZADD", "z", "1", "a")

	out := execCommand(t, cs, "ZSCORE", "z", "nope")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("ZSCORE for a missing member = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "ZSCORE", "nokey", "a")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("ZSCORE on a missing key = %q, want %q", out, want)
	}
}

func TestZIncrBy(t *testing.T) {
	cs := newTestClientState(t)

	// New member on a new key starts implicitly at 0 before the delta.
	out := execCommand(t, cs, "ZINCRBY", "z", "5", "a")
	if want := "$1\r\n5\r\n"; string(out) != want {
		t.Fatalf("ZINCRBY (new member) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "ZINCRBY", "z", "-2", "a")
	if want := "$1\r\n3\r\n"; string(out) != want {
		t.Fatalf("ZINCRBY (negative delta) = %q, want %q", out, want)
	}
}

func TestZIncrByInvalidDelta(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "ZINCRBY", "z", "notafloat", "a")
	want := "-" + ErrNotFloatMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("ZINCRBY with a non-float delta = %q, want %q", out, want)
	}
}

func TestZRangeByScoreBoundaryInclusive(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "ZADD", "z", "1", "a", "2", "b", "3", "c")

	out := execCommand(t, cs, "ZRANGEBYSCORE", "z", "1", "2")
	want := "*2\r\n$1\r\na\r\n$1\r\nb\r\n"
	if string(out) != want {
		t.Fatalf("ZRANGEBYSCORE 1 2 (inclusive both ends) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "ZRANGEBYSCORE", "z", "-inf", "+inf")
	want = "*3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n"
	if string(out) != want {
		t.Fatalf("ZRANGEBYSCORE -inf +inf = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "ZRANGEBYSCORE", "z", "10", "20")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("ZRANGEBYSCORE outside any score = %q, want %q", out, want)
	}
}

func TestZRangeByScoreInvalidBound(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "ZRANGEBYSCORE", "z", "notafloat", "1")
	want := "-" + ErrNotFloatMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("ZRANGEBYSCORE with a non-float min = %q, want %q", out, want)
	}
}

func TestZRem(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "ZADD", "z", "1", "a", "2", "b")

	out := execCommand(t, cs, "ZREM", "z", "a", "nope")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("ZREM (1 existing + 1 missing) = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "ZCARD", "z")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("ZCARD after ZREM = %q, want %q", out, want)
	}
}

func TestZSetCommandsWrongType(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "SET", "s", "v")

	cases := []struct {
		name string
		args []string
	}{
		{"ZREM", []string{"ZREM", "s", "a"}},
		{"ZRANGE", []string{"ZRANGE", "s", "0", "-1"}},
		{"ZREVRANGE", []string{"ZREVRANGE", "s", "0", "-1"}},
		{"ZSCORE", []string{"ZSCORE", "s", "a"}},
		{"ZRANK", []string{"ZRANK", "s", "a"}},
		{"ZCARD", []string{"ZCARD", "s"}},
		{"ZRANGEBYSCORE", []string{"ZRANGEBYSCORE", "s", "0", "1"}},
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

func TestZSetCommandsWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	cases := []struct {
		cmd  string
		args []string
	}{
		{"zadd", []string{"ZADD", "z", "1"}},
		{"zrem", []string{"ZREM", "z"}},
		{"zrange", []string{"ZRANGE", "z", "0"}},
		{"zrevrange", []string{"ZREVRANGE", "z", "0"}},
		{"zscore", []string{"ZSCORE", "z"}},
		{"zrank", []string{"ZRANK", "z"}},
		{"zcard", []string{"ZCARD"}},
		{"zincrby", []string{"ZINCRBY", "z", "1"}},
		{"zrangebyscore", []string{"ZRANGEBYSCORE", "z", "0"}},
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
