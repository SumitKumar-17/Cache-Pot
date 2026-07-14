package observability

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
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
		counter("cachepot_evictions_total", "Keys evicted by the maxmemory-style bounded-size trigger.", snap.EvictionsTotal)

		counter("cachepot_consolidations_total", "SUMMARY.CREATE/consolidate invocations.", snap.ConsolidationsTotal)
		counter("cachepot_memories_deduped_total", "Source memories dropped as near-duplicates during consolidation's dedup pass (excluded from summarization input, never deleted from the store).", snap.MemoriesDedupedTotal)

		counter("cachepot_graph_extractions_total", "GRAPH.EXTRACT/extract_entities invocations.", snap.GraphExtractionsTotal)
		counter("cachepot_entities_extracted_total", "Entities added to the knowledge graph across all extractions (0 whenever the CompletionProvider's response wasn't valid JSON, e.g. the mock provider).", snap.EntitiesExtractedTotal)
		counter("cachepot_relations_extracted_total", "Relations added to the knowledge graph across all extractions.", snap.RelationsExtractedTotal)

		counter("cachepot_mcp_calls_total", "Total MCP tool invocations.", snap.MCPCallsTotal)
		fmt.Fprintf(w, "# HELP cachepot_mcp_calls_by_tool_total Total MCP tool invocations, by tool name.\n# TYPE cachepot_mcp_calls_by_tool_total counter\n")
		for tool, count := range snap.MCPCallsByTool {
			fmt.Fprintf(w, "cachepot_mcp_calls_by_tool_total{tool=%q} %d\n", tool, count)
		}

		counter("cachepot_embedding_calls_total", "Total embedding-provider calls issued.", snap.EmbeddingCallsTotal)
		counter("cachepot_embedding_calls_errors_total", "Embedding-provider calls that returned an error.", snap.EmbeddingCallsErrors)
		gauge("cachepot_embedding_calls_in_flight", "Embedding-provider calls currently in flight.", snap.EmbeddingCallsInFlight)

		counter("cachepot_completion_calls_total", "Total completion-provider calls issued.", snap.CompletionCallsTotal)
		counter("cachepot_completion_calls_errors_total", "Completion-provider calls that returned an error.", snap.CompletionCallsErrors)
		gauge("cachepot_completion_calls_in_flight", "Completion-provider calls currently in flight.", snap.CompletionCallsInFlight)

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

// StatsHandler renders m.Snapshot() plus tracker.Snapshot() as a single
// JSON document -- the same underlying Metrics data as MetricsHandler, in
// a form meant for the dashboard (and any other JSON consumer) to
// render directly, including a few precomputed convenience fields (hit
// rates) that Prometheus text format doesn't carry, plus an "analytics"
// section carrying internal/analytics's cost/savings/token data. tracker may
// be nil, in which case the analytics section reports its zero value rather
// than panicking.
func StatsHandler(m *Metrics, tracker *analytics.Tracker) http.Handler {
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
			VectorSearchesTotal:     snap.VectorSearchesTotal,
			MemoryReadsTotal:        snap.MemoryReadsTotal,
			MemoryWritesTotal:       snap.MemoryWritesTotal,
			EvictionsTotal:          snap.EvictionsTotal,
			ConsolidationsTotal:     snap.ConsolidationsTotal,
			MemoriesDedupedTotal:    snap.MemoriesDedupedTotal,
			GraphExtractionsTotal:   snap.GraphExtractionsTotal,
			EntitiesExtractedTotal:  snap.EntitiesExtractedTotal,
			RelationsExtractedTotal: snap.RelationsExtractedTotal,
			MCP: statsMCP{
				CallsTotal:  snap.MCPCallsTotal,
				CallsByTool: snap.MCPCallsByTool,
			},
			Embedding: statsEmbedding{
				CallsTotal:    snap.EmbeddingCallsTotal,
				CallsErrors:   snap.EmbeddingCallsErrors,
				CallsInFlight: snap.EmbeddingCallsInFlight,
			},
			Completion: statsCompletion{
				CallsTotal:    snap.CompletionCallsTotal,
				CallsErrors:   snap.CompletionCallsErrors,
				CallsInFlight: snap.CompletionCallsInFlight,
			},
			Latency:   snap.Latency,
			Analytics: statsAnalyticsFrom(analyticsSnapshot(tracker)),
		})
	})
}

