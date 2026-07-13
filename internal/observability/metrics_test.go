package observability

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCacheHitMissCounters(t *testing.T) {
	m := NewMetrics()
	m.SemanticCacheHit()
	m.SemanticCacheHit()
	m.SemanticCacheMiss()
	m.PromptCacheMiss()
	m.ToolCacheHit()

	snap := m.Snapshot()
	if snap.SemanticCache.Hits != 2 || snap.SemanticCache.Misses != 1 {
		t.Fatalf("semantic cache stats = %+v, want 2 hits/1 miss", snap.SemanticCache)
	}
	if got, want := snap.SemanticCache.HitRate(), 2.0/3.0; got != want {
		t.Fatalf("semantic cache hit rate = %v, want %v", got, want)
	}
	if snap.PromptCache.Hits != 0 || snap.PromptCache.Misses != 1 {
		t.Fatalf("prompt cache stats = %+v, want 0 hits/1 miss", snap.PromptCache)
	}
	if snap.ToolCache.Hits != 1 || snap.ToolCache.Misses != 0 {
		t.Fatalf("tool cache stats = %+v, want 1 hit/0 misses", snap.ToolCache)
	}
	empty := CacheStats{}
	if got := empty.HitRate(); got != 0 {
		t.Fatalf("HitRate() with no lookups = %v, want 0 (not NaN)", got)
	}
}

func TestVectorMemoryCounters(t *testing.T) {
	m := NewMetrics()
	m.VectorSearchPerformed()
	m.VectorSearchPerformed()
	m.MemoryRead()
	m.MemoryWrite()
	m.MemoryWrite()

	snap := m.Snapshot()
	if snap.VectorSearchesTotal != 2 {
		t.Fatalf("VectorSearchesTotal = %d, want 2", snap.VectorSearchesTotal)
	}
	if snap.MemoryReadsTotal != 1 {
		t.Fatalf("MemoryReadsTotal = %d, want 1", snap.MemoryReadsTotal)
	}
	if snap.MemoryWritesTotal != 2 {
		t.Fatalf("MemoryWritesTotal = %d, want 2", snap.MemoryWritesTotal)
	}
}

func TestMCPCallsByTool(t *testing.T) {
	m := NewMetrics()
	m.MCPCallRecorded("remember")
	m.MCPCallRecorded("remember")
	m.MCPCallRecorded("recall")

	snap := m.Snapshot()
	if snap.MCPCallsTotal != 3 {
		t.Fatalf("MCPCallsTotal = %d, want 3", snap.MCPCallsTotal)
	}
	if snap.MCPCallsByTool["remember"] != 2 || snap.MCPCallsByTool["recall"] != 1 {
		t.Fatalf("MCPCallsByTool = %v, want remember:2 recall:1", snap.MCPCallsByTool)
	}
}

func TestCommandLatency(t *testing.T) {
	m := NewMetrics()
	m.RecordCommandLatency("strings", 10*time.Millisecond)
	m.RecordCommandLatency("strings", 30*time.Millisecond)
	m.RecordCommandLatency("hash", 5*time.Millisecond)

	snap := m.Snapshot()
	byFamily := map[string]LatencyStats{}
	for _, l := range snap.Latency {
		byFamily[l.Family] = l
	}
	strs, ok := byFamily["strings"]
	if !ok {
		t.Fatalf("no latency stats recorded for family %q", "strings")
	}
	if strs.Count != 2 {
		t.Fatalf("strings.Count = %d, want 2", strs.Count)
	}
	wantAvg := int64(20 * time.Millisecond)
	if strs.AvgNanos != wantAvg {
		t.Fatalf("strings.AvgNanos = %d, want %d", strs.AvgNanos, wantAvg)
	}
	wantMax := int64(30 * time.Millisecond)
	if strs.MaxNanos != wantMax {
		t.Fatalf("strings.MaxNanos = %d, want %d", strs.MaxNanos, wantMax)
	}
	if byFamily["hash"].Count != 1 {
		t.Fatalf("hash.Count = %d, want 1", byFamily["hash"].Count)
	}
}

