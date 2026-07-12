package mcp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/embed"
	"github.com/SumitKumar-17/cache-pot/internal/mcp"
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

	semanticCache *semantic.SemanticCache
	promptCache   *semantic.PromptCache
	toolCache     *toolcache.ToolCache
	vectorStore   *vector.Store
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	semanticCache := semantic.New(embed.NewMock(8))
	promptCache := semantic.NewPromptCache()
	toolCache := toolcache.New()
	vectorStore := vector.New()

	srv := mcp.New(semanticCache, promptCache, toolCache, vectorStore)
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
		t:             t,
		ts:            ts,
		cs:            cs,
		ctx:           ctx,
		semanticCache: semanticCache,
		promptCache:   promptCache,
		toolCache:     toolCache,
		vectorStore:   vectorStore,
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
		"delete_vector",
		"find_similar",
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
		Auth:          auth.New(""),
		Metrics:       observability.NewMetrics(),
		Logger:        observability.NewLogger(slog.LevelError),
		PubSub:        resp.NewPubSub(),
		Registry:      registry,
		SemanticCache: env.semanticCache,
		PromptCache:   env.promptCache,
		ToolCache:     env.toolCache,
		VectorStore:   env.vectorStore,
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
