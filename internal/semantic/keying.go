package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// TemplateKey computes the stable cache key for a prompt-template cache
// entry: the hex-encoded SHA-256 of
// template + "\x00" + canonicalizedVariablesJSON + "\x00" + model.
//
// variablesJSON must be a JSON object; it is canonicalized by unmarshaling
// into a map[string]any and re-marshaling (encoding/json marshals map keys
// in sorted order), so two JSON strings that differ only in key order
// produce the same key. An error is returned if variablesJSON is not valid
// JSON — callers (the CACHE.PROMPT RESP handler) should surface that as a
// RESP error rather than treating it as a cache miss.
//
// Because the raw template string is folded directly into the hash,
// changing the template text is definitionally a different key: no
// separate cache-invalidation step is needed when a template is edited,
// entries tied to the old template text simply stop being addressable.
func TemplateKey(template, variablesJSON, model string) (string, error) {
	var vars map[string]any
	if err := json.Unmarshal([]byte(variablesJSON), &vars); err != nil {
		return "", fmt.Errorf("semantic: invalid variables JSON: %w", err)
	}
	canon, err := json.Marshal(vars)
	if err != nil {
		return "", fmt.Errorf("semantic: re-marshal canonicalized variables JSON: %w", err)
	}

	h := sha256.New()
	h.Write([]byte(template))
	h.Write([]byte{0})
	h.Write(canon)
	h.Write([]byte{0})
	h.Write([]byte(model))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// promptEntry is one exact-match cached response.
type promptEntry struct {
	response  string
	expiresAt *time.Time
}

// PromptCache is an exact-match cache keyed by an already-computed key
// string (see TemplateKey) — it knows nothing about templates, variables,
// or models itself, just key -> response. Its RESP-facing caller
// (CACHE.PROMPT) is responsible for computing the key.
//
// Expiry works the same way as SemanticCache: entries carry an optional
// absolute expiry time and are lazily evicted on read rather than reaped
// by a background goroutine.
//
// PromptCache is safe for concurrent use.
type PromptCache struct {
	mu      sync.Mutex
	entries map[string]promptEntry

	// now is overridable in tests to avoid real sleeps for TTL expiry.
	now func() time.Time
}

// NewPromptCache builds an empty PromptCache.
func NewPromptCache() *PromptCache {
	return &PromptCache{
		entries: make(map[string]promptEntry),
		now:     time.Now,
	}
}

// Set stores response under key. ttl <= 0 means the entry never expires.
func (p *PromptCache) Set(key string, response string, ttl time.Duration) {
	var expiresAt *time.Time
	if ttl > 0 {
		t := p.now().Add(ttl)
		expiresAt = &t
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[key] = promptEntry{response: response, expiresAt: expiresAt}
}

// Get looks up key, lazily evicting it first if it has expired.
func (p *PromptCache) Get(key string) (response string, found bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e, ok := p.entries[key]
	if !ok {
		return "", false
	}
	if e.expired(p.now()) {
		delete(p.entries, key)
		return "", false
	}
	return e.response, true
}

func (e *promptEntry) expired(now time.Time) bool {
	return e.expiresAt != nil && !e.expiresAt.After(now)
}
