package vector

import (
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/SumitKumar-17/cache-pot/internal/embed"
)

// vecEntry is one stored vector within a namespace: the embedding itself,
// arbitrary string-valued metadata usable for exact-match FILTERing in
// VECTOR.SEARCH, and an optional raw text payload used only for HYBRID
// keyword+vector search.
type vecEntry struct {
	vector   []float32
	metadata map[string]string
	text     string
}

// Result is one nearest-neighbor search result.
type Result struct {
	ID    string
	Score float64
}

// HybridOpts configures Store.Search's naive hybrid keyword+vector mode.
// When non-nil, Search blends each candidate's vector score with a naive
// keyword-overlap score computed between QueryText and the candidate's
// stored text (see VECTOR.UPSERT's TEXT option):
//
//	final = Alpha*normalizedVectorScore + (1-Alpha)*keywordScore
//
// keywordScore is "fraction of QueryText's (unique, lowercased,
// whitespace-tokenized) tokens also present in the candidate's text
// tokens" -- no stemming, no IDF weighting, no phrase matching. This is
// intentionally naive: the project's own roadmap scopes hybrid search here
// as "naive hybrid search, not a solved feature."
//
// normalizedVectorScore is the metric's own score for cosine/dot (used
// as-is, already roughly bounded) or 1/(1+distance) for euclidean, which
// maps a distance in [0, +Inf) to a similarity-like value in (0, 1] so it
// can be blended the same way (a naive, not principled, transform -- it
// just needs "smaller distance -> larger blended score").
type HybridOpts struct {
	QueryText string
	Alpha     float64
}

// Store is a flat (brute-force) vector index, partitioned by namespace so
// unrelated collections of vectors (e.g. different applications/tenants)
// never cross-match. Within a namespace, Search does a linear scan of
// every stored vector, computing the requested distance metric against the
// query -- the same "flat index first, ANN later" approach
// internal/semantic.SemanticCache uses for its brute-force prompt-
// similarity scan (see internal/semantic/cache.go's doc comment), just
// partitioned by namespace instead of (model, temperature). Swap in a real
// ANN backend (e.g. behind the Index interface in index.go) per namespace
// once brute-force stops being fast enough.
//
// Store is safe for concurrent use: a single RWMutex guards the namespace
// map and every namespace's contents. Brute-force scanning is the
// bottleneck either way at this phase, so per-namespace locks would add
// complexity without a real concurrency win yet.
type Store struct {
	mu         sync.RWMutex
	namespaces map[string]map[string]vecEntry
}

// New builds an empty Store.
func New() *Store {
	return &Store{namespaces: make(map[string]map[string]vecEntry)}
}

// Upsert inserts or replaces the vector stored under (namespace, id).
// Upserting an id that already exists in namespace entirely replaces its
// previous vector/metadata/text (not a merge). metadata and text may be
// nil/empty if not needed.
func (s *Store) Upsert(namespace, id string, vec []float32, metadata map[string]string, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ns, ok := s.namespaces[namespace]
	if !ok {
		ns = make(map[string]vecEntry)
		s.namespaces[namespace] = ns
	}
	// Copy vec so later caller-side mutation of the slice they passed in
	// can't corrupt stored state.
	stored := make([]float32, len(vec))
	copy(stored, vec)
	ns[id] = vecEntry{vector: stored, metadata: metadata, text: text}
}

// Delete removes the vector stored under (namespace, id), reporting
// whether it existed.
func (s *Store) Delete(namespace, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	ns, ok := s.namespaces[namespace]
	if !ok {
		return false
	}
	if _, ok := ns[id]; !ok {
		return false
	}
	delete(ns, id)
	return true
}

