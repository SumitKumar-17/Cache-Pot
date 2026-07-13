package mcp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/consolidate"
	"github.com/SumitKumar-17/cache-pot/internal/embed"
	"github.com/SumitKumar-17/cache-pot/internal/llm"
	"github.com/SumitKumar-17/cache-pot/internal/mcp"
	"github.com/SumitKumar-17/cache-pot/internal/memory"
	"github.com/SumitKumar-17/cache-pot/internal/observability"
	"github.com/SumitKumar-17/cache-pot/internal/semantic"
	"github.com/SumitKumar-17/cache-pot/internal/server/resp"
	"github.com/SumitKumar-17/cache-pot/internal/toolcache"
	"github.com/SumitKumar-17/cache-pot/internal/vector"
)

// testEnv bundles the real (never mocked, except for the embedding
// provider itself) shared instances and a connected MCP client session for
// driving the server through the actual streamable-HTTP wire protocol.
type testEnv struct {
	t   *testing.T
	ts  *httptest.Server
	cs  *sdkmcp.ClientSession
	ctx context.Context

	semanticCache      *semantic.SemanticCache
	promptCache        *semantic.PromptCache
	toolCache          *toolcache.ToolCache
	vectorStore        *vector.Store
	memoryStore        *memory.Store
	completionProvider llm.CompletionProvider
	consolidator       *consolidate.Consolidator
	metrics            *observability.Metrics
	analytics          *analytics.Tracker
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	semanticCache := semantic.New(embed.NewMock(8))
	promptCache := semantic.NewPromptCache()
	toolCache := toolcache.New()
	vectorStore := vector.New()
	memoryStore := memory.New(embed.NewMock(8))
	completionProvider := llm.NewMock()
	consolidator := consolidate.New(memoryStore, completionProvider)
	tracker := analytics.New()
	metrics := observability.NewMetrics()

	srv := mcp.New(semanticCache, promptCache, toolCache, vectorStore, memoryStore, consolidator, metrics, tracker)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx := context.Background()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "cachepot-test-client", Version: "0.0.1"}, nil)
	transport := &sdkmcp.StreamableClientTransport{Endpoint: ts.URL}
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	return &testEnv{
		t:                  t,
		ts:                 ts,
		cs:                 cs,
		ctx:                ctx,
		semanticCache:      semanticCache,
		promptCache:        promptCache,
		toolCache:          toolCache,
		vectorStore:        vectorStore,
		memoryStore:        memoryStore,
		completionProvider: completionProvider,
		consolidator:       consolidator,
		metrics:            metrics,
		analytics:          tracker,
	}
}