// analyticsSnapshot returns tracker.Snapshot(), or the zero Snapshot if
// tracker is nil, so callers never need a nil check of their own.
func analyticsSnapshot(tracker *analytics.Tracker) analytics.Snapshot {
	if tracker == nil {
		return analytics.Snapshot{}
	}
	return tracker.Snapshot()
}

type statsResponse struct {
	Connections             statsConnections      `json:"connections"`
	CommandsTotal           int64                 `json:"commands_total"`
	ErrorsTotal             int64                 `json:"errors_total"`
	Caches                  map[string]statsCache `json:"caches"`
	VectorSearchesTotal     int64                 `json:"vector_searches_total"`
	MemoryReadsTotal        int64                 `json:"memory_reads_total"`
	MemoryWritesTotal       int64                 `json:"memory_writes_total"`
	EvictionsTotal          int64                 `json:"evictions_total"`
	ConsolidationsTotal     int64                 `json:"consolidations_total"`
	MemoriesDedupedTotal    int64                 `json:"memories_deduped_total"`
	GraphExtractionsTotal   int64                 `json:"graph_extractions_total"`
	EntitiesExtractedTotal  int64                 `json:"entities_extracted_total"`
	RelationsExtractedTotal int64                 `json:"relations_extracted_total"`
	MCP                     statsMCP              `json:"mcp"`
	Embedding               statsEmbedding        `json:"embedding"`
	Completion              statsCompletion       `json:"completion"`
	Latency                 []LatencyStats        `json:"latency_by_family"`
	Analytics               statsAnalytics        `json:"analytics"`
}

// statsAnalytics is the JSON shape of internal/analytics's cost/savings/
// token layer (internal/analytics.Snapshot), folded into the same /stats
// document rather than a separate endpoint.
type statsAnalytics struct {
	EmbeddingByModel map[string]statsModelUsage `json:"embedding_by_model"`
	// CompletionByModel is kept as its own field, separate from
	// EmbeddingByModel, mirroring analytics.Snapshot's own separation of
	// embedding cost from completion cost.
	CompletionByModel   map[string]statsModelUsage `json:"completion_by_model"`
	MoneySavedTotalUSD  float64                    `json:"money_saved_total_usd"`
	TopExpensiveEntries []statsExpensiveEntry      `json:"top_expensive_entries"`
}

type statsModelUsage struct {
	Tokens       int64   `json:"tokens"`
	CostUSD      float64 `json:"cost_usd"`
	PricingKnown bool    `json:"pricing_known"`
}

type statsExpensiveEntry struct {
	CacheType string  `json:"cache_type"`
	Prompt    string  `json:"prompt"`
	CostUSD   float64 `json:"cost_usd"`
	Hits      int64   `json:"hits"`
}