func TestConcurrentCounters(t *testing.T) {
	m := NewMetrics()
	var wg sync.WaitGroup
	const n = 200
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.SemanticCacheHit()
			m.MCPCallRecorded("recall")
			m.RecordCommandLatency("vector", time.Microsecond)
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	if snap.SemanticCache.Hits != n {
		t.Fatalf("SemanticCache.Hits = %d, want %d", snap.SemanticCache.Hits, n)
	}
	if snap.MCPCallsByTool["recall"] != n {
		t.Fatalf("MCPCallsByTool[recall] = %d, want %d", snap.MCPCallsByTool["recall"], n)
	}
}

// fakeProvider is a minimal embed.Provider double for testing
// InstrumentProvider without any real network/mock-hashing behavior.
type fakeProvider struct {
	err error
}

func (f *fakeProvider) Embed(context.Context, string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []float32{1, 2, 3}, nil
}

func (f *fakeProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 2, 3}
	}
	return out, nil
}

func (f *fakeProvider) Dimensions() int { return 3 }
func (f *fakeProvider) Name() string    { return "fake" }

func TestInstrumentProviderSuccess(t *testing.T) {
	m := NewMetrics()
	p := InstrumentProvider(&fakeProvider{}, m)

	if _, err := p.Embed(context.Background(), "hello"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if _, err := p.EmbedBatch(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	snap := m.Snapshot()
	if snap.EmbeddingCallsTotal != 2 {
		t.Fatalf("EmbeddingCallsTotal = %d, want 2", snap.EmbeddingCallsTotal)
	}
	if snap.EmbeddingCallsErrors != 0 {
		t.Fatalf("EmbeddingCallsErrors = %d, want 0", snap.EmbeddingCallsErrors)
	}
	if snap.EmbeddingCallsInFlight != 0 {
		t.Fatalf("EmbeddingCallsInFlight = %d, want 0 after completion", snap.EmbeddingCallsInFlight)
	}
	if got := p.Dimensions(); got != 3 {
		t.Fatalf("Dimensions() = %d, want 3", got)
	}
	if got := p.Name(); got != "fake" {
		t.Fatalf("Name() = %q, want %q", got, "fake")
	}
}

func TestInstrumentProviderError(t *testing.T) {
	m := NewMetrics()
	p := InstrumentProvider(&fakeProvider{err: errors.New("boom")}, m)

	if _, err := p.Embed(context.Background(), "hello"); err == nil {
		t.Fatal("expected an error")
	}

	snap := m.Snapshot()
	if snap.EmbeddingCallsTotal != 1 {
		t.Fatalf("EmbeddingCallsTotal = %d, want 1", snap.EmbeddingCallsTotal)
	}
	if snap.EmbeddingCallsErrors != 1 {
		t.Fatalf("EmbeddingCallsErrors = %d, want 1", snap.EmbeddingCallsErrors)
	}
	if snap.EmbeddingCallsInFlight != 0 {
		t.Fatalf("EmbeddingCallsInFlight = %d, want 0 even after an error", snap.EmbeddingCallsInFlight)
	}
}

func TestMetricsHandlerRendersPrometheusText(t *testing.T) {
	m := NewMetrics()
	m.SemanticCacheHit()
	m.MCPCallRecorded("remember")
	m.RecordCommandLatency("strings", 5*time.Millisecond)

	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE cachepot_semantic_cache_hits_total counter",
		"cachepot_semantic_cache_hits_total 1",
		`cachepot_mcp_calls_by_tool_total{tool="remember"} 1`,
		`cachepot_commands_by_family_total{family="strings"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics output missing %q; full output:\n%s", want, body)
		}
	}
}

func TestStatsHandlerRendersJSON(t *testing.T) {
	m := NewMetrics()
	m.SemanticCacheHit()
	m.SemanticCacheMiss()

	rec := httptest.NewRecorder()
	StatsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/stats", nil))

	body := rec.Body.String()
	if !strings.Contains(body, `"semantic_cache"`) {
		t.Fatalf("stats JSON missing semantic_cache key; body:\n%s", body)
	}
	if !strings.Contains(body, `"hit_rate":0.5`) {
		t.Fatalf("stats JSON missing expected hit_rate; body:\n%s", body)
	}
}