// call invokes tool with args and decodes its structured output into out
// (a pointer), failing the test on any protocol-level or tool-level error.
func (e *testEnv) call(tool string, args map[string]any, out any) {
	e.t.Helper()
	res, err := e.cs.CallTool(e.ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		e.t.Fatalf("CallTool(%s): %v", tool, err)
	}
	if res.IsError {
		e.t.Fatalf("CallTool(%s) returned a tool error: %+v", tool, res.Content)
	}
	if out == nil {
		return
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		e.t.Fatalf("marshal structured content for %s: %v", tool, err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		e.t.Fatalf("unmarshal structured content for %s: %v", tool, err)
	}
}

func TestListTools(t *testing.T) {
	env := newTestEnv(t)

	res, err := env.cs.ListTools(env.ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	want := []string{
		"cache_prompt_get",
		"cache_prompt_set",
		"cache_semantic_get",
		"cache_semantic_set",
		"consolidate",
		"delete_vector",
		"find_similar",
		"recall",
		"remember",
		"store_vector",
		"tool_cache_get",
		"tool_cache_set",
	}
	var got []string
	for _, tl := range res.Tools {
		got = append(got, tl.Name)
		if tl.Description == "" {
			t.Errorf("tool %s has no description", tl.Name)
		}
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("got %d tools %v, want %d tools %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool[%d] = %q, want %q (full list: %v)", i, got[i], want[i], got)
		}
	}
}

func TestStoreVectorThenFindSimilar(t *testing.T) {
	env := newTestEnv(t)

	env.call("store_vector", map[string]any{
		"namespace": "docs",
		"id":        "doc-1",
		"vector":    []float64{1, 0, 0, 0, 0, 0, 0, 0},
		"metadata":  map[string]string{"lang": "en"},
	}, nil)
	env.call("store_vector", map[string]any{
		"namespace": "docs",
		"id":        "doc-2",
		"vector":    []float64{0, 1, 0, 0, 0, 0, 0, 0},
	}, nil)

	var out mcp.FindSimilarOutput
	env.call("find_similar", map[string]any{
		"namespace": "docs",
		"vector":    []float64{1, 0, 0, 0, 0, 0, 0, 0},
		"k":         float64(1),
	}, &out)

	if len(out.Results) != 1 {
		t.Fatalf("find_similar: got %d results, want 1: %+v", len(out.Results), out.Results)
	}
	if out.Results[0].ID != "doc-1" {
		t.Errorf("find_similar: got id %q, want doc-1", out.Results[0].ID)
	}
	if out.Results[0].Score < 0.99 {
		t.Errorf("find_similar: got score %v for an identical vector, want ~1.0", out.Results[0].Score)
	}

	// Confirm directly against the shared Store instance too, proving MCP
	// wrote through to the exact same object rather than a private copy.
	direct := env.vectorStore.Search("docs", []float32{1, 0, 0, 0, 0, 0, 0, 0}, 1, vector.Cosine, nil, nil)
	if len(direct) != 1 || direct[0].ID != "doc-1" {
		t.Fatalf("direct Store.Search after MCP store_vector: got %+v, want [doc-1]", direct)
	}

	var delOut mcp.DeleteVectorOutput
	env.call("delete_vector", map[string]any{"namespace": "docs", "id": "doc-1"}, &delOut)
	if !delOut.Deleted {
		t.Errorf("delete_vector: got Deleted=false, want true")
	}
	if again := env.vectorStore.Delete("docs", "doc-1"); again {
		// second delete via the shared instance should now report false
		t.Errorf("doc-1 should already be gone from the shared Store after MCP delete_vector")
	}
}

func TestCacheSemanticSetThenGet(t *testing.T) {
	env := newTestEnv(t)

	env.call("cache_semantic_set", map[string]any{
		"prompt":   "What is the capital of France?",
		"response": "Paris",
	}, nil)

	var out mcp.CacheSemanticGetOutput
	env.call("cache_semantic_get", map[string]any{
		"prompt": "What is the capital of France?",
	}, &out)

	if !out.Found {
		t.Fatalf("cache_semantic_get: got Found=false, want true")
	}
	if out.Response != "Paris" {
		t.Errorf("cache_semantic_get: got response %q, want %q", out.Response, "Paris")
	}

	// A completely unrelated prompt should miss.
	var miss mcp.CacheSemanticGetOutput
	env.call("cache_semantic_get", map[string]any{
		"prompt": "How do I bake a sourdough loaf?",
	}, &miss)
	if miss.Found {
		t.Errorf("cache_semantic_get: unrelated prompt unexpectedly hit with response %q", miss.Response)
	}
}

func TestCachePromptSetThenGet(t *testing.T) {
	env := newTestEnv(t)

	env.call("cache_prompt_set", map[string]any{
		"template":       "Summarize: {{.text}}",
		"variables_json": `{"text":"hello world"}`,
		"model":          "gpt-test",
		"response":       "A greeting.",
	}, nil)

	var out mcp.CachePromptGetOutput
	env.call("cache_prompt_get", map[string]any{
		"template":       "Summarize: {{.text}}",
		"variables_json": `{"text":"hello world"}`,
		"model":          "gpt-test",
	}, &out)
	if !out.Found || out.Response != "A greeting." {
		t.Fatalf("cache_prompt_get: got %+v, want found=true response=%q", out, "A greeting.")
	}
}

func TestCacheSemanticCostRecordsSavingsOnHit(t *testing.T) {
	env := newTestEnv(t)

	env.call("cache_semantic_set", map[string]any{
		"prompt":   "What is Kubernetes?",
		"response": "K8s is a container orchestrator.",
		"cost":     0.01,
	}, nil)

	var out mcp.CacheSemanticGetOutput
	env.call("cache_semantic_get", map[string]any{"prompt": "What is Kubernetes?"}, &out)
	if !out.Found {
		t.Fatal("expected a hit")
	}

	snap := env.analytics.Snapshot()
	if snap.MoneySavedTotalUSD != 0.01 {
		t.Fatalf("MoneySavedTotalUSD = %v, want 0.01", snap.MoneySavedTotalUSD)
	}
}

func TestCacheSemanticNoCostRecordsNoSavingsViaMCP(t *testing.T) {
	env := newTestEnv(t)

	env.call("cache_semantic_set", map[string]any{
		"prompt":   "What is Kubernetes?",
		"response": "K8s is a container orchestrator.",
	}, nil)

	var out mcp.CacheSemanticGetOutput
	env.call("cache_semantic_get", map[string]any{"prompt": "What is Kubernetes?"}, &out)
	if !out.Found {
		t.Fatal("expected a hit")
	}

	snap := env.analytics.Snapshot()
	if snap.MoneySavedTotalUSD != 0 {
		t.Fatalf("MoneySavedTotalUSD = %v, want exactly 0 when no cost was ever supplied", snap.MoneySavedTotalUSD)
	}
}

func TestCachePromptCostRecordsSavingsOnHit(t *testing.T) {
	env := newTestEnv(t)

	env.call("cache_prompt_set", map[string]any{
		"template":       "Summarize: {{.text}}",
		"variables_json": `{"text":"hello world"}`,
		"model":          "gpt-test",
		"response":       "A greeting.",
		"cost":           0.03,
	}, nil)

	var out mcp.CachePromptGetOutput
	env.call("cache_prompt_get", map[string]any{
		"template":       "Summarize: {{.text}}",
		"variables_json": `{"text":"hello world"}`,
		"model":          "gpt-test",
	}, &out)
	if !out.Found {
		t.Fatal("expected a hit")
	}

	snap := env.analytics.Snapshot()
	if snap.MoneySavedTotalUSD != 0.03 {
		t.Fatalf("MoneySavedTotalUSD = %v, want 0.03", snap.MoneySavedTotalUSD)
	}
}

func TestToolCacheSetThenGet(t *testing.T) {
	env := newTestEnv(t)

	env.call("tool_cache_set", map[string]any{
		"tool_name": "github.search",
		"args_json": `{"q":"cache-pot"}`,
		"result":    `{"stars":42}`,
	}, nil)

	var out mcp.ToolCacheGetOutput
	env.call("tool_cache_get", map[string]any{
		"tool_name": "github.search",
		"args_json": `{"q":"cache-pot"}`,
	}, &out)
	if !out.Found || out.Result != `{"stars":42}` {
		t.Fatalf("tool_cache_get: got %+v", out)
	}
}

func TestRememberThenRecall(t *testing.T) {
	env := newTestEnv(t)

	var rememberOut mcp.RememberOutput
	env.call("remember", map[string]any{
		"agent_id": "agent-1",
		"content":  "the user prefers dark mode",
	}, &rememberOut)
	if rememberOut.ID == "" {
		t.Fatalf("remember: got empty id")
	}

	var recallOut mcp.RecallOutput
	env.call("recall", map[string]any{
		"agent_id": "agent-1",
		"query":    "the user prefers dark mode",
	}, &recallOut)

	found := false
	for _, r := range recallOut.Results {
		if r.ID == rememberOut.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("recall: remembered id %q not found in results %+v", rememberOut.ID, recallOut.Results)
	}

	// Confirm directly against the shared Store instance too, proving MCP
	// wrote through to the exact same object rather than a private copy.
	direct, foundDirect, err := env.memoryStore.Get(env.ctx, "default", rememberOut.ID)
	if err != nil {
		t.Fatalf("direct memoryStore.Get: %v", err)
	}
	if !foundDirect || direct.Content != "the user prefers dark mode" {
		t.Fatalf("direct memoryStore.Get after MCP remember: got %+v, foundDirect=%v", direct, foundDirect)
	}
}

// TestRecallDoesNotLeakOtherAgentsMemories proves recall has the same
// no-cross-agent-leak guarantee as AGENT.RECALL: even when another agent's
// memory is semantically identical (same content) and lives in the same
// workspace, a recall for a different agent must never surface it.
func TestRecallDoesNotLeakOtherAgentsMemories(t *testing.T) {
	env := newTestEnv(t)

	var ownOut mcp.RememberOutput
	env.call("remember", map[string]any{
		"agent_id": "agent-a",
		"content":  "shared topic: database migrations",
	}, &ownOut)

	var otherOut mcp.RememberOutput
	env.call("remember", map[string]any{
		"agent_id": "agent-b",
		"content":  "shared topic: database migrations",
	}, &otherOut)

	var recallOut mcp.RecallOutput
	env.call("recall", map[string]any{
		"agent_id": "agent-a",
		"query":    "database migrations",
	}, &recallOut)

	if len(recallOut.Results) != 1 {
		t.Fatalf("recall(agent-a): got %d results, want exactly 1: %+v", len(recallOut.Results), recallOut.Results)
	}
	if recallOut.Results[0].ID != ownOut.ID {
		t.Fatalf("recall(agent-a): got id %q, want agent-a's own memory %q", recallOut.Results[0].ID, ownOut.ID)
	}
	for _, r := range recallOut.Results {
		if r.ID == otherOut.ID {
			t.Fatalf("recall(agent-a) leaked agent-b's memory %q", otherOut.ID)
		}
	}
}

// TestConsolidateTool exercises the consolidate MCP tool end to end: it
// remembers a few near-identical episodic memories plus one distinct one
// for an agent, consolidates them, and confirms the result matches
// SUMMARY.CREATE's own documented behavior (source/deduped counts, and the
// new summary being a real, fetchable long_term memory via the shared
// memoryStore instance -- proving the tool wrote through to the same store,
// not a private copy).
func TestConsolidateTool(t *testing.T) {
	env := newTestEnv(t)

	remember := func(agentID, content, kind string) {
		env.call("remember", map[string]any{
			"agent_id": agentID,
			"content":  content,
			"kind":     kind,
		}, nil)
	}
	remember("agent-1", "user completed the onboarding flow", "episodic")
	remember("agent-1", "User completed the onboarding flow", "episodic")
	remember("agent-1", "the weather in paris is nice today", "episodic")

	var out mcp.ConsolidateOutput
	env.call("consolidate", map[string]any{"agent_id": "agent-1"}, &out)

	if out.SourceCount != 3 {
		t.Fatalf("consolidate: SourceCount = %d, want 3", out.SourceCount)
	}
	if out.DedupedCount != 2 {
		t.Fatalf("consolidate: DedupedCount = %d, want 2 (2 near-duplicates collapsed to 1, plus 1 distinct)", out.DedupedCount)
	}
	if out.SummaryID == "" {
		t.Fatal("consolidate: got empty summary_id, want a real id")
	}

	direct, found, err := env.memoryStore.Get(env.ctx, "default", out.SummaryID)
	if err != nil {
		t.Fatalf("direct memoryStore.Get: %v", err)
	}
	if !found {
		t.Fatalf("direct memoryStore.Get: summary id %q not found in the shared store", out.SummaryID)
	}
	if direct.Kind != memory.LongTerm {
		t.Fatalf("summary Kind = %v, want LongTerm", direct.Kind)
	}
}

// TestConsolidateToolNothingToSummarize confirms the empty-input case
// reports an empty summary_id and zero counts, not a tool error.
func TestConsolidateToolNothingToSummarize(t *testing.T) {
	env := newTestEnv(t)

	var out mcp.ConsolidateOutput
	env.call("consolidate", map[string]any{"agent_id": "agent-with-no-memories"}, &out)

	if out.SummaryID != "" {
		t.Fatalf("consolidate (no memories): SummaryID = %q, want empty", out.SummaryID)
	}
	if out.SourceCount != 0 || out.DedupedCount != 0 {
		t.Fatalf("consolidate (no memories): got %+v, want SourceCount=0 DedupedCount=0", out)
	}
}

// TestSharedStateWithRESP is the whole point of this package: it proves
// there is no adapter layer or second storage between the MCP server and
// the RESP server. It builds a real resp.Deps sharing the exact same
// SemanticCache instance the MCP server above was constructed with, drives
// a real RESP connection (via HandleConn over a net.Pipe) with real RESP
// wire-protocol bytes, and checks that a value written through one
// protocol is visible through the other, in both directions.
func TestSharedStateWithRESP(t *testing.T) {
	env := newTestEnv(t)

	registry := resp.NewRegistry()
	resp.RegisterAll(registry)
	deps := &resp.Deps{
		Auth:               auth.New(""),
		Metrics:            env.metrics,
		Logger:             observability.NewLogger(slog.LevelError),
		PubSub:             resp.NewPubSub(),
		Registry:           registry,
		SemanticCache:      env.semanticCache,
		PromptCache:        env.promptCache,
		ToolCache:          env.toolCache,
		VectorStore:        env.vectorStore,
		MemoryStore:        env.memoryStore,
		CompletionProvider: env.completionProvider,
		Consolidator:       env.consolidator,
	}

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	go resp.HandleConn(serverConn, deps)

	respClient := newInlineRESPClient(t, clientConn)

	// RESP write -> MCP read.
	respClient.send("CACHE.SEMANTIC SET written-by-resp resp-response")
	if got := respClient.recvLine(); got != "+OK" {
		t.Fatalf("CACHE.SEMANTIC SET reply = %q, want +OK", got)
	}

	var out mcp.CacheSemanticGetOutput
	env.call("cache_semantic_get", map[string]any{"prompt": "written-by-resp"}, &out)
	if !out.Found || out.Response != "resp-response" {
		t.Fatalf("MCP cache_semantic_get after RESP SET: got %+v, want found=true response=resp-response", out)
	}

	// MCP write -> RESP read.
	env.call("cache_semantic_set", map[string]any{
		"prompt":   "written-by-mcp",
		"response": "mcp-response",
	}, nil)

	respClient.send("CACHE.SEMANTIC GET written-by-mcp")
	if got := respClient.recvBulk(); got != "mcp-response" {
		t.Fatalf("RESP CACHE.SEMANTIC GET after MCP SET: got %q, want mcp-response", got)
	}

	// Memory: RESP AGENT.REMEMBER -> MCP recall.
	respClient.send("AGENT.REMEMBER cross-agent remembered-via-resp")
	memID := respClient.recvBulk()
	if memID == "" {
		t.Fatalf("AGENT.REMEMBER reply: got empty id")
	}

	var recallOut mcp.RecallOutput
	env.call("recall", map[string]any{
		"agent_id": "cross-agent",
		"query":    "remembered-via-resp",
	}, &recallOut)
	foundViaMCP := false
	for _, r := range recallOut.Results {
		if r.ID == memID {
			foundViaMCP = true
		}
	}
	if !foundViaMCP {
		t.Fatalf("MCP recall after RESP AGENT.REMEMBER: memory id %q not found in %+v", memID, recallOut.Results)
	}

	// Memory: MCP remember -> RESP AGENT.RECALL.
	var rememberOut mcp.RememberOutput
	env.call("remember", map[string]any{
		"agent_id": "cross-agent",
		"content":  "remembered-via-mcp",
	}, &rememberOut)

	respClient.send("AGENT.RECALL cross-agent remembered-via-mcp")
	ids := respClient.recvArrayBulkStrings()
	foundViaRESP := false
	for _, id := range ids {
		if id == rememberOut.ID {
			foundViaRESP = true
		}
	}
	if !foundViaRESP {
		t.Fatalf("RESP AGENT.RECALL after MCP remember: memory id %q not found in %v", rememberOut.ID, ids)
	}

	// Consolidation: RESP AGENT.REMEMBER (episodic) -> MCP consolidate ->
	// RESP MEMORY.GET.
	respClient.send("AGENT.REMEMBER consolidate-agent episodic-fact-one KIND episodic")
	respClient.recvBulk()
	respClient.send("AGENT.REMEMBER consolidate-agent episodic-fact-two KIND episodic")
	respClient.recvBulk()

	var consolidateOut mcp.ConsolidateOutput
	env.call("consolidate", map[string]any{"agent_id": "consolidate-agent"}, &consolidateOut)
	if consolidateOut.SourceCount != 2 {
		t.Fatalf("MCP consolidate after RESP AGENT.REMEMBER: SourceCount = %d, want 2", consolidateOut.SourceCount)
	}
	if consolidateOut.SummaryID == "" {
		t.Fatalf("MCP consolidate after RESP AGENT.REMEMBER: got empty summary_id")
	}

	respClient.send("MEMORY.GET default " + consolidateOut.SummaryID)
	fields := respClient.recvArrayBulkStrings()
	foundLongTerm := false
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "kind" && fields[i+1] == "long_term" {
			foundLongTerm = true
		}
	}
	if !foundLongTerm {
		t.Fatalf("RESP MEMORY.GET after MCP consolidate: fields %v, want a kind=long_term pair", fields)
	}

	// And the reverse direction: RESP SUMMARY.CREATE -> MCP-visible via the
	// shared memoryStore instance.
	respClient.send("SUMMARY.CREATE consolidate-agent")
	summaryID := respClient.recvBulk()
	if summaryID == "" {
		t.Fatalf("RESP SUMMARY.CREATE: got empty id")
	}
	directSummary, foundDirect, err := env.memoryStore.Get(env.ctx, "default", summaryID)
	if err != nil {
		t.Fatalf("direct memoryStore.Get after RESP SUMMARY.CREATE: %v", err)
	}
	if !foundDirect || directSummary.Kind != memory.LongTerm {
		t.Fatalf("direct memoryStore.Get after RESP SUMMARY.CREATE: got %+v, foundDirect=%v", directSummary, foundDirect)
	}
}

