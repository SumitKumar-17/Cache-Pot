# AGENTS.md — internal/toolcache

Read the root `AGENTS.md` first; this file only covers what's specific to this package.

## Role

`internal/toolcache` implements `TOOL.CACHE`: an exact-match cache for agent tool-call
results (e.g. a GitHub/Slack/Jira API call's response), keyed by (tool name,
canonicalized arguments), shared across every connection/agent. Shipped in v0.2.0
alongside `internal/semantic`. No embedding, no LLM, no similarity — this is the
simplest cache in the codebase.

## Key types and invariants

- `ToolKey(toolName, argsJSON)`: SHA-256 of `toolName + "\x00" + canonicalizedArgsJSON`.
  Canonicalization is unmarshal-then-remarshal into `any` (relies on
  `encoding/json` sorting map keys), so `{"repo":"foo","number":42}` and
  `{"number":42,"repo":"foo"}` produce the *same* key — see
  `TestToolKeyArgOrderIndependent`. This is the exact-match counterpart to
  `internal/semantic.TemplateKey`; both live in different packages but use the identical
  canonicalization trick.
- `ToolCache` itself is a dumb `key -> result` map — it has no notion of tool names or
  argument shapes; `ToolKey` computation is the caller's (RESP handler's) job. Matching
  is exact string equality on the precomputed key, never a similarity/threshold check —
  contrast with `internal/semantic.SemanticCache`, which is approximate. If you're
  debugging a "should have hit but missed" bug here, the answer is almost always "the
  canonicalized argument JSON differs in some value, not just key order," not a scoring
  issue.
- Lazy TTL expiry (optional absolute `expiresAt`, evicted on read), same pattern as
  `semantic.PromptCache`. `Get` uses a read-lock-then-optionally-upgrade-to-write-lock
  pattern (`toolcache.go`'s `Get`) with a double-check under the write lock — read that
  carefully before changing the locking, it's there specifically to avoid a race where a
  concurrent `Set` refreshes the entry between the `RUnlock` and the `Lock`.

## Package-specific gotchas

- This package is intentionally the thinnest cache in the codebase — no partitioning
  by model/temperature (unlike `internal/semantic`), no embedding provider dependency,
  no cost tracking field on entries. Don't add any of those without a real product
  reason; a bug here is far more likely to be in the RESP handler computing `argsJSON`
  than in this package.

## Testing

```bash
go test ./internal/toolcache/...
```

`-race` matters if you touch `Get`'s lock-upgrade path — that's the one piece of
nontrivial concurrency logic in the package. No mocks/test doubles needed; everything
here is pure data structures and hashing, no external dependency to fake.

## Honest limitations

None beyond what's already in the root `AGENTS.md` (`TOOL.CACHE` is a global, unscoped
cache, not workspace-partitioned — see that file's v0.7.0 note).
