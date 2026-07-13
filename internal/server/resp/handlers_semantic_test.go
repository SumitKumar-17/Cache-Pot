package resp

import (
	"strings"
	"testing"
)

func TestCacheSemanticSetGetRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.SEMANTIC", "SET", "What is the capital of France?", "Paris", "MODEL", "gpt-4", "TEMP", "0.7")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("CACHE.SEMANTIC SET reply = %q, want %q", out, want)
	}

	// Exact same prompt/model/temp: hit.
	out = execCommand(t, cs, "CACHE.SEMANTIC", "GET", "What is the capital of France?", "MODEL", "gpt-4", "TEMP", "0.7")
	if want := "$5\r\nParis\r\n"; string(out) != want {
		t.Fatalf("CACHE.SEMANTIC GET (hit) reply = %q, want %q", out, want)
	}

	// Different model: same prompt shouldn't cross-match.
	out = execCommand(t, cs, "CACHE.SEMANTIC", "GET", "What is the capital of France?", "MODEL", "claude", "TEMP", "0.7")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("CACHE.SEMANTIC GET (different model) reply = %q, want %q", out, want)
	}

	// Unrelated prompt, same partition: miss.
	out = execCommand(t, cs, "CACHE.SEMANTIC", "GET", "Tell me a joke about penguins", "MODEL", "gpt-4", "TEMP", "0.7")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("CACHE.SEMANTIC GET (unrelated) reply = %q, want %q", out, want)
	}
}

func TestCacheSemanticHitMissMetrics(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "CACHE.SEMANTIC", "SET", "What is Kubernetes?", "K8s is a container orchestrator.")
	execCommand(t, cs, "CACHE.SEMANTIC", "GET", "What is Kubernetes?") // hit
	execCommand(t, cs, "CACHE.SEMANTIC", "GET", "unrelated question")  // miss

	snap := cs.Deps.Metrics.Snapshot()
	if snap.SemanticCache.Hits != 1 {
		t.Fatalf("SemanticCache.Hits = %d, want 1", snap.SemanticCache.Hits)
	}
	if snap.SemanticCache.Misses != 1 {
		t.Fatalf("SemanticCache.Misses = %d, want 1", snap.SemanticCache.Misses)
	}
}

func TestCacheSemanticGetDefaultsAndMiss(t *testing.T) {
	cs := newTestClientState(t)

	// No prior SET: a GET with default MODEL/TEMP/THRESHOLD should miss
	// cleanly with a null bulk reply, not an error.
	out := execCommand(t, cs, "CACHE.SEMANTIC", "GET", "anything")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("CACHE.SEMANTIC GET (empty cache) reply = %q, want %q", out, want)
	}
}

func TestCacheSemanticWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.SEMANTIC", "SET", "onlyprompt")
	want := "-" + ErrWrongNumberOfArgs("cache.semantic") + "\r\n"
	if string(out) != want {
		t.Fatalf("CACHE.SEMANTIC SET wrong arity = %q, want %q", out, want)
	}
}

func TestCacheSemanticUnknownSubcommand(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.SEMANTIC", "FROB", "x")
	if !strings.HasPrefix(string(out), "-ERR") {
		t.Fatalf("CACHE.SEMANTIC unknown subcommand reply = %q, want a RESP error", out)
	}
}

func TestCachePromptSetGetRoundTrip(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.PROMPT", "SET", "Hello {{name}}", `{"name":"Sumit","lang":"Go"}`, "gpt-4", "Hello Sumit!")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("CACHE.PROMPT SET reply = %q, want %q", out, want)
	}

	// Same template/variables (different key order)/model: hit.
	out = execCommand(t, cs, "CACHE.PROMPT", "GET", "Hello {{name}}", `{"lang":"Go","name":"Sumit"}`, "gpt-4")
	if want := "$12\r\nHello Sumit!\r\n"; string(out) != want {
		t.Fatalf("CACHE.PROMPT GET (hit, reordered JSON keys) reply = %q, want %q", out, want)
	}

	// Different template text: miss.
	out = execCommand(t, cs, "CACHE.PROMPT", "GET", "Hi {{name}}", `{"name":"Sumit","lang":"Go"}`, "gpt-4")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("CACHE.PROMPT GET (different template) reply = %q, want %q", out, want)
	}

	// Different model: miss.
	out = execCommand(t, cs, "CACHE.PROMPT", "GET", "Hello {{name}}", `{"name":"Sumit","lang":"Go"}`, "claude")
	if want := "$-1\r\n"; string(out) != want {
		t.Fatalf("CACHE.PROMPT GET (different model) reply = %q, want %q", out, want)
	}
}

