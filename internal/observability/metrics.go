// Package observability provides minimal, dependency-free instrumentation:
// atomic counters plus a thin slog wrapper. It is deliberately structured so
// a Prometheus (or other) exporter can wrap Metrics.Snapshot() later without
// pulling a metrics client library into Phase 1. Phase 5 is that "later":
// per-operation-type hit/miss/latency tracking, plus hand-rolled /metrics
// (Prometheus text) and /stats (JSON) HTTP handlers in http.go, still with
// no metrics client dependency, matching this project's existing precedent
// (e.g. the stdlib-only OpenAI embeddings provider).
package observability

import (
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds process-wide atomic counters. All fields are safe for
// concurrent use from any goroutine (RESP connection handlers, MCP tool
// handlers, the TTL reaper, etc.).
type Metrics struct {
	connectionsTotal    atomic.Int64
	connectionsActive   atomic.Int64
	connectionsRejected atomic.Int64
	commandsTotal       atomic.Int64
	errorsTotal         atomic.Int64

	semanticCacheHits   atomic.Int64
	semanticCacheMisses atomic.Int64
	promptCacheHits     atomic.Int64
	promptCacheMisses   atomic.Int64
	toolCacheHits       atomic.Int64
	toolCacheMisses     atomic.Int64
	vectorSearches      atomic.Int64
	memoryReads         atomic.Int64
	memoryWrites        atomic.Int64
	evictionsTotal      atomic.Int64

	mcpCallsTotal  atomic.Int64
	mcpMu          sync.Mutex
	mcpCallsByTool map[string]int64

	embeddingCallsTotal    atomic.Int64
	embeddingCallsErrors   atomic.Int64
	embeddingCallsInFlight atomic.Int64

	completionCallsTotal    atomic.Int64
	completionCallsErrors   atomic.Int64
	completionCallsInFlight atomic.Int64

	latMu   sync.Mutex
	latency map[string]*latencyAccumulator
}

// latencyAccumulator is a dependency-free per-family latency accumulator:
// count, sum, and max, all atomic. This is deliberately not a histogram --
// bucketed histograms are more machinery than this project's stated scope
// needs; count/sum (for an average) and max are enough to answer "how many,
// how slow on average, how slow at worst" per command family.
type latencyAccumulator struct {
	count    atomic.Int64
	sumNanos atomic.Int64
	maxNanos atomic.Int64
}

func (l *latencyAccumulator) record(d time.Duration) {
	l.count.Add(1)
	l.sumNanos.Add(int64(d))
	for {
		cur := l.maxNanos.Load()
		n := int64(d)
		if n <= cur {
			return
		}
		if l.maxNanos.CompareAndSwap(cur, n) {
			return
		}
	}
}

// NewMetrics constructs an empty Metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		mcpCallsByTool: make(map[string]int64),
		latency:        make(map[string]*latencyAccumulator),
	}
}

// ConnectionOpened records a new accepted connection.
func (m *Metrics) ConnectionOpened() {
	m.connectionsTotal.Add(1)
	m.connectionsActive.Add(1)
}

// ConnectionClosed records a connection going away.
func (m *Metrics) ConnectionClosed() {
	m.connectionsActive.Add(-1)
}

// ConnectionRejected records a connection refused (e.g. MaxConnections hit).
func (m *Metrics) ConnectionRejected() {
	m.connectionsRejected.Add(1)
}

// CommandExecuted records one dispatched command.
func (m *Metrics) CommandExecuted() {
	m.commandsTotal.Add(1)
}

// ErrorReturned records one error reply sent to a client.
func (m *Metrics) ErrorReturned() {
	m.errorsTotal.Add(1)
}

// SemanticCacheHit/Miss record a CACHE.SEMANTIC GET outcome.
func (m *Metrics) SemanticCacheHit()  { m.semanticCacheHits.Add(1) }
func (m *Metrics) SemanticCacheMiss() { m.semanticCacheMisses.Add(1) }

