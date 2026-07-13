package analytics

import (
	"sort"
	"strings"
	"sync"
)

// maxTopEntries bounds how many of the highest-cost, at-least-once-hit
// cache entries Tracker keeps around for the dashboard's "most expensive
// cached prompts" view. This keeps Tracker's memory footprint bounded even
// under sustained traffic with many unique prompts, matching its stated
// role as a lightweight dashboard data source rather than a full
// warehouse.
const maxTopEntries = 20

// pricePerMillionTokensUSD is a small, hand-maintained table of published
// OpenAI embedding-model pricing, in USD per 1,000,000 tokens.
//
// These are the prices published on OpenAI's pricing page as of when this
// code was written (2026) and WILL drift over time as OpenAI changes
// pricing -- operators should verify current pricing against OpenAI's own
// pricing page before treating any cost figure derived from this table as
// authoritative. It exists to give a reasonable estimate, not a bill.
var pricePerMillionTokensUSD = map[string]float64{
	"text-embedding-3-small": 0.02,
	"text-embedding-3-large": 0.13,
	"text-embedding-ada-002": 0.10,
}

// ModelUsage is a point-in-time summary of one embedding model's recorded
// token usage and estimated cost.
type ModelUsage struct {
	Tokens int64
	// CostUSD is the estimated cost attributed to Tokens, using
	// pricePerMillionTokensUSD. Only meaningful when PricingKnown is true.
	CostUSD float64
	// PricingKnown reports whether the model was recognized in
	// pricePerMillionTokensUSD. When false, Tokens is still a real,
	// accumulated count, but CostUSD is deliberately left at 0 rather than
	// guessing a price for a model this table doesn't know about.
	PricingKnown bool
}

// ExpensiveEntry is one cache entry (identified by its cache type and
// prompt/template text) that has been hit at least once with a
// caller-reported Cost > 0.
type ExpensiveEntry struct {
	// CacheType is "semantic" or "prompt", identifying which cache the
	// entry came from.
	CacheType string
	// Prompt is the human-readable text identifying the entry: the
	// original prompt for the semantic cache, or the template text for
	// the prompt cache.
	Prompt string
	// Cost is the highest caller-reported COST seen for this entry.
	Cost float64
	// Hits is how many times this entry has been served as a cache hit
	// while carrying a positive Cost.
	Hits int64
}

// Tracker is a dependency-free, in-memory, safe-for-concurrent-use cost and
// savings tracker. It is intentionally not a time-series store: it holds
// only running totals plus a small bounded top-N list, enough to back a
// point-in-time dashboard snapshot.
type Tracker struct {
	mu sync.Mutex

	byModel map[string]*ModelUsage

	moneySavedTotalUSD float64
	// top holds at most maxTopEntries entries, unique by (CacheType,
	// Prompt). It is not kept sorted between calls; Snapshot sorts a copy
	// on read.
	top []ExpensiveEntry
}

// New builds an empty Tracker.
func New() *Tracker {
	return &Tracker{byModel: make(map[string]*ModelUsage)}
}

// normalizeModelName accepts either a raw model name (e.g.
// "text-embedding-3-small") or a Provider.Name()-shaped string (e.g.
// "openai:text-embedding-3-small") and returns just the model portion, so
// RecordEmbeddingUsage callers don't need to know which form they have.
func normalizeModelName(name string) string {
	if _, after, found := strings.Cut(name, ":"); found {
		return after
	}
	return name
}

// RecordEmbeddingUsage records that an embedding call against model
// consumed tokens tokens, accumulating per-model totals and, for
// recognized models, an estimated USD cost. tokens <= 0 is a no-op (there
// is nothing real to record). model may be a raw model name or a
// Provider.Name()-shaped "provider:model" string.
func (t *Tracker) RecordEmbeddingUsage(model string, tokens int) {
	if tokens <= 0 {
		return
	}
	name := normalizeModelName(model)

	t.mu.Lock()
	defer t.mu.Unlock()

	u, ok := t.byModel[name]
	if !ok {
		u = &ModelUsage{}
		t.byModel[name] = u
	}
	u.Tokens += int64(tokens)
	if price, known := pricePerMillionTokensUSD[name]; known {
		u.PricingKnown = true
		u.CostUSD += float64(tokens) * price / 1_000_000
	}
}

// RecordCacheHitSavings records that a cache hit on a (cacheType, prompt)
// entry avoided re-paying cost dollars (the cost the caller originally
// reported it took to produce the cached response, via CACHE.SEMANTIC/
// CACHE.PROMPT SET's optional COST argument). cost <= 0 is a no-op: money
// saved is only ever recorded from an explicit, caller-reported cost,
// never a fabricated estimate.
//
// Scoped deliberately to CACHE.SEMANTIC/CACHE.PROMPT: "money saved" most
// naturally means "avoided an LLM completion call," which is what these
// two caches exist for. TOOL.CACHE's tool-call costs are a different (and
// more varied/unknowable) cost model, out of scope for this tracker.
func (t *Tracker) RecordCacheHitSavings(cacheType, prompt string, cost float64) {
	if cost <= 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.moneySavedTotalUSD += cost

	for i := range t.top {
		if t.top[i].CacheType == cacheType && t.top[i].Prompt == prompt {
			t.top[i].Hits++
			if cost > t.top[i].Cost {
				t.top[i].Cost = cost
			}
			return
		}
	}

	entry := ExpensiveEntry{CacheType: cacheType, Prompt: prompt, Cost: cost, Hits: 1}
	if len(t.top) < maxTopEntries {
		t.top = append(t.top, entry)
		return
	}
	// The bounded list is full: only displace the current cheapest entry,
	// and only if this one is more expensive.
	minIdx := 0
	for i := 1; i < len(t.top); i++ {
		if t.top[i].Cost < t.top[minIdx].Cost {
			minIdx = i
		}
	}
	if entry.Cost > t.top[minIdx].Cost {
		t.top[minIdx] = entry
	}
}

// Snapshot is a point-in-time, serializable copy of Tracker's state,
// following the same Snapshot()-returns-a-value-type convention
// observability.Metrics already uses.
type Snapshot struct {
	EmbeddingByModel    map[string]ModelUsage
	MoneySavedTotalUSD  float64
	TopExpensiveEntries []ExpensiveEntry
}

// Snapshot returns the current tracked values. TopExpensiveEntries is
// sorted by Cost descending.
func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	byModel := make(map[string]ModelUsage, len(t.byModel))
	for name, u := range t.byModel {
		byModel[name] = *u
	}

	top := make([]ExpensiveEntry, len(t.top))
	copy(top, t.top)
	sort.Slice(top, func(i, j int) bool { return top[i].Cost > top[j].Cost })

	return Snapshot{
		EmbeddingByModel:    byModel,
		MoneySavedTotalUSD:  t.moneySavedTotalUSD,
		TopExpensiveEntries: top,
	}
}
