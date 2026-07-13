package memstore

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/eviction"
)

// shard is one partition of the overall keyspace: a plain map guarded by its
// own RWMutex. Store routes each (workspace, key) pair to exactly one shard
// by hashing the composite namespaced key.
type shard struct {
	mu   sync.RWMutex
	data map[string]*Entry

	// ttlKeys tracks the subset of composite keys that currently have a
	// non-nil ExpiresAt, so the TTL reaper can sample "keys with TTL"
	// directly instead of scanning the whole shard every tick.
	ttlKeys map[string]struct{}

	// versions is a per-key monotonic mutation counter used by WATCH/EXEC.
	// It is intentionally NOT stored on Entry: a key's version must keep
	// incrementing across delete+recreate cycles (Redis semantics: DEL then
	// SET on a watched key still aborts the transaction), so it is tracked
	// independently of the Entry object's lifetime.
	versions map[string]uint64

	// entryCount points at the owning Store's global entry-count counter.
	// setEntryLocked/deleteEntryLocked adjust it atomically whenever a key
	// is genuinely inserted/removed (not just updated in place), so
	// Store.EntryCount() stays exact across every code path that writes or
	// deletes from any shard's data map -- passive/active TTL expiry, DEL,
	// FLUSHDB/FLUSHALL, RENAME, and the eviction trigger itself -- without
	// each of those call sites needing to remember to update it themselves.
	entryCount *int64
}

func newShard(entryCount *int64) *shard {
	return &shard{
		data:       make(map[string]*Entry),
		ttlKeys:    make(map[string]struct{}),
		versions:   make(map[string]uint64),
		entryCount: entryCount,
	}
}

// getLocked returns the live (non-expired) entry for key, or nil. Caller
// must hold at least a read lock.
func (s *shard) getLocked(key string, now time.Time) *Entry {
	e, ok := s.data[key]
	if !ok || e.expired(now) {
		return nil
	}
	return e
}

// get performs a passively-expiring read of key under a read lock.
func (s *shard) get(key string, now time.Time) *Entry {
	s.mu.RLock()
	e := s.getLocked(key, now)
	s.mu.RUnlock()
	return e
}

// getForWriteLocked returns the live entry for key, lazily deleting it first
// if expired. Caller must already hold the write lock.
func (s *shard) getForWriteLocked(key string, now time.Time) *Entry {
	e, ok := s.data[key]
	if !ok {
		return nil
	}
	if e.expired(now) {
		s.deleteEntryLocked(key)
		s.bumpVersionLocked(key)
		return nil
	}
	return e
}

// setEntryLocked stores e under key, maintaining the ttlKeys index and the
// global entry count (incremented only when key did not already exist --
// an update to an existing key is not a new entry). Caller must hold the
// write lock.
func (s *shard) setEntryLocked(key string, e *Entry) {
	if _, existed := s.data[key]; !existed {
		atomic.AddInt64(s.entryCount, 1)
	}
	s.data[key] = e
	if e.ExpiresAt != nil {
		s.ttlKeys[key] = struct{}{}
	} else {
		delete(s.ttlKeys, key)
	}
	s.bumpVersionLocked(key)
}

// setExpiryLocked updates e's ExpiresAt and the ttlKeys index. Caller must
// hold the write lock.
func (s *shard) setExpiryLocked(key string, e *Entry, at *time.Time) {
	e.ExpiresAt = at
	if at != nil {
		s.ttlKeys[key] = struct{}{}
	} else {
		delete(s.ttlKeys, key)
	}
	s.bumpVersionLocked(key)
}

// deleteEntryLocked removes key from data and ttlKeys, decrementing the
// global entry count iff key was actually present (deleting an
// already-absent key must not under-count). Caller must hold the write
// lock. The version counter is intentionally left untouched by deletion
// bookkeeping here; callers that delete as part of a semantic mutation
// (DEL, expiry, etc.) call bumpVersionLocked explicitly.
func (s *shard) deleteEntryLocked(key string) {
	if _, existed := s.data[key]; existed {
		atomic.AddInt64(s.entryCount, -1)
	}
	delete(s.data, key)
	delete(s.ttlKeys, key)
}

// bumpVersionLocked increments the WATCH version counter for key. Caller
// must hold the write lock.
func (s *shard) bumpVersionLocked(key string) uint64 {
	v := s.versions[key] + 1
	s.versions[key] = v
	return v
}

// versionLocked returns the current WATCH version counter for key (0 if
// never mutated). Caller must hold at least a read lock.
func (s *shard) versionLocked(key string) uint64 {
	return s.versions[key]
}

// sweepSampleLocked examines up to sampleSize keys from ttlKeys and deletes
// any that have expired. Returns the number removed. Caller must hold the
// write lock. Go's random map iteration order gives this a natural
// "sample" quality without needing an explicit RNG.
func (s *shard) sweepSampleLocked(sampleSize int, now time.Time) int {
	removed := 0
	examined := 0
	for key := range s.ttlKeys {
		if examined >= sampleSize {
			break
		}
		examined++
		if e, ok := s.data[key]; ok && e.expired(now) {
			s.deleteEntryLocked(key)
			s.bumpVersionLocked(key)
			removed++
		}
	}
	return removed
}

// pickEvictionVictimLocked samples up to sampleSize existing entries in
// this shard (all of them if the shard has fewer), scores each via policy,
// and returns the composite key of the single highest-scoring one (the
// "most evictable" per the policy's convention), or "" if the shard is
// empty. Caller must hold the write lock.
//
// This is intentionally approximate/local -- a bounded sample of one
// shard, not a global exact sort over the whole keyspace -- mirroring the
// same "bounded-sample first, exact global structures are future work if
// ever needed" philosophy internal/storage/ttl's reaper already
// established for TTL expiry in this codebase.
func (s *shard) pickEvictionVictimLocked(policy eviction.Policy, now time.Time, sampleSize int) string {
	var victimKey string
	var victimScore float64
	haveVictim := false

	examined := 0
	for key, e := range s.data {
		if examined >= sampleSize {
			break
		}
		examined++
		score := policy.Score(eviction.Signals{
			LastAccess:  e.LastAccess,
			Now:         now,
			AccessCount: e.AccessCount,
		})
		if !haveVictim || score > victimScore {
			victimKey = key
			victimScore = score
			haveVictim = true
		}
	}
	return victimKey
}
