package semantic

// This file documents (but does not yet implement — Phase 2) how semantic
// cache keys will be derived.
//
// Unlike Phase 1's exact-match string keys, a semantic cache key must
// capture everything that can change what counts as a valid cache hit for
// an LLM call: the model identifier (different models produce different
// distributions of responses to the same prompt), the prompt's embedding
// (for approximate/similarity matching rather than exact string equality),
// and generation parameters that affect determinism — most importantly
// temperature (a high-temperature request should probably not be served
// from a low-temperature cache entry, and vice versa). The Phase 2 design
// will likely combine: (1) an exact-match fast path keyed on
// (model, temperature-bucket, normalized-prompt-hash), and (2) a fallback
// nearest-neighbor lookup via internal/vector.Index scoped to
// (model, temperature-bucket) when the fast path misses, accepting a hit
// above some similarity threshold. None of this is implemented yet.
