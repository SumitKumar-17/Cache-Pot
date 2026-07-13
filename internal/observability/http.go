package observability

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// MetricsHandler renders m.Snapshot() as Prometheus text exposition format
// (hand-rolled: "# HELP"/"# TYPE" comment lines plus "metric_name value"
// lines -- no prometheus/client_golang dependency, matching this project's
// existing precedent of stdlib-only instrumentation code).
func MetricsHandler(m *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snap := m.Snapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		counter := func(name, help string, value int64) {
			fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, value)
		}
		gauge := func(name, help string, value int64) {
			fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, value)
		}

		counter("cachepot_connections_total", "Total accepted connections.", snap.ConnectionsTotal)
		gauge("cachepot_connections_active", "Currently open connections.", snap.ConnectionsActive)
		counter("cachepot_connections_rejected_total", "Connections rejected (max-connections reached).", snap.ConnectionsRejected)
		counter("cachepot_commands_total", "Total dispatched RESP commands.", snap.CommandsTotal)
		counter("cachepot_errors_total", "Total error replies sent to clients.", snap.ErrorsTotal)

		counter("cachepot_semantic_cache_hits_total", "CACHE.SEMANTIC GET hits.", snap.SemanticCache.Hits)
		counter("cachepot_semantic_cache_misses_total", "CACHE.SEMANTIC GET misses.", snap.SemanticCache.Misses)
		counter("cachepot_prompt_cache_hits_total", "CACHE.PROMPT GET hits.", snap.PromptCache.Hits)
		counter("cachepot_prompt_cache_misses_total", "CACHE.PROMPT GET misses.", snap.PromptCache.Misses)
		counter("cachepot_tool_cache_hits_total", "TOOL.CACHE GET hits.", snap.ToolCache.Hits)
		counter("cachepot_tool_cache_misses_total", "TOOL.CACHE GET misses.", snap.ToolCache.Misses)

		counter("cachepot_vector_searches_total", "VECTOR.SEARCH invocations.", snap.VectorSearchesTotal)
		counter("cachepot_memory_reads_total", "Agent-memory reads (MEMORY.GET/SEARCH, AGENT.RECALL).", snap.MemoryReadsTotal)
		counter("cachepot_memory_writes_total", "Agent-memory writes (MEMORY.PUT, AGENT.REMEMBER).", snap.MemoryWritesTotal)

		counter("cachepot_mcp_calls_total", "Total MCP tool invocations.", snap.MCPCallsTotal)
		fmt.Fprintf(w, "# HELP cachepot_mcp_calls_by_tool_total Total MCP tool invocations, by tool name.\n# TYPE cachepot_mcp_calls_by_tool_total counter\n")
		for tool, count := range snap.MCPCallsByTool {
			fmt.Fprintf(w, "cachepot_mcp_calls_by_tool_total{tool=%q} %d\n", tool, count)
		}

		counter("cachepot_embedding_calls_total", "Total embedding-provider calls issued.", snap.EmbeddingCallsTotal)
		counter("cachepot_embedding_calls_errors_total", "Embedding-provider calls that returned an error.", snap.EmbeddingCallsErrors)
		gauge("cachepot_embedding_calls_in_flight", "Embedding-provider calls currently in flight.", snap.EmbeddingCallsInFlight)

		fmt.Fprintf(w, "# HELP cachepot_command_latency_avg_seconds Average command latency, by command family.\n# TYPE cachepot_command_latency_avg_seconds gauge\n")
		for _, l := range snap.Latency {
			fmt.Fprintf(w, "cachepot_command_latency_avg_seconds{family=%q} %g\n", l.Family, float64(l.AvgNanos)/1e9)
		}
		fmt.Fprintf(w, "# HELP cachepot_command_latency_max_seconds Maximum observed command latency, by command family.\n# TYPE cachepot_command_latency_max_seconds gauge\n")
		for _, l := range snap.Latency {
			fmt.Fprintf(w, "cachepot_command_latency_max_seconds{family=%q} %g\n", l.Family, float64(l.MaxNanos)/1e9)
		}
		fmt.Fprintf(w, "# HELP cachepot_commands_by_family_total Total commands dispatched, by command family.\n# TYPE cachepot_commands_by_family_total counter\n")
		for _, l := range snap.Latency {
			fmt.Fprintf(w, "cachepot_commands_by_family_total{family=%q} %d\n", l.Family, l.Count)
		}
	})
}

// StatsHandler renders m.Snapshot() as JSON -- the same underlying data as
// MetricsHandler, in a form meant for the Phase 5 dashboard (and any other
// JSON consumer) to render directly, including a few precomputed
// convenience fields (hit rates) that Prometheus text format doesn't carry.
func StatsHandler(m *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snap := m.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statsResponse{
			Connections: statsConnections{
				Total:    snap.ConnectionsTotal,
				Active:   snap.ConnectionsActive,
				Rejected: snap.ConnectionsRejected,
			},
			CommandsTotal: snap.CommandsTotal,
			ErrorsTotal:   snap.ErrorsTotal,
			Caches: map[string]statsCache{
				"semantic_cache": statsCacheFrom(snap.SemanticCache),
				"prompt_cache":   statsCacheFrom(snap.PromptCache),
				"tool_cache":     statsCacheFrom(snap.ToolCache),
			},
			VectorSearchesTotal: snap.VectorSearchesTotal,
			MemoryReadsTotal:    snap.MemoryReadsTotal,
			MemoryWritesTotal:   snap.MemoryWritesTotal,
			MCP: statsMCP{
				CallsTotal:  snap.MCPCallsTotal,
				CallsByTool: snap.MCPCallsByTool,
			},
			Embedding: statsEmbedding{
				CallsTotal:    snap.EmbeddingCallsTotal,
				CallsErrors:   snap.EmbeddingCallsErrors,
				CallsInFlight: snap.EmbeddingCallsInFlight,
			},
			Latency: snap.Latency,
		})
	})
}

type statsResponse struct {
	Connections         statsConnections      `json:"connections"`
	CommandsTotal       int64                 `json:"commands_total"`
	ErrorsTotal         int64                 `json:"errors_total"`
	Caches              map[string]statsCache `json:"caches"`
	VectorSearchesTotal int64                 `json:"vector_searches_total"`
	MemoryReadsTotal    int64                 `json:"memory_reads_total"`
	MemoryWritesTotal   int64                 `json:"memory_writes_total"`
	MCP                 statsMCP              `json:"mcp"`
	Embedding           statsEmbedding        `json:"embedding"`
	Latency             []LatencyStats        `json:"latency_by_family"`
}

type statsConnections struct {
	Total    int64 `json:"total"`
	Active   int64 `json:"active"`
	Rejected int64 `json:"rejected"`
}

type statsCache struct {
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
	HitRate float64 `json:"hit_rate"`
}

func statsCacheFrom(c CacheStats) statsCache {
	return statsCache{Hits: c.Hits, Misses: c.Misses, HitRate: c.HitRate()}
}

type statsMCP struct {
	CallsTotal  int64            `json:"calls_total"`
	CallsByTool map[string]int64 `json:"calls_by_tool"`
}

type statsEmbedding struct {
	CallsTotal    int64 `json:"calls_total"`
	CallsErrors   int64 `json:"calls_errors"`
	CallsInFlight int64 `json:"calls_in_flight"`
}
