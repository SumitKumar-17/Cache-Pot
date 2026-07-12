// Package toolcache implements an exact-match cache for agent tool-call
// results (e.g. a GitHub/Slack/Jira API call), keyed by (tool name,
// canonicalized arguments) and shared across all connections/agents. It
// backs the TOOL.CACHE RESP command; see
// internal/server/resp/handlers_toolcache.go.
package toolcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ToolKey computes the stable cache key for a tool-call result: the
// hex-encoded SHA-256 of toolName + "\x00" + canonicalizedArgsJSON.
//
// argsJSON is canonicalized by unmarshaling into an any and re-marshaling
// (encoding/json marshals object keys in sorted order), so two JSON
// strings that differ only in key order produce the same key. An error is
// returned if argsJSON is not valid JSON — callers (the TOOL.CACHE RESP
// handler) should surface that as a RESP error rather than treating it as
// a cache miss.
func ToolKey(toolName, argsJSON string) (string, error) {
	var args any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("toolcache: invalid args JSON: %w", err)
	}
	canon, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("toolcache: re-marshal canonicalized args JSON: %w", err)
	}

	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write([]byte{0})
	h.Write(canon)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// entry is one cached tool-call result.
type entry struct {
	result    string
	expiresAt *time.Time
}

func (e *entry) expired(now time.Time) bool {
	return e.expiresAt != nil && !e.expiresAt.After(now)
}

// ToolCache is an exact-match cache keyed by an already-computed key
// string (see ToolKey) — it knows nothing about tool names or arguments
// itself, just key -> result. Its RESP-facing caller (TOOL.CACHE) is
// responsible for computing the key.
//
// Expiry works the same way as semantic.PromptCache: entries carry an
// optional absolute expiry time and are lazily evicted on read rather than
// reaped by a background goroutine.
//
// ToolCache is safe for concurrent use.
type ToolCache struct {
	mu      sync.RWMutex
	entries map[string]entry

	// now is overridable in tests to avoid real sleeps for TTL expiry.
	now func() time.Time
}

// New builds an empty ToolCache.
func New() *ToolCache {
	return &ToolCache{
		entries: make(map[string]entry),
		now:     time.Now,
	}
}

// Set stores result under key. ttl <= 0 means the entry never expires.
func (c *ToolCache) Set(key string, result string, ttl time.Duration) {
	var expiresAt *time.Time
	if ttl > 0 {
		t := c.now().Add(ttl)
		expiresAt = &t
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{result: result, expiresAt: expiresAt}
}

// Get looks up key, lazily evicting it first if it has expired.
func (c *ToolCache) Get(key string) (result string, found bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	expired := ok && e.expired(c.now())
	c.mu.RUnlock()

	if !ok {
		return "", false
	}
	if expired {
		c.mu.Lock()
		// Re-check under the write lock in case the entry was refreshed by
		// a concurrent Set between the RUnlock above and this Lock.
		if e2, ok2 := c.entries[key]; ok2 && e2.expired(c.now()) {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		return "", false
	}
	return e.result, true
}
