package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/server"
)

// freePort picks an available TCP port by binding to :0 and immediately
// releasing it, so a caller-controlled server (server.Config.MCPPort binds
// its own listener rather than accepting one) can reuse the number. There's
// a narrow window where another process could grab it first, same as any
// "pick a free port" test helper.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// startServerWithMCP is like startServer but also enables the MCP/metrics
// HTTP listener on a free port, returning the RESP address and the
// "http://host:port" base URL for /metrics and /stats.
func startServerWithMCP(t *testing.T) (respAddr, mcpBaseURL string) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	respAddr = ln.Addr().String()
	mcpPort := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		cfg := server.Config{MaxConnections: 1000, MCPPort: mcpPort}
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

	mcpBaseURL = fmt.Sprintf("http://127.0.0.1:%d", mcpPort)
	waitForHTTP(t, mcpBaseURL+"/stats")
	return respAddr, mcpBaseURL
}

// waitForHTTP polls url briefly until it responds, since the MCP HTTP
// listener starts in a separate goroutine and may not be bound yet the
// instant RunListener returns control to the caller.
func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to become reachable", url)
}

func TestMetricsAndStatsEndpoints(t *testing.T) {
	respAddr, mcpBaseURL := startServerWithMCP(t)
	rdb := newClient(respAddr)
	defer rdb.Close()

	ctx := context.Background()
	if err := rdb.Do(ctx, "CACHE.SEMANTIC", "SET", "what is kubernetes", "an orchestrator").Err(); err != nil {
		t.Fatalf("CACHE.SEMANTIC SET: %v", err)
	}
	if err := rdb.Do(ctx, "CACHE.SEMANTIC", "GET", "what is kubernetes").Err(); err != nil {
		t.Fatalf("CACHE.SEMANTIC GET (hit): %v", err)
	}
	// A miss is a valid (nil) result, not a Go-redis error, so don't check
	// Err() here beyond redis.Nil.
	_ = rdb.Do(ctx, "CACHE.SEMANTIC", "GET", "totally unrelated prompt").Err()

	// /metrics: Prometheus text, should reflect the hit we just recorded.
	resp, err := http.Get(mcpBaseURL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", resp.StatusCode)
	}
	text := string(body)
	if !strings.Contains(text, "cachepot_semantic_cache_hits_total 1") {
		t.Fatalf("/metrics missing expected hit count; body:\n%s", text)
	}
	if !strings.Contains(text, "cachepot_semantic_cache_misses_total 1") {
		t.Fatalf("/metrics missing expected miss count; body:\n%s", text)
	}

	// /stats: JSON, same underlying data.
	resp, err = http.Get(mcpBaseURL + "/stats")
	if err != nil {
		t.Fatalf("GET /stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/stats status = %d, want 200", resp.StatusCode)
	}
	var stats struct {
		Caches map[string]struct {
			Hits   int64 `json:"hits"`
			Misses int64 `json:"misses"`
		} `json:"caches"`
		CommandsTotal int64 `json:"commands_total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode /stats JSON: %v", err)
	}
	if stats.Caches["semantic_cache"].Hits != 1 {
		t.Fatalf("/stats semantic_cache.hits = %d, want 1", stats.Caches["semantic_cache"].Hits)
	}
	if stats.Caches["semantic_cache"].Misses != 1 {
		t.Fatalf("/stats semantic_cache.misses = %d, want 1", stats.Caches["semantic_cache"].Misses)
	}
	if stats.CommandsTotal == 0 {
		t.Fatalf("/stats commands_total = 0, want > 0")
	}
}