// PromptCacheHit/Miss record a CACHE.PROMPT GET outcome.
func (m *Metrics) PromptCacheHit()  { m.promptCacheHits.Add(1) }
func (m *Metrics) PromptCacheMiss() { m.promptCacheMisses.Add(1) }

// ToolCacheHit/Miss record a TOOL.CACHE GET outcome.
func (m *Metrics) ToolCacheHit()  { m.toolCacheHits.Add(1) }
func (m *Metrics) ToolCacheMiss() { m.toolCacheMisses.Add(1) }

// VectorSearchPerformed records one VECTOR.SEARCH call. Vector search has no
// clean hit/miss concept the way an exact-match cache does -- it always
// returns its best-effort ranked results -- so this just counts
// invocations, not hits/misses.
func (m *Metrics) VectorSearchPerformed() { m.vectorSearches.Add(1) }

// MemoryRead records one agent-memory read (MEMORY.GET, MEMORY.SEARCH, or
// AGENT.RECALL).
func (m *Metrics) MemoryRead() { m.memoryReads.Add(1) }

// MemoryWrite records one agent-memory write (MEMORY.PUT or AGENT.REMEMBER).
func (m *Metrics) MemoryWrite() { m.memoryWrites.Add(1) }

// KeyEvicted records one key evicted by internal/storage/memstore's
// maxmemory-style bounded-size trigger. It's a plain no-argument callback
// (see memstore.WithOnEvict) so memstore never needs to import this
// package -- observability depends on storage-adjacent behavior, not the
// other way around.
func (m *Metrics) KeyEvicted() { m.evictionsTotal.Add(1) }

// MCPCallRecorded records one MCP tool invocation, both overall and by tool
// name.
func (m *Metrics) MCPCallRecorded(tool string) {
	m.mcpCallsTotal.Add(1)
	m.mcpMu.Lock()
	m.mcpCallsByTool[tool]++
	m.mcpMu.Unlock()
}

// EmbeddingCallStarted records the start of an embedding-provider call:
// increments the total-issued counter and the in-flight gauge. Pair with a
// deferred EmbeddingCallFinished. This is the "embedding queue depth"
// signal from the project's original vision.
func (m *Metrics) EmbeddingCallStarted() {
	m.embeddingCallsTotal.Add(1)
	m.embeddingCallsInFlight.Add(1)
}

// EmbeddingCallFinished records the end of an embedding-provider call:
// decrements the in-flight gauge, and increments the error counter if err
// is non-nil.
func (m *Metrics) EmbeddingCallFinished(err error) {
	m.embeddingCallsInFlight.Add(-1)
	if err != nil {
		m.embeddingCallsErrors.Add(1)
	}
}

// CompletionCallStarted records the start of a completion-provider call:
// increments the total-issued counter and the in-flight gauge. Pair with a
// deferred CompletionCallFinished. Mirrors EmbeddingCallStarted for
// Cache-Pot's first text-generation capability (internal/llm).
func (m *Metrics) CompletionCallStarted() {
	m.completionCallsTotal.Add(1)
	m.completionCallsInFlight.Add(1)
}

// CompletionCallFinished records the end of a completion-provider call:
// decrements the in-flight gauge, and increments the error counter if err
// is non-nil.
func (m *Metrics) CompletionCallFinished(err error) {
	m.completionCallsInFlight.Add(-1)
	if err != nil {
		m.completionCallsErrors.Add(1)
	}
}

// RecordCommandLatency records one command's execution latency under the
// given family (see internal/server/resp's commandFamily), lazily creating
// that family's accumulator on first use. The family set is small and
// bounded (roughly a dozen), so this map only ever grows to that size.
func (m *Metrics) RecordCommandLatency(family string, d time.Duration) {
	m.latMu.Lock()
	acc, ok := m.latency[family]
	if !ok {
		acc = &latencyAccumulator{}
		m.latency[family] = acc
	}
	m.latMu.Unlock()
	acc.record(d)
}

