# AGENTS.md — internal/semantic

Read the root `AGENTS.md` first; this file only covers what's specific to this package.

## Role

`internal/semantic` implements two caches on top of `internal/embed.Provider`, both
shipped in v0.2.0:
- `SemanticCache` (`cache.go`) — `CACHE.SEMANTIC`: similarity-based cache for LLM
  responses. A query prompt hits if its cosine similarity to some previously-stored
  prompt (in the same model+temperature partition) clears a caller-supplied threshold.
- `PromptCache` (`keying.go`) — `CACHE.PROMPT`: exact-match cache keyed by a SHA-256 of
  (template + canonicalized variables JSON + model). No embedding involved at all.

## Key types and invariants

- **Partitioning**: `SemanticCache` entries are grouped by `"model\x00temperature"`.
  Identical prompt text against a different model, or the same model at a different
  temperature, is always a miss — they're different partitions, never compared against
  each other. See `TestSemanticCacheDifferentModelPartition` /
  `TestSemanticCacheDifferentTempPartition`.
- **Similarity-threshold hit logic** (`SemanticCache.Get`): brute-force linear scan of
  every non-expired entry in the matching partition, cosine similarity against each,
  keep the best score, hit only if `bestScore >= threshold`. This is the opposite
  matching philosophy from `internal/toolcache`'s exact canonicalized-argument
  hashing — semantic cache hits are approximate/threshold-based and require an embedding
  call on every `Get`; toolcache hits are exact and require no embedding at all. Don't
  conflate the two when fixing a "cache miss when I expected a hit" bug — check which
  package you're actually in first.
- **Lazy expiry, not a reaper**: both caches store an optional absolute `expiresAt` and
  evict expired entries only when encountered during a `Get`/scan — no background
  goroutine. `SemanticCache.Get` does this compaction in place (`kept := entries[:0]`,
  safe because the write index never runs ahead of the read index).
- **`TemplateKey`** canonicalizes `variablesJSON` by unmarshal-then-remarshal into
  `map[string]any` (Go's `encoding/json` sorts map keys), so key order in the input JSON
  never affects the cache key — verified by `TestTemplateKeyVariableOrderIndependent`.
  Changing the raw template *text* is definitionally a new key/new cache entries; there
  is no separate invalidation step.
- **`cost` plumbing**: both `Set` methods take an optional caller-reported `cost` and
  both `Get` methods return the hit entry's stored cost. `internal/semantic` does **not**
  import `internal/analytics` — it just carries the number back out; the RESP/MCP layer
  holding the shared `*analytics.Tracker` records savings itself. Don't add an
  analytics import here to "simplify" this — it's a deliberate layering decision.
- Both caches are safe for concurrent use via an internal `sync.Mutex`; `now` is an
  overridable field (defaults to `time.Now`) purely so TTL tests don't need real sleeps
  for every case — some tests (see `TestSemanticCacheTTLExpiry`) still use real short
  sleeps rather than overriding `now`, which is fine, just be aware both patterns exist.

## Package-specific gotchas

- `SemanticCache.Get` embeds the query prompt on *every call* (it needs the vector to
  compare) — a cache miss still costs one embedding call. This is unavoidable given the
  design; don't "optimize" it away without understanding this is inherent to
  similarity-based lookup, not a bug.
- The O(n) per-partition scan is intentional (see `cache.go`'s doc comment) — the
  project's stated approach is "flat index first, ANN later," matching
  `internal/vector.Store`'s own brute-force design. Don't silently replace it with an
  ANN structure without discussing the tradeoff (see root `AGENTS.md`'s "Bounded
  sampling over full scans" convention).

## Testing

```bash
go test ./internal/semantic/...
```

`-race` matters if you touch locking in `cache.go`/`keying.go` — both types guard shared
maps with a mutex under concurrent `Set`/`Get`, run the repo-wide `go test ./... -race`
sweep after any change there. Tests use `embed.NewMock(8)` exclusively (see
`newTestCache()` in `cache_test.go`) — no real embedding provider or network access
needed. The mock's documented near-duplicate behavior (case/whitespace variants score
close but not identical) is directly exercised by
`TestSemanticCacheCaseWhitespaceVariantHit`.

## Honest limitations

- `SemanticCache` has no cross-partition search — a prompt cached under one
  (model, temperature) pair is genuinely unreachable from any other pair, by design, not
  as an oversight to fix.