func statsAnalyticsFrom(snap analytics.Snapshot) statsAnalytics {
	byModel := make(map[string]statsModelUsage, len(snap.EmbeddingByModel))
	for model, u := range snap.EmbeddingByModel {
		byModel[model] = statsModelUsage{Tokens: u.Tokens, CostUSD: u.CostUSD, PricingKnown: u.PricingKnown}
	}
	completionByModel := make(map[string]statsModelUsage, len(snap.CompletionByModel))
	for model, u := range snap.CompletionByModel {
		completionByModel[model] = statsModelUsage{Tokens: u.Tokens, CostUSD: u.CostUSD, PricingKnown: u.PricingKnown}
	}
	top := make([]statsExpensiveEntry, len(snap.TopExpensiveEntries))
	for i, e := range snap.TopExpensiveEntries {
		top[i] = statsExpensiveEntry{CacheType: e.CacheType, Prompt: e.Prompt, CostUSD: e.Cost, Hits: e.Hits}
	}
	return statsAnalytics{
		EmbeddingByModel:    byModel,
		CompletionByModel:   completionByModel,
		MoneySavedTotalUSD:  snap.MoneySavedTotalUSD,
		TopExpensiveEntries: top,
	}
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

type statsCompletion struct {
	CallsTotal    int64 `json:"calls_total"`
	CallsErrors   int64 `json:"calls_errors"`
	CallsInFlight int64 `json:"calls_in_flight"`
}

// DashboardHandler renders a plain server-rendered HTML operator/debug
// view (no JS framework, no external CSS/JS dependency, matching this
// project's "no unnecessary dependency" precedent) over the same
// Metrics/Tracker data /stats exposes as JSON: money saved, embedding
// tokens consumed (by model, with cost where the model's pricing is
// known), average/max latency per command family, hit rate per cache
// type, and the most expensive cached prompts.
//
// Deliberately not shown: a "tokens avoided" figure. A CACHE.SEMANTIC hit
// still re-embeds the query prompt to compare similarity, so no embedding
// tokens are actually avoided by a cache hit -- only the (unmeasured,
// caller-side) LLM completion cost is. Showing a fabricated "tokens
// avoided" number would contradict this package's stated honesty
// requirement, so the dashboard sticks to what's actually tracked: tokens
// consumed, and dollars saved (from caller-reported COST).
func DashboardHandler(m *Metrics, tracker *analytics.Tracker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		view := buildDashboardView(m.Snapshot(), analyticsSnapshot(tracker))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := dashboardTemplate.Execute(w, view); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

type dashboardView struct {
	Connections   statsConnections
	CommandsTotal int64
	ErrorsTotal   int64
	Caches        []dashboardCacheRow
	Latency       []LatencyStats
	Models        []dashboardModelRow
	MoneySavedUSD float64
	TopEntries    []analytics.ExpensiveEntry
}

type dashboardCacheRow struct {
	Name string
	statsCache
}

type dashboardModelRow struct {
	Model string
	analytics.ModelUsage
}

func buildDashboardView(snap Snapshot, aSnap analytics.Snapshot) dashboardView {
	caches := []dashboardCacheRow{
		{Name: "semantic_cache", statsCache: statsCacheFrom(snap.SemanticCache)},
		{Name: "prompt_cache", statsCache: statsCacheFrom(snap.PromptCache)},
		{Name: "tool_cache", statsCache: statsCacheFrom(snap.ToolCache)},
	}

	latency := make([]LatencyStats, len(snap.Latency))
	copy(latency, snap.Latency)
	sort.Slice(latency, func(i, j int) bool { return latency[i].Family < latency[j].Family })

	models := make([]dashboardModelRow, 0, len(aSnap.EmbeddingByModel))
	for name, u := range aSnap.EmbeddingByModel {
		models = append(models, dashboardModelRow{Model: name, ModelUsage: u})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Model < models[j].Model })

	return dashboardView{
		Connections:   statsConnections{Total: snap.ConnectionsTotal, Active: snap.ConnectionsActive, Rejected: snap.ConnectionsRejected},
		CommandsTotal: snap.CommandsTotal,
		ErrorsTotal:   snap.ErrorsTotal,
		Caches:        caches,
		Latency:       latency,
		Models:        models,
		MoneySavedUSD: aSnap.MoneySavedTotalUSD,
		TopEntries:    aSnap.TopExpensiveEntries,
	}
}

// avgMillis and maxMillis convert LatencyStats' nanosecond fields to
// milliseconds for display, exposed to the template as methods on
// LatencyStats via these package-level template funcs (html/template
// funcs can't be methods on a type defined elsewhere, so they're plain
// functions instead).
func avgMillis(l LatencyStats) float64 { return float64(l.AvgNanos) / 1e6 }
func maxMillis(l LatencyStats) float64 { return float64(l.MaxNanos) / 1e6 }
func mulf(a, b float64) float64        { return a * b }

var dashboardTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"avgMillis": avgMillis,
	"maxMillis": maxMillis,
	"mulf":      mulf,
}).Parse(dashboardHTML))

const dashboardHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Cache-Pot Dashboard</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 2rem; color: #1a1a1a; background: #fafafa; }
  h1 { font-size: 1.4rem; }
  h2 { font-size: 1.1rem; margin-top: 2rem; border-bottom: 1px solid #ddd; padding-bottom: .25rem; }
  table { border-collapse: collapse; width: 100%; margin-top: .5rem; background: #fff; }
  th, td { text-align: left; padding: .4rem .6rem; border-bottom: 1px solid #eee; font-size: .9rem; }
  th { background: #f0f0f0; }
  .stat { display: inline-block; margin-right: 2rem; margin-top: .5rem; }
  .stat .value { font-size: 1.6rem; font-weight: 600; display: block; }
  .stat .label { font-size: .8rem; color: #666; }
  .muted { color: #888; font-size: .85rem; }
</style>
</head>
<body>
<h1>Cache-Pot Dashboard</h1>
<p class="muted">Operator/debug view over live process state -- not a marketing surface. Refresh the page for updated figures.</p>

<div>
  <div class="stat"><span class="value">${{printf "%.4f" .MoneySavedUSD}}</span><span class="label">money saved (from caller-reported COST)</span></div>
  <div class="stat"><span class="value">{{.CommandsTotal}}</span><span class="label">commands total</span></div>
  <div class="stat"><span class="value">{{.ErrorsTotal}}</span><span class="label">errors total</span></div>
  <div class="stat"><span class="value">{{.Connections.Active}}</span><span class="label">connections active</span></div>
</div>

<h2>Cache hit rate</h2>
<table>
<tr><th>cache</th><th>hits</th><th>misses</th><th>hit rate</th></tr>
{{range .Caches}}<tr><td>{{.Name}}</td><td>{{.Hits}}</td><td>{{.Misses}}</td><td>{{printf "%.1f%%" (mulf .HitRate 100)}}</td></tr>
{{end}}
</table>

<h2>Latency by command family</h2>
<table>
<tr><th>family</th><th>count</th><th>avg (ms)</th><th>max (ms)</th></tr>
{{range .Latency}}<tr><td>{{.Family}}</td><td>{{.Count}}</td><td>{{printf "%.3f" (avgMillis .)}}</td><td>{{printf "%.3f" (maxMillis .)}}</td></tr>
{{end}}
</table>

<h2>Embedding tokens consumed, by model</h2>
<table>
<tr><th>model</th><th>tokens</th><th>estimated cost (USD)</th></tr>
{{range .Models}}<tr><td>{{.Model}}</td><td>{{.Tokens}}</td><td>{{if .PricingKnown}}{{printf "$%.6f" .CostUSD}}{{else}}<span class="muted">unknown model, cost not estimated</span>{{end}}</td></tr>
{{else}}<tr><td colspan="3" class="muted">no embedding usage recorded yet</td></tr>
{{end}}
</table>

<h2>Most expensive cached prompts (hit at least once)</h2>
<table>
<tr><th>cache</th><th>prompt / template</th><th>cost (USD)</th><th>hits</th></tr>
{{range .TopEntries}}<tr><td>{{.CacheType}}</td><td>{{.Prompt}}</td><td>{{printf "$%.4f" .Cost}}</td><td>{{.Hits}}</td></tr>
{{else}}<tr><td colspan="4" class="muted">no COST-tagged cache hits recorded yet</td></tr>
{{end}}
</table>

</body>
</html>
`