// inlineRESPClient is a minimal RESP2 client good enough for this test: it
// writes inline commands (plain whitespace-separated text lines, which
// resp.ReadCommand accepts) and parses simple-string/bulk-string replies.
type inlineRESPClient struct {
	t    *testing.T
	conn net.Conn
	buf  []byte
}

func newInlineRESPClient(t *testing.T, conn net.Conn) *inlineRESPClient {
	return &inlineRESPClient{t: t, conn: conn}
}

func (c *inlineRESPClient) send(line string) {
	c.t.Helper()
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		c.t.Fatalf("write %q: %v", line, err)
	}
}

// readLine reads up to and including the next "\r\n", buffering any
// trailing bytes read past it for the next call.
func (c *inlineRESPClient) readLine() string {
	c.t.Helper()
	for {
		if i := indexCRLF(c.buf); i >= 0 {
			line := string(c.buf[:i])
			c.buf = c.buf[i+2:]
			return line
		}
		tmp := make([]byte, 4096)
		_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
		n, err := c.conn.Read(tmp)
		if err != nil {
			c.t.Fatalf("read: %v", err)
		}
		c.buf = append(c.buf, tmp[:n]...)
	}
}

func indexCRLF(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '\r' && b[i+1] == '\n' {
			return i
		}
	}
	return -1
}