// LatencyStats is a point-in-time summary of one family's recorded
// latencies.
type LatencyStats struct {
	Family   string
	Count    int64
	AvgNanos int64
	MaxNanos int64
}

// CacheStats is a point-in-time hit/miss summary for one exact-match cache.
type CacheStats struct {
	Hits   int64
	Misses int64
}

// HitRate returns Hits / (Hits + Misses), or 0 if there have been no
// lookups yet at all (avoids a NaN from a 0/0 division).
func (c CacheStats) HitRate() float64 {
	total := c.Hits + c.Misses
	if total == 0 {
		return 0
	}
	return float64(c.Hits) / float64(total)
}

// Snapshot is a point-in-time copy of counter values, safe to read without
// further synchronization. A Prometheus exporter (or any other sink) can be
// built by periodically calling Metrics.Snapshot() and translating the
// fields into its own metric types.
type Snapshot struct {
	ConnectionsTotal    int64
	ConnectionsActive   int64
	ConnectionsRejected int64
	CommandsTotal       int64
	ErrorsTotal         int64

	SemanticCache CacheStats
	PromptCache   CacheStats
	ToolCache     CacheStats

	VectorSearchesTotal int64
	MemoryReadsTotal    int64
	MemoryWritesTotal   int64
	EvictionsTotal      int64

	MCPCallsTotal  int64
	MCPCallsByTool map[string]int64

	EmbeddingCallsTotal    int64
	EmbeddingCallsErrors   int64
	EmbeddingCallsInFlight int64

	CompletionCallsTotal    int64
	CompletionCallsErrors   int64
	CompletionCallsInFlight int64

	Latency []LatencyStats
}

// Snapshot returns the current counter values.
func (m *Metrics) Snapshot() Snapshot {
	m.mcpMu.Lock()
	byTool := make(map[string]int64, len(m.mcpCallsByTool))
	maps.Copy(byTool, m.mcpCallsByTool)
	m.mcpMu.Unlock()

	m.latMu.Lock()
	lat := make([]LatencyStats, 0, len(m.latency))
	for family, acc := range m.latency {
		count := acc.count.Load()
		var avg int64
		if count > 0 {
			avg = acc.sumNanos.Load() / count
		}
		lat = append(lat, LatencyStats{
			Family:   family,
			Count:    count,
			AvgNanos: avg,
			MaxNanos: acc.maxNanos.Load(),
		})
	}
	m.latMu.Unlock()

	return Snapshot{
		ConnectionsTotal:    m.connectionsTotal.Load(),
		ConnectionsActive:   m.connectionsActive.Load(),
		ConnectionsRejected: m.connectionsRejected.Load(),
		CommandsTotal:       m.commandsTotal.Load(),
		ErrorsTotal:         m.errorsTotal.Load(),

		SemanticCache: CacheStats{Hits: m.semanticCacheHits.Load(), Misses: m.semanticCacheMisses.Load()},
		PromptCache:   CacheStats{Hits: m.promptCacheHits.Load(), Misses: m.promptCacheMisses.Load()},
		ToolCache:     CacheStats{Hits: m.toolCacheHits.Load(), Misses: m.toolCacheMisses.Load()},

		VectorSearchesTotal: m.vectorSearches.Load(),
		MemoryReadsTotal:    m.memoryReads.Load(),
		MemoryWritesTotal:   m.memoryWrites.Load(),
		EvictionsTotal:      m.evictionsTotal.Load(),

		MCPCallsTotal:  m.mcpCallsTotal.Load(),
		MCPCallsByTool: byTool,

		EmbeddingCallsTotal:    m.embeddingCallsTotal.Load(),
		EmbeddingCallsErrors:   m.embeddingCallsErrors.Load(),
		EmbeddingCallsInFlight: m.embeddingCallsInFlight.Load(),

		CompletionCallsTotal:    m.completionCallsTotal.Load(),
		CompletionCallsErrors:   m.completionCallsErrors.Load(),
		CompletionCallsInFlight: m.completionCallsInFlight.Load(),

		Latency: lat,
	}
}
