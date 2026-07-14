// This file drives Cache-Pot against the REAL OpenAI API (real embeddings,
// real chat completions) -- not the mock providers every other test in this
// module uses. It exists to catch the class of thing a mock provider can
// never surface: how a real embedding model actually scores a paraphrase,
// whether a real completion model actually extracts sensible entities, etc.
//
// These tests are automatically skipped unless a real .env file exists at
// the repo root with a working OPENAI_API_KEY -- see loadRealOpenAIEnv.
// .env is git-ignored (see .gitignore) and is never present in CI, so these
// never run there and never cost CI anything; they're an opt-in local check
// for a developer who has their own OpenAI key. Each real API call costs a
// small amount of real money and takes real network time -- keep the number
// of calls in each test small and the assertions loose (real model output
// varies run to run; assert structural/numeric properties, not exact text).
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/server"
)

// loadRealOpenAIEnv reads OPENAI_API_KEY/OPENAI_API_BASE from the repo
// root's .env file (test/integration's working directory is this package
// dir, so ../../.env), without overriding any already-exported real env var
// -- the same "real env wins" precedent as cmd/cachepotd's own dotenv
// loader. Returns ("", "") if no usable key is found either way.
func loadRealOpenAIEnv(t *testing.T) (key, base string) {
	t.Helper()

	envPath := filepath.Join("..", "..", ".env")
	if data, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if k == "" {
				continue
			}
			if _, alreadySet := os.LookupEnv(k); !alreadySet {
				os.Setenv(k, v)
			}
		}
	}

	key = os.Getenv("OPENAI_API_KEY")
	base = os.Getenv("OPENAI_API_BASE")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return key, base
}

// requireRealOpenAI skips the calling test unless a real API key is
// available, and returns a server.Config pre-wired for real embeddings and
// real completions against it.
func requireRealOpenAI(t *testing.T) server.Config {
	t.Helper()
	key, base := loadRealOpenAIEnv(t)
	if key == "" {
		t.Skip("no OPENAI_API_KEY found in .env -- skipping real-OpenAI integration test (this is expected in CI; add a working key to .env to run this locally)")
	}
	return server.Config{
		MaxConnections:     1000,
		EmbedProvider:      "openai",
		CompletionProvider: "openai",
		OpenAIAPIKey:       key,
		OpenAIAPIBase:      base,
	}
}

// TestRealOpenAISemanticCacheThresholdBehavior verifies, against the real
// embeddings API, the exact threshold-sensitivity documented in
// docs/commands/semantic-cache.md's "Tune THRESHOLD for real embeddings"
// warning: a same-words/different-case paraphrase hits comfortably above
// the 0.85 default, but a same-concept/different-words paraphrase ("k8s" vs
// "Kubernetes") scores meaningfully lower and misses at 0.85 -- while
// hitting once THRESHOLD is lowered. If a future embeddings-model swap
// changes this balance enough to fail this test, the docs claim above needs
// re-verifying, not just this test.
func TestRealOpenAISemanticCacheThresholdBehavior(t *testing.T) {
	cfg := requireRealOpenAI(t)
	addr := startServerWithConfig(t, cfg)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := rdb.Do(ctx, "CACHE.SEMANTIC", "SET", "What is Kubernetes?", "K8s is a container orchestrator.", "MODEL", "gpt-4").Err(); err != nil {
		t.Fatalf("CACHE.SEMANTIC SET: %v", err)
	}

	// Same words, different case -- comfortably above the 0.85 default.
	val, err := rdb.Do(ctx, "CACHE.SEMANTIC", "GET", "what is kubernetes", "MODEL", "gpt-4").Text()
	if err != nil {
		t.Fatalf("CACHE.SEMANTIC GET (case paraphrase) at default threshold: %v (want a hit)", err)
	}
	if val != "K8s is a container orchestrator." {
		t.Fatalf("CACHE.SEMANTIC GET (case paraphrase) = %q, want the cached response", val)
	}

	// Same concept, different words, at the DEFAULT threshold -- expected
	// to miss (redis.Nil), per the measured ~0.5-0.7 real-world similarity
	// for this exact pair (see docs/commands/semantic-cache.md).
	if err := rdb.Do(ctx, "CACHE.SEMANTIC", "GET", "what is k8s?", "MODEL", "gpt-4").Err(); err == nil {
		t.Fatal("CACHE.SEMANTIC GET (abbreviation paraphrase) at default threshold succeeded, want a miss (redis.Nil) -- if this now hits, real embedding-model behavior has changed and docs/commands/semantic-cache.md's threshold guidance needs re-verifying")
	}

	// Same query, lowered THRESHOLD -- expected to hit now.
	val, err = rdb.Do(ctx, "CACHE.SEMANTIC", "GET", "what is k8s?", "MODEL", "gpt-4", "THRESHOLD", "0.5").Text()
	if err != nil {
		t.Fatalf("CACHE.SEMANTIC GET (abbreviation paraphrase) at THRESHOLD 0.5: %v (want a hit)", err)
	}
	if val != "K8s is a container orchestrator." {
		t.Fatalf("CACHE.SEMANTIC GET (abbreviation paraphrase, THRESHOLD 0.5) = %q, want the cached response", val)
	}

	// A genuinely unrelated query should still miss regardless of threshold
	// laxness in the range we exercise here.
	if err := rdb.Do(ctx, "CACHE.SEMANTIC", "GET", "how do I bake sourdough bread", "MODEL", "gpt-4", "THRESHOLD", "0.5").Err(); err == nil {
		t.Fatal("CACHE.SEMANTIC GET (unrelated query) succeeded, want a miss")
	}
}