// recvLine reads one simple-string/error/integer style reply line verbatim
// (e.g. "+OK").
func (c *inlineRESPClient) recvLine() string {
	return c.readLine()
}

// recvBulk reads one RESP2 bulk-string reply and returns its payload (or
// "" for a null bulk string).
func (c *inlineRESPClient) recvBulk() string {
	c.t.Helper()
	header := c.readLine()
	if len(header) == 0 || header[0] != '$' {
		c.t.Fatalf("expected bulk-string header, got %q", header)
	}
	if header == "$-1" {
		return ""
	}
	// The payload (plus its own trailing CRLF) may not be fully buffered
	// yet; readLine's buffering loop handles that transparently since we
	// just ask it for another line boundary.
	return c.readLine()
}

// recvArrayBulkStrings reads one RESP2 array reply composed entirely of
// bulk strings (e.g. AGENT.RECALL's reply) and returns their payloads in
// order. A nil array ("*-1") or empty array ("*0") both yield an empty
// slice.
func (c *inlineRESPClient) recvArrayBulkStrings() []string {
	c.t.Helper()
	header := c.readLine()
	if len(header) == 0 || header[0] != '*' {
		c.t.Fatalf("expected array header, got %q", header)
	}
	if header == "*-1" || header == "*0" {
		return nil
	}
	n, err := strconv.Atoi(header[1:])
	if err != nil {
		c.t.Fatalf("array header %q: %v", header, err)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = c.recvBulk()
	}
	return out
}