func TestCachePromptInvalidJSON(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.PROMPT", "SET", "Hello {{name}}", "not json", "gpt-4", "resp")
	want := "-" + ErrInvalidJSONMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("CACHE.PROMPT SET invalid JSON = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "CACHE.PROMPT", "GET", "Hello {{name}}", "not json", "gpt-4")
	if string(out) != want {
		t.Fatalf("CACHE.PROMPT GET invalid JSON = %q, want %q", out, want)
	}
}

func TestCachePromptWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.PROMPT", "SET", "tmpl", "{}")
	want := "-" + ErrWrongNumberOfArgs("cache.prompt") + "\r\n"
	if string(out) != want {
		t.Fatalf("CACHE.PROMPT SET wrong arity = %q, want %q", out, want)
	}
}

func TestCacheSemanticCostRecordsSavingsOnHit(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.SEMANTIC", "SET", "What is Kubernetes?", "K8s is a container orchestrator.", "COST", "0.01")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("CACHE.SEMANTIC SET with COST reply = %q, want %q", out, want)
	}

	execCommand(t, cs, "CACHE.SEMANTIC", "GET", "What is Kubernetes?") // hit

	snap := cs.Deps.Analytics.Snapshot()
	if snap.MoneySavedTotalUSD != 0.01 {
		t.Fatalf("MoneySavedTotalUSD = %v, want 0.01", snap.MoneySavedTotalUSD)
	}
	if len(snap.TopExpensiveEntries) != 1 || snap.TopExpensiveEntries[0].CacheType != "semantic" {
		t.Fatalf("TopExpensiveEntries = %+v, want one semantic entry", snap.TopExpensiveEntries)
	}
}

func TestCacheSemanticNoCostRecordsNoSavings(t *testing.T) {
	cs := newTestClientState(t)

	execCommand(t, cs, "CACHE.SEMANTIC", "SET", "What is Kubernetes?", "K8s is a container orchestrator.")
	execCommand(t, cs, "CACHE.SEMANTIC", "GET", "What is Kubernetes?") // hit, but no COST was ever supplied

	snap := cs.Deps.Analytics.Snapshot()
	if snap.MoneySavedTotalUSD != 0 {
		t.Fatalf("MoneySavedTotalUSD = %v, want exactly 0 when no COST was ever supplied", snap.MoneySavedTotalUSD)
	}
	if len(snap.TopExpensiveEntries) != 0 {
		t.Fatalf("TopExpensiveEntries = %+v, want none", snap.TopExpensiveEntries)
	}
}

func TestCacheSemanticNegativeCostIsError(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.SEMANTIC", "SET", "prompt", "response", "COST", "-1")
	want := "-" + ErrCostNegativeMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("CACHE.SEMANTIC SET negative COST = %q, want %q", out, want)
	}
}

func TestCacheSemanticNonNumericCostIsError(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.SEMANTIC", "SET", "prompt", "response", "COST", "not-a-number")
	want := "-" + ErrNotFloatMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("CACHE.SEMANTIC SET non-numeric COST = %q, want %q", out, want)
	}
}

func TestCachePromptCostRecordsSavingsOnHit(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.PROMPT", "SET", "Hello {{name}}", `{"name":"Sumit"}`, "gpt-4", "Hello Sumit!", "COST", "0.02")
	if want := "+OK\r\n"; string(out) != want {
		t.Fatalf("CACHE.PROMPT SET with COST reply = %q, want %q", out, want)
	}

	execCommand(t, cs, "CACHE.PROMPT", "GET", "Hello {{name}}", `{"name":"Sumit"}`, "gpt-4") // hit

	snap := cs.Deps.Analytics.Snapshot()
	if snap.MoneySavedTotalUSD != 0.02 {
		t.Fatalf("MoneySavedTotalUSD = %v, want 0.02", snap.MoneySavedTotalUSD)
	}
	if len(snap.TopExpensiveEntries) != 1 || snap.TopExpensiveEntries[0].CacheType != "prompt" {
		t.Fatalf("TopExpensiveEntries = %+v, want one prompt entry", snap.TopExpensiveEntries)
	}
}

func TestCachePromptNegativeCostIsError(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "CACHE.PROMPT", "SET", "tmpl", "{}", "gpt-4", "resp", "COST", "-0.5")
	want := "-" + ErrCostNegativeMsg + "\r\n"
	if string(out) != want {
		t.Fatalf("CACHE.PROMPT SET negative COST = %q, want %q", out, want)
	}
}