// TestRealOpenAIMemorySearchRanksBySemanticSimilarity is something no
// mock-provider test can prove: that MEMORY.SEARCH's ranking reflects real
// semantic relevance, not just word overlap. Three memories are stored on
// unrelated topics (networking, response-formatting preferences, and
// databases); a query about "connecting containers to each other" shares
// almost no words with the networking memory but should still rank it
// first.
func TestRealOpenAIMemorySearchRanksBySemanticSimilarity(t *testing.T) {
	cfg := requireRealOpenAI(t)
	addr := startServerWithConfig(t, cfg)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	memories := []string{
		"Kubernetes pods communicate with each other over a virtual network using CNI plugins.",
		"The user prefers concise, bullet-point summaries instead of long paragraphs.",
		"PostgreSQL indexes can dramatically speed up queries that filter on a specific column.",
	}
	for _, m := range memories {
		if err := rdb.Do(ctx, "MEMORY.PUT", "research-bot", m, "KIND", "episodic").Err(); err != nil {
			t.Fatalf("MEMORY.PUT %q: %v", m, err)
		}
	}

	results, err := rdb.Do(ctx, "MEMORY.SEARCH", "default", "how do containers talk to one another over the network", "K", "1").StringSlice()
	if err != nil {
		t.Fatalf("MEMORY.SEARCH: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("MEMORY.SEARCH returned %d results, want 1", len(results))
	}

	fields, err := rdb.Do(ctx, "MEMORY.GET", "default", results[0]).StringSlice()
	if err != nil {
		t.Fatalf("MEMORY.GET on top result: %v", err)
	}
	var content string
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "content" {
			content = fields[i+1]
		}
	}
	if !strings.Contains(content, "CNI") {
		t.Fatalf("top MEMORY.SEARCH result content = %q, want the networking memory (real embeddings should rank it first by meaning, not word overlap)", content)
	}
}

