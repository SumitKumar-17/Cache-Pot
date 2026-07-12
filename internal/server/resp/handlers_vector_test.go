package resp

import (
	"strings"
	"testing"
)

func TestVectorUpsertSearchDeleteRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "[1,0]")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("VECTOR.UPSERT reply = %q, want %q", out, want)
	}
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "b", "[0,1]")
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "c", "[0.9,0.1]")

	out = execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]")
	want := "*3\r\n$1\r\na\r\n$1\r\nc\r\n$1\r\nb\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.SEARCH reply = %q, want %q", out, want)
	}

	// Delete the closest match, then re-search: should no longer appear.
	out = execCommand(t, cs, "VECTOR.DELETE", "docs", "a")
	if want := ":1\r\n"; string(out) != want {
		t.Fatalf("VECTOR.DELETE (existing) reply = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "VECTOR.DELETE", "docs", "a")
	if want := ":0\r\n"; string(out) != want {
		t.Fatalf("VECTOR.DELETE (already deleted) reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "K", "1")
	want = "*1\r\n$1\r\nc\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.SEARCH after delete reply = %q, want %q", out, want)
	}
}

func TestVectorSearchWithScores(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "[1,0]")

	out := execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "WITHSCORES")
	want := "*2\r\n$1\r\na\r\n$1\r\n1\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.SEARCH WITHSCORES reply = %q, want %q", out, want)
	}
}

func TestVectorSearchUnknownNamespaceEmpty(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "VECTOR.SEARCH", "nope", "[1,0]")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("VECTOR.SEARCH (unknown namespace) reply = %q, want %q", out, want)
	}
}

func TestVectorUpsertInvalidVectorJSON(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "not json")
	want := "-" + ErrInvalidVectorJSONMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.UPSERT invalid JSON reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", `{"not":"an array"}`)
	if string(out) != want {
		t.Fatalf("VECTOR.UPSERT non-array JSON reply = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", `["not","numeric"]`)
	if string(out) != want {
		t.Fatalf("VECTOR.UPSERT non-numeric array reply = %q, want %q", out, want)
	}
}

func TestVectorUpsertInvalidMetadataJSON(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "[1,0]", "METADATA", "not json")
	want := "-" + ErrInvalidMetadataJSONMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.UPSERT invalid metadata JSON reply = %q, want %q", out, want)
	}
}

func TestVectorSearchInvalidVectorJSON(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "VECTOR.SEARCH", "docs", "not json")
	want := "-" + ErrInvalidVectorJSONMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.SEARCH invalid JSON reply = %q, want %q", out, want)
	}
}

func TestVectorSearchFilter(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "[1,0]", "METADATA", `{"category":"fruit"}`)
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "b", "[1,0]", "METADATA", `{"category":"veg"}`)

	out := execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "FILTER", "category", "fruit")
	want := "*1\r\n$1\r\na\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.SEARCH FILTER reply = %q, want %q", out, want)
	}
}

func TestVectorSearchFilterNumericMetadata(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "[1,0]", "METADATA", `{"version":3}`)

	out := execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "FILTER", "version", "3")
	want := "*1\r\n$1\r\na\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.SEARCH FILTER (numeric metadata) reply = %q, want %q", out, want)
	}
}

func TestVectorSearchHybrid(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "vecwinner", "[1,0.01]", "TEXT", "completely unrelated text")
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "kwwinner", "[1,0.5]", "TEXT", "golang cache pot vector search")

	// Pure vector search: vecwinner should be best.
	out := execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "K", "1")
	want := "*1\r\n$9\r\nvecwinner\r\n"
	if string(out) != want {
		t.Fatalf("pure-vector VECTOR.SEARCH reply = %q, want %q", out, want)
	}

	// Hybrid search weighted heavily toward keyword overlap: kwwinner
	// should now come out on top.
	out = execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "K", "1", "HYBRID", "golang cache pot vector search", "ALPHA", "0.2")
	want = "*1\r\n$8\r\nkwwinner\r\n"
	if string(out) != want {
		t.Fatalf("hybrid VECTOR.SEARCH reply = %q, want %q", out, want)
	}
}

func TestVectorUpsertReplacesEntirely(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "[1,0]", "METADATA", `{"k":"v1"}`)
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "[1,0]", "METADATA", `{"k":"v2"}`)

	out := execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "FILTER", "k", "v1")
	if want := "*0\r\n"; string(out) != want {
		t.Fatalf("search on stale metadata = %q, want %q", out, want)
	}
	out = execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "FILTER", "k", "v2")
	if want := "*1\r\n$1\r\na\r\n"; string(out) != want {
		t.Fatalf("search on new metadata = %q, want %q", out, want)
	}
}

func TestVectorUpsertWrongArity(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "VECTOR.UPSERT", "docs", "a")
	want := "-" + ErrWrongNumberOfArgs("vector.upsert") + "\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.UPSERT wrong arity reply = %q, want %q", out, want)
	}
}

func TestVectorSearchWrongArity(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "VECTOR.SEARCH", "docs")
	want := "-" + ErrWrongNumberOfArgs("vector.search") + "\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.SEARCH wrong arity reply = %q, want %q", out, want)
	}
}

func TestVectorDeleteWrongArity(t *testing.T) {
	cs := newTestClientState(t)
	out := execCommand(t, cs, "VECTOR.DELETE", "docs")
	want := "-" + ErrWrongNumberOfArgs("vector.delete") + "\r\n"
	if string(out) != want {
		t.Fatalf("VECTOR.DELETE wrong arity reply = %q, want %q", out, want)
	}
}

func TestVectorSearchUnknownOptionSyntaxError(t *testing.T) {
	cs := newTestClientState(t)
	execCommand(t, cs, "VECTOR.UPSERT", "docs", "a", "[1,0]")

	out := execCommand(t, cs, "VECTOR.SEARCH", "docs", "[1,0]", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR syntax error") {
		t.Fatalf("VECTOR.SEARCH unknown option reply = %q, want a syntax error", out)
	}
}
