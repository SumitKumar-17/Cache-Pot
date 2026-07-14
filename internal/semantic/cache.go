// Package semantic implements Cache-Pot's similarity-based and exact-match
// caches for LLM responses: SemanticCache (embedding similarity search,
// scoped by model/temperature) and PromptCache (exact-match, keyed by a
// hash of template + variables + model).
package semantic

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/embed"
)

// semanticEntry is one cached (prompt, response) pair alongside the
// embedding used to match future prompts against it.
type semanticEntry struct {
	prompt    string
	embedding []float32
	response  string
	expiresAt *time.Time
	// cost is the optional, caller-reported dollar cost of originally
	// producing response (e.g. the LLM completion cost the caller paid).
	// It defaults to 0 when the caller never supplies one via CACHE.
	// SEMANTIC SET's optional COST argument -- internal/semantic knows
	// nothing about internal/analytics; it just carries this value back
	// out of Get so the RESP/MCP layer (which holds the shared
	// *analytics.Tracker) can record any money-saved savings itself.
	cost float64
}

func (e *semanticEntry) expired(now time.Time) bool {
	return e.expiresAt != nil && !e.expiresAt.After(now)
}

// SemanticCache is a similarity-based cache for LLM responses: instead of
// requiring an exact prompt match, Get returns the stored response for the
// closest previously-seen prompt in the same (model, temperature)
// partition, provided its cosine similarity to the query prompt is at or
// above a threshold.
//
// Partitioning: entries are grouped by "model\x00temperature" so an
// identical prompt against a different model or temperature never
// cross-matches — different models, and the same model at different
// temperatures, can validly deserve different cached answers.
//
// Lookup strategy: Get does a brute-force linear scan of every
// non-expired entry in the matching partition, computing cosine similarity
// against each one. This is intentionally simple: the project's stated
// vector design is "flat index first, ANN later" (see internal/vector), and
// a single partition is expected to stay small enough for an O(n) scan to
// be fine. Swap in an ANN index per partition (e.g. backed by
// internal/vector.Index) once that stops being true.
//
// Expiry: entries carry an optional absolute expiry time. Get lazily
// evicts any expired entry it encounters during its scan rather than
// running a separate reaper goroutine — this could be unified with
// internal/eviction's policies rather than reinventing expiry a third time.
//
// SemanticCache is safe for concurrent use.
type SemanticCache struct {
	provider embed.Provider

	mu         sync.Mutex
	partitions map[string][]semanticEntry

	// now is overridable in tests so TTL-expiry tests don't need real
	// sleeps for the general case; production code always uses time.Now.
	now func() time.Time
}

// New builds a SemanticCache that uses provider to embed prompts.
func New(provider embed.Provider) *SemanticCache {
	return &SemanticCache{
		provider:   provider,
		partitions: make(map[string][]semanticEntry),
		now:        time.Now,
	}
}

func partitionKey(model, temp string) string {
	return model + "\x00" + temp
}

// Set embeds prompt and stores it alongside response in the (model, temp)
// partition. ttl <= 0 means the entry never expires. cost is the optional,
// caller-reported dollar cost of producing response; <= 0 means "unknown/
// not reported" and Get will never report savings for this entry.
func (c *SemanticCache) Set(ctx context.Context, prompt, model, temp, response string, ttl time.Duration, cost float64) error {
	vec, err := c.provider.Embed(ctx, prompt)
	if err != nil {
		return err
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := c.now().Add(ttl)
		expiresAt = &t
	}

	e := semanticEntry{prompt: prompt, embedding: vec, response: response, expiresAt: expiresAt, cost: cost}

	key := partitionKey(model, temp)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partitions[key] = append(c.partitions[key], e)
	return nil
}

// Get embeds prompt and searches the (model, temp) partition for the
// closest previously-stored prompt, reporting a hit if that closest
// entry's cosine similarity to prompt is >= threshold. Expired entries
// encountered during the scan are evicted lazily and never considered.
// cost is the hit entry's stored cost (0 if none was ever supplied, or on
// a miss) -- callers holding a *analytics.Tracker should record savings
// from it themselves; internal/semantic deliberately doesn't import
// internal/analytics.
func (c *SemanticCache) Get(ctx context.Context, prompt, model, temp string, threshold float64) (response string, found bool, cost float64, err error) {
	vec, err := c.provider.Embed(ctx, prompt)
	if err != nil {
		return "", false, 0, err
	}

	key := partitionKey(model, temp)
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	entries := c.partitions[key]
	// Compact in place: kept shares entries' backing array, and the
	// write index never runs ahead of the read index, so this is safe.
	kept := entries[:0]
	bestIdx := -1
	bestScore := math.Inf(-1)
	for _, e := range entries {
		if e.expired(now) {
			continue
		}
		kept = append(kept, e)
		score := embed.Cosine(vec, e.embedding)
		if math.IsNaN(score) {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestIdx = len(kept) - 1
		}
	}
	c.partitions[key] = kept

	if bestIdx < 0 || bestScore < threshold {
		return "", false, 0, nil
	}
	return kept[bestIdx].response, true, kept[bestIdx].cost, nil
}