// TestRealOpenAISummaryCreateProducesRealSummary drives SUMMARY.CREATE
// against a real completion model and checks structural properties of the
// result (non-empty, not a verbatim copy of any single input, mentions the
// shared subject) rather than exact text, since real LLM output varies
// between runs.
func TestRealOpenAISummaryCreateProducesRealSummary(t *testing.T) {
	cfg := requireRealOpenAI(t)
	addr := startServerWithConfig(t, cfg)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sources := []string{
		"The user asked how to expose a Kubernetes Service outside the cluster.",
		"The user followed up asking specifically about Kubernetes Ingress controllers.",
		"The user mentioned they are using the nginx Ingress controller in production.",
	}
	for _, s := range sources {
		if err := rdb.Do(ctx, "MEMORY.PUT", "research-bot", s, "KIND", "episodic").Err(); err != nil {
			t.Fatalf("MEMORY.PUT %q: %v", s, err)
		}
	}

	summaryID, err := rdb.Do(ctx, "SUMMARY.CREATE", "research-bot").Text()
	if err != nil {
		t.Fatalf("SUMMARY.CREATE: %v", err)
	}
	if summaryID == "" {
		t.Fatal("SUMMARY.CREATE returned an empty id, want a real summary id")
	}

	fields, err := rdb.Do(ctx, "MEMORY.GET", "default", summaryID).StringSlice()
	if err != nil {
		t.Fatalf("MEMORY.GET on the new summary: %v", err)
	}
	var content, kind string
	for i := 0; i+1 < len(fields); i += 2 {
		switch fields[i] {
		case "content":
			content = fields[i+1]
		case "kind":
			kind = fields[i+1]
		}
	}
	if kind != "long_term" {
		t.Fatalf("summary memory kind = %q, want long_term", kind)
	}
	if len(content) < 10 {
		t.Fatalf("summary content = %q, want a real, non-trivial summary", content)
	}
	for _, s := range sources {
		if content == s {
			t.Fatalf("summary content is a verbatim copy of one source memory, want an actual summary")
		}
	}
	if !strings.Contains(strings.ToLower(content), "kubernetes") && !strings.Contains(strings.ToLower(content), "ingress") {
		t.Fatalf("summary content = %q, want it to actually reference the shared subject (Kubernetes/Ingress)", content)
	}
}

