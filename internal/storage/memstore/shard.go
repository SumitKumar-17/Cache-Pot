package memstore

import (
	"sync"
	"time"
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
}

func newShard() *shard {
	return &shard{
		data:     make(map[string]*Entry),
		ttlKeys:  make(map[string]struct{}),
		versions: make(map[string]uint64),
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

// setEntryLocked stores e under key, maintaining the ttlKeys index. Caller
// must hold the write lock.
func (s *shard) setEntryLocked(key string, e *Entry) {
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

// deleteEntryLocked removes key from data and ttlKeys. Caller must hold the
// write lock. The version counter is intentionally left untouched by
// deletion bookkeeping here; callers that delete as part of a semantic
// mutation (DEL, expiry, etc.) call bumpVersionLocked explicitly.
func (s *shard) deleteEntryLocked(key string) {
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
