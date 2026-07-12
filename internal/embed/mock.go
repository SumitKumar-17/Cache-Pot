package embed

import (
	"context"
	"hash/fnv"
	"math"
	"math/rand"
	"strings"
)

// mockProvider is a deterministic, dependency-free Provider implementation
// for tests and offline/local development that don't have (or want) an API
// key. It produces realistic-enough vectors for exercising similarity
// thresholds: identical text always yields identical vectors, unrelated
// text yields very different vectors, and near-duplicate text (same words,
// different case/whitespace) yields vectors that are close but usually not
// bit-identical.
type mockProvider struct {
	dims int
}

// NewMock returns a deterministic mock Provider that produces vectors of
// length dims. If dims <= 0, a default of 8 is used.
func NewMock(dims int) Provider {
	if dims <= 0 {
		dims = 8
	}
	return &mockProvider{dims: dims}
}

func (m *mockProvider) Name() string    { return "mock" }
func (m *mockProvider) Dimensions() int { return m.dims }

func (m *mockProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return deterministicEmbedding(text, m.dims), nil
}

func (m *mockProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := m.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// deterministicEmbedding builds a deterministic pseudo-embedding for text.
//
// The result is the sum of two components:
//
//  1. A "bag of words" component derived from lowercased,
//     whitespace-normalized tokens (via strings.Fields). This dominates the
//     vector's direction: two texts with the same words — regardless of
//     case or exact whitespace — produce an identical component here.
//
//  2. A small "exact form" perturbation derived from a hash of the raw,
//     unnormalized text. Byte-identical inputs get an identical
//     perturbation (so exact duplicates map to exactly the same vector);
//     near-duplicates (same words, different case/whitespace) get a small,
//     different perturbation, so their vectors end up close but not
//     identical — useful for exercising a semantic cache's similarity
//     threshold logic.
//
// This is NOT a real embedding model: similarity is driven by token
// overlap, not meaning. It exists purely to give deterministic, offline,
// dependency-free test fixtures.
func deterministicEmbedding(text string, dims int) []float32 {
	vec := make([]float64, dims)

	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		// Whitespace-only or empty input still gets a stable, distinct
		// vector rather than an all-zero one.
		words = []string{"\x00empty\x00"}
	}
	for _, w := range words {
		addSeededVector(vec, fnvSeed("w:"+w), 1.0)
	}
	for i := range vec {
		vec[i] /= float64(len(words))
	}

	// Small perturbation from the exact raw text.
	addSeededVector(vec, fnvSeed("raw:"+text), 0.05)

	return l2NormalizeToFloat32(vec)
}

// fnvSeed hashes s into a deterministic 64-bit seed suitable for seeding a
// PRNG. FNV-1a is used purely as a fast, deterministic, dependency-free
// hash — it is not a cryptographic hash and must never be used as one.
func fnvSeed(s string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return int64(h.Sum64())
}

// addSeededVector adds weight * u to dst in place, where u is a
// deterministic pseudo-random vector with entries uniform in [-1, 1]
// derived from seed.
func addSeededVector(dst []float64, seed int64, weight float64) {
	src := rand.New(rand.NewSource(seed))
	for i := range dst {
		dst[i] += weight * (src.Float64()*2 - 1)
	}
}

// l2NormalizeToFloat32 L2-normalizes v and converts it to float32. A
// (near-)zero-magnitude vector is returned converted but unnormalized to
// avoid dividing by zero.
func l2NormalizeToFloat32(v []float64) []float32 {
	var sumSq float64
	for _, x := range v {
		sumSq += x * x
	}
	norm := math.Sqrt(sumSq)
	out := make([]float32, len(v))
	if norm == 0 {
		return out
	}
	for i, x := range v {
		out[i] = float32(x / norm)
	}
	return out
}