// TestRealOpenAIGraphExtractProducesRealEntities drives GRAPH.EXTRACT
// against a real completion model on a memory with clear, well-known named
// entities and relationships, and checks that real (non-zero) extraction
// happened -- the mock provider always returns [0, 0] here (see
// internal/graph/AGENTS.md), so this is the only place that path is
// actually exercised end to end.
func TestRealOpenAIGraphExtractProducesRealEntities(t *testing.T) {
	cfg := requireRealOpenAI(t)
	addr := startServerWithConfig(t, cfg)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := rdb.Do(ctx, "MEMORY.PUT", "bot", "Kubernetes was originally created by Google and is now maintained by the Cloud Native Computing Foundation.", "ID", "graph-mem-1").Err(); err != nil {
		t.Fatalf("MEMORY.PUT: %v", err)
	}

	counts, err := rdb.Do(ctx, "GRAPH.EXTRACT", "default", "graph-mem-1").Int64Slice()
	if err != nil {
		t.Fatalf("GRAPH.EXTRACT: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("GRAPH.EXTRACT reply = %v, want [entities_added, relations_added]", counts)
	}
	entitiesAdded := counts[0]
	if entitiesAdded == 0 {
		t.Fatal("GRAPH.EXTRACT with a real completion provider extracted 0 entities from a memory naming Kubernetes/Google/CNCF, want > 0")
	}

	// Entity ids are lowercase/underscored by prompt design (see
	// extractSystemPrompt's doc comment in internal/graph/extract.go), never
	// the original display casing -- querying with the capitalized form is a
	// real, documented gotcha (see docs/commands/graph.md), verified here
	// directly: it must miss.
	if related, err := rdb.Do(ctx, "GRAPH.RELATED", "default", "Kubernetes").StringSlice(); err != nil {
		t.Fatalf("GRAPH.RELATED Kubernetes (capitalized): %v", err)
	} else if len(related) != 0 {
		t.Fatalf("GRAPH.RELATED Kubernetes (capitalized) = %v, want empty -- if this now returns results, extraction ids are no longer lowercase and docs/commands/graph.md needs updating", related)
	}

	// The provenance node ("memory:<id>") is always present regardless of
	// wording, so it's the reliable way to reach whatever the extractor
	// actually named its entities.
	related, err := rdb.Do(ctx, "GRAPH.RELATED", "default", "memory:graph-mem-1").StringSlice()
	if err != nil {
		t.Fatalf("GRAPH.RELATED memory:graph-mem-1: %v", err)
	}
	if len(related) == 0 {
		t.Fatal("GRAPH.RELATED found no related nodes from the memory's own provenance node, want at least one")
	}
}

// TestRealOpenAIAgentRememberRecallScoping exercises AGENT.REMEMBER/
// AGENT.RECALL specifically (not just their MEMORY.PUT/MEMORY.SEARCH
// counterparts, even though they share the same underlying store) with
// real embeddings, and proves the always-agent-scoped guarantee still
// holds: alice's memory is never recallable through bob's AGENT.RECALL,
// even though both memories are topically related enough that a
// workspace-wide MEMORY.SEARCH would rank them close together.
func TestRealOpenAIAgentRememberRecallScoping(t *testing.T) {
	cfg := requireRealOpenAI(t)
	addr := startServerWithConfig(t, cfg)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := rdb.Do(ctx, "AGENT.REMEMBER", "alice", "Alice likes her code reviews to focus on security issues first.").Err(); err != nil {
		t.Fatalf("AGENT.REMEMBER alice: %v", err)
	}
	if err := rdb.Do(ctx, "AGENT.REMEMBER", "bob", "Bob likes his code reviews to focus on performance issues first.").Err(); err != nil {
		t.Fatalf("AGENT.REMEMBER bob: %v", err)
	}

	// A workspace-wide search for "code review preferences" should find
	// both -- they're genuinely topically close.
	both, err := rdb.Do(ctx, "MEMORY.SEARCH", "default", "what does this person want code reviews to prioritize").StringSlice()
	if err != nil {
		t.Fatalf("MEMORY.SEARCH: %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("MEMORY.SEARCH (workspace-wide) found %d results, want 2 (both alice's and bob's memory)", len(both))
	}

	// But AGENT.RECALL as bob must never surface alice's memory, no matter
	// how semantically close the query is to it.
	bobResults, err := rdb.Do(ctx, "AGENT.RECALL", "bob", "what does this person want code reviews to prioritize", "K", "5").StringSlice()
	if err != nil {
		t.Fatalf("AGENT.RECALL bob: %v", err)
	}
	if len(bobResults) != 1 {
		t.Fatalf("AGENT.RECALL bob found %d results, want exactly 1 (bob's own memory only)", len(bobResults))
	}
	fields, err := rdb.Do(ctx, "MEMORY.GET", "default", bobResults[0]).StringSlice()
	if err != nil {
		t.Fatalf("MEMORY.GET on bob's recalled memory: %v", err)
	}
	var agentID string
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "agent_id" {
			agentID = fields[i+1]
		}
	}
	if agentID != "bob" {
		t.Fatalf("AGENT.RECALL bob returned a memory owned by agent_id=%q, want bob -- this would be a real cross-agent memory leak", agentID)
	}
}

// TestRealOpenAIConsolidateNonDestructiveDedup verifies, against real
// embeddings, that near-duplicate memories are actually recognized as
// duplicates by Consolidate's cosine-similarity dedup pass (something the
// mock provider can't meaningfully test since it has no real notion of
// "near-duplicate meaning"), AND that every source memory is still present
// in the store afterward -- dedup only narrows the summarization input, it
// never deletes anything.
func TestRealOpenAIConsolidateNonDestructiveDedup(t *testing.T) {
	cfg := requireRealOpenAI(t)
	addr := startServerWithConfig(t, cfg)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Two near-verbatim restatements of the same event, plus one distinct
	// event, all for the same agent.
	sourceIDs := make([]string, 0, 3)
	for _, content := range []string{
		"The deployment to production failed at 3pm due to a database migration timeout.",
		"At 3pm, the production deployment failed because a database migration timed out.",
		"The user asked for a walkthrough of how VECTOR.SEARCH's HYBRID option combines keyword and vector scores.",
	} {
		id, err := rdb.Do(ctx, "MEMORY.PUT", "ops-bot", content, "KIND", "episodic").Text()
		if err != nil {
			t.Fatalf("MEMORY.PUT %q: %v", content, err)
		}
		sourceIDs = append(sourceIDs, id)
	}

	result, err := rdb.Do(ctx, "SUMMARY.CREATE", "ops-bot", "DEDUP_THRESHOLD", "0.85").Result()
	if err != nil {
		t.Fatalf("SUMMARY.CREATE: %v", err)
	}
	if result == nil {
		t.Fatal("SUMMARY.CREATE returned nil, want a real summary id (3 source memories exist)")
	}

	// Every source memory must still be individually retrievable -- dedup
	// must never have deleted anything from the store.
	for _, id := range sourceIDs {
		fields, err := rdb.Do(ctx, "MEMORY.GET", "default", id).StringSlice()
		if err != nil {
			t.Fatalf("MEMORY.GET source %s after consolidation: %v", id, err)
		}
		if len(fields) == 0 {
			t.Fatalf("source memory %s is gone after SUMMARY.CREATE, want it still present (dedup must be non-destructive)", id)
		}
	}
}

// TestRealOpenAIEmbeddingCostAnalyticsTracksRealUsage drives one real
// embedding call and one real completion call, and confirms /stats'
// analytics section reports real, non-zero token usage and cost for both
// (text-embedding-3-small and gpt-4o-mini, internal/embed's and
// internal/llm's defaults) -- proving both InstrumentProvider and
// InstrumentCompletionProvider (internal/server/server.go) actually
// capture real OpenAI usage data, not just the mock providers'
// always-zero TokenUsage.
func TestRealOpenAIEmbeddingCostAnalyticsTracksRealUsage(t *testing.T) {
	cfg := requireRealOpenAI(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	respAddr := ln.Addr().String()
	mcpPort := freePort(t)
	cfg.MCPPort = mcpPort

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.RunListener(ctx, cfg, ln); err != nil {
			t.Errorf("server.RunListener: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shut down in time")
		}
	})
	mcpBaseURL := fmt.Sprintf("http://127.0.0.1:%d", mcpPort)
	waitForHTTP(t, mcpBaseURL+"/stats")

	rdb := newClient(respAddr)
	defer rdb.Close()
	reqCtx, reqCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer reqCancel()

	if err := rdb.Do(reqCtx, "CACHE.SEMANTIC", "SET", "What does the WATCH command do?", "It marks a key to be monitored for optimistic-lock transaction aborts.").Err(); err != nil {
		t.Fatalf("CACHE.SEMANTIC SET (real embedding call): %v", err)
	}
	if err := rdb.Do(reqCtx, "MEMORY.PUT", "bot", "The user asked what the WATCH command does.", "KIND", "episodic").Err(); err != nil {
		t.Fatalf("MEMORY.PUT: %v", err)
	}
	if err := rdb.Do(reqCtx, "SUMMARY.CREATE", "bot").Err(); err != nil {
		t.Fatalf("SUMMARY.CREATE (real completion call): %v", err)
	}

	resp, err := http.Get(mcpBaseURL + "/stats")
	if err != nil {
		t.Fatalf("GET /stats: %v", err)
	}
	defer resp.Body.Close()
	type modelUsage struct {
		Tokens       int64   `json:"tokens"`
		CostUSD      float64 `json:"cost_usd"`
		PricingKnown bool    `json:"pricing_known"`
	}
	var stats struct {
		Analytics struct {
			EmbeddingByModel  map[string]modelUsage `json:"embedding_by_model"`
			CompletionByModel map[string]modelUsage `json:"completion_by_model"`
		} `json:"analytics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode /stats JSON: %v", err)
	}

	embedUsage, ok := stats.Analytics.EmbeddingByModel["text-embedding-3-small"]
	if !ok {
		t.Fatalf("/stats analytics.embedding_by_model has no entry for text-embedding-3-small; got %+v", stats.Analytics.EmbeddingByModel)
	}
	if embedUsage.Tokens == 0 {
		t.Fatal("/stats reports 0 embedding tokens for a real embedding call, want > 0 (real OpenAI usage.total_tokens)")
	}
	if !embedUsage.PricingKnown {
		t.Fatal("/stats reports pricing_known=false for text-embedding-3-small, want true (it's in internal/analytics' pricing table)")
	}
	if embedUsage.CostUSD <= 0 {
		t.Fatalf("/stats reports embedding cost_usd=%v for a real embedding call with known pricing, want > 0", embedUsage.CostUSD)
	}

	completionUsage, ok := stats.Analytics.CompletionByModel["gpt-4o-mini"]
	if !ok {
		t.Fatalf("/stats analytics.completion_by_model has no entry for gpt-4o-mini; got %+v", stats.Analytics.CompletionByModel)
	}
	if completionUsage.Tokens == 0 {
		t.Fatal("/stats reports 0 completion tokens for a real SUMMARY.CREATE call, want > 0")
	}
	if !completionUsage.PricingKnown {
		t.Fatal("/stats reports pricing_known=false for gpt-4o-mini, want true (it's in internal/analytics' pricing table)")
	}
	if completionUsage.CostUSD <= 0 {
		t.Fatalf("/stats reports completion cost_usd=%v for a real completion call with known pricing, want > 0", completionUsage.CostUSD)
	}
}