// Search finds up to k nearest neighbors to vec within namespace according
// to metric, optionally restricted to entries whose metadata matches every
// key/value pair in filter (exact string equality; see
// internal/server/resp/handlers_vector.go for how metadata values are
// canonicalized to strings), and optionally blended with a naive hybrid
// keyword score (see HybridOpts).
//
// Results are sorted best-match-first: for pure-vector search (hybrid ==
// nil) that means highest score first for Cosine/Dot and lowest distance
// first for Euclidean; for hybrid search it always means highest blended
// score first, since HybridOpts already normalizes Euclidean into a
// similarity-like value before blending. Ties are broken by ID
// (ascending) for deterministic output. Results are capped at k; k <= 0
// returns nil.
//
// A stored entry whose vector has a different dimension than vec is
// skipped rather than aborting the whole search with an error -- a
// namespace could in principle hold vectors of mixed dimension if a caller
// misuses it, and one mismatched entry shouldn't fail every other
// candidate. Likewise, an entry whose raw metric score comes back NaN
// (e.g. Cosine against a zero-magnitude vector) is skipped.
//
// An unknown namespace, or a namespace with no matching entries, returns
// nil, not an error.
func (s *Store) Search(namespace string, vec []float32, k int, metric DistanceMetric, filter map[string]string, hybrid *HybridOpts) []Result {
	if k <= 0 {
		return nil
	}

	type candidate struct {
		id string
		e  vecEntry
	}

	s.mu.RLock()
	ns := s.namespaces[namespace]
	candidates := make([]candidate, 0, len(ns))
	for id, e := range ns {
		candidates = append(candidates, candidate{id: id, e: e})
	}
	s.mu.RUnlock()

	results := make([]Result, 0, len(candidates))
	for _, c := range candidates {
		if !matchesFilter(c.e.metadata, filter) {
			continue
		}
		if len(c.e.vector) != len(vec) {
			continue
		}

		score, ok := rawScore(vec, c.e.vector, metric)
		if !ok {
			continue
		}

		if hybrid != nil {
			vecScore := normalizeForBlend(score, metric)
			kwScore := keywordScore(hybrid.QueryText, c.e.text)
			score = hybrid.Alpha*vecScore + (1-hybrid.Alpha)*kwScore
		}

		results = append(results, Result{ID: c.id, Score: score})
	}

	higherIsBetter := hybrid != nil || metric != Euclidean
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			if higherIsBetter {
				return results[i].Score > results[j].Score
			}
			return results[i].Score < results[j].Score
		}
		return results[i].ID < results[j].ID
	})

	if len(results) > k {
		results = results[:k]
	}
	return results
}

// rawScore computes metric's raw score between the query vector a and a
// candidate vector b, reporting ok=false if the result is undefined (NaN).
func rawScore(a, b []float32, metric DistanceMetric) (score float64, ok bool) {
	var v float64
	switch metric {
	case Dot:
		v = embed.Dot(a, b)
	case Euclidean:
		v = embed.Euclidean(a, b)
	default: // Cosine
		v = embed.Cosine(a, b)
	}
	if math.IsNaN(v) {
		return 0, false
	}
	return v, true
}

// normalizeForBlend maps a raw metric score into a similarity-like value
// suitable for blending with the naive keyword score: Cosine/Dot scores
// are used as-is (already roughly bounded / already "higher is better"),
// while Euclidean distance is transformed via 1/(1+distance) so a smaller
// distance yields a larger, comparable score. See HybridOpts's doc comment.
func normalizeForBlend(score float64, metric DistanceMetric) float64 {
	if metric == Euclidean {
		return 1 / (1 + score)
	}
	return score
}

// matchesFilter reports whether metadata contains every key/value pair in
// filter (exact string equality). A nil/empty filter always matches.
func matchesFilter(metadata map[string]string, filter map[string]string) bool {
	for k, v := range filter {
		if metadata == nil {
			return false
		}
		mv, ok := metadata[k]
		if !ok || mv != v {
			return false
		}
	}
	return true
}

// tokenize lowercases and whitespace-splits s into a set of unique tokens.
func tokenize(s string) map[string]struct{} {
	fields := strings.Fields(strings.ToLower(s))
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		set[f] = struct{}{}
	}
	return set
}

// keywordScore is the naive keyword-overlap score used by hybrid search:
// the fraction of query's unique tokens that also appear in text's tokens
// (case-insensitive, whitespace-tokenized). Returns 0 if either side has
// no tokens.
func keywordScore(query, text string) float64 {
	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return 0
	}
	tTokens := tokenize(text)
	if len(tTokens) == 0 {
		return 0
	}
	matched := 0
	for t := range qTokens {
		if _, ok := tTokens[t]; ok {
			matched++
		}
	}
	return float64(matched) / float64(len(qTokens))
}
