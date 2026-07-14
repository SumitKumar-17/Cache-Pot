// Package memstore is Cache-Pot's in-memory implementation of
// internal/storage.Engine: a sharded map with passive + active TTL expiry.
package memstore

import (
	"bytes"
	"context"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/eviction"
	"github.com/SumitKumar-17/cache-pot/internal/storage"
	"github.com/SumitKumar-17/cache-pot/internal/storage/ttl"
)

// Store is the sharded, in-memory Engine implementation. It satisfies
// storage.Engine.
type Store struct {
	shards []*shard
	n      int
	now    func() time.Time

	// globalMu serializes MULTI/EXEC transaction bodies (see Exec). This is a
	// simplicity-over-throughput tradeoff: a single global mutex instead of
	// a cross-shard locking protocol, to avoid lock-ordering/deadlock
	// complexity for what remains a low-throughput feature.
	// Revisit if transaction throughput ever becomes a bottleneck.
	globalMu sync.Mutex

	reaperCancel context.CancelFunc
	reaperDone   chan struct{}

	// entryCount is the exact, server-wide (not per-workspace) total
	// number of live entries across all shards. Every shard mutates it
	// atomically as part of setEntryLocked/deleteEntryLocked, so it stays
	// accurate across every insert/delete path (SET, HSET/LPUSH/etc.
	// first-write, DEL, FLUSHDB/FLUSHALL, RENAME, and TTL-reaper-driven
	// expiry) without each of those call sites needing to remember to
	// touch it themselves.
	entryCount int64

	// maxEntries is the maxmemory-style bound on entryCount: 0 means
	// unlimited (eviction disabled), matching this project's "0 means off"
	// convention (e.g. --mcp-port 0).
	maxEntries int

	// policy scores entries for eviction eligibility when maxEntries is
	// exceeded. Defaults to eviction.LRU{} even when maxEntries == 0,
	// since it's simply unused in that case.
	policy eviction.Policy

	// onEvict, if non-nil, is called exactly once per evicted entry. This
	// is how eviction visibility reaches internal/observability without
	// this package importing it: the caller (internal/server) passes a
	// plain callback (e.g. Metrics.KeyEvicted) instead.
	onEvict func()
}

var _ storage.Engine = (*Store)(nil)

const (
	defaultNumShards = 32

	// evictionSampleSize bounds how many existing entries in the target
	// shard are examined when the maxEntries cap is hit, mirroring
	// internal/storage/ttl's reaper sample size for the same
	// bounded-sampling philosophy.
	evictionSampleSize = 20
)

// Option configures optional Store behavior not needed by most callers
// (tests, and any caller happy with an unbounded store using the default
// LRU policy) -- see WithMaxEntries, WithEvictionPolicy, and WithOnEvict.
type Option func(*Store)

// WithMaxEntries sets the server-wide entry-count cap that triggers
// eviction. n <= 0 means unlimited (the default).
func WithMaxEntries(n int) Option {
	return func(s *Store) { s.maxEntries = n }
}

// WithEvictionPolicy sets the Policy used to pick an eviction victim once
// maxEntries is exceeded. Ignored (but harmless) if maxEntries is left at
// its default of 0.
func WithEvictionPolicy(p eviction.Policy) Option {
	return func(s *Store) {
		if p != nil {
			s.policy = p
		}
	}
}

// WithOnEvict sets a callback invoked exactly once per entry the store
// evicts. nil-safe if never set.
func WithOnEvict(f func()) Option {
	return func(s *Store) { s.onEvict = f }
}

// New creates a Store with numShards shards (defaulting to 32 if <= 0) and
// starts its background TTL reaper goroutine. Call Close to stop it. With
// no options, eviction is disabled (maxEntries == 0, unbounded) -- the same
// behavior this constructor has always had.
func New(numShards int, opts ...Option) *Store {
	return newStore(numShards, time.Now, opts...)
}

// NewWithClock is like New but lets callers (tests) inject a deterministic
// clock instead of time.Now, so TTL-expiry (and eviction-recency) behavior
// can be tested without real time.Sleep calls.
func NewWithClock(numShards int, clock func() time.Time, opts ...Option) *Store {
	return newStore(numShards, clock, opts...)
}

func newStore(numShards int, clock func() time.Time, opts ...Option) *Store {
	if numShards <= 0 {
		numShards = defaultNumShards
	}
	s := &Store{n: numShards, now: clock, policy: eviction.LRU{}}
	for _, opt := range opts {
		opt(s)
	}

	shards := make([]*shard, numShards)
	for i := range shards {
		shards[i] = newShard(&s.entryCount)
	}
	s.shards = shards

	ctx, cancel := context.WithCancel(context.Background())
	s.reaperCancel = cancel
	s.reaperDone = make(chan struct{})
	reaper := ttl.New(s, 100*time.Millisecond, 20)
	go func() {
		defer close(s.reaperDone)
		reaper.Run(ctx)
	}()

	return s
}

// EntryCount returns the current exact, server-wide total number of live
// entries across all shards.
func (s *Store) EntryCount() int64 {
	return atomic.LoadInt64(&s.entryCount)
}

// evictIfNeededLocked evicts at most one entry from sh if inserting one
// brand-new key would push the total entry count over maxEntries. It is a
// no-op when maxEntries <= 0 (eviction disabled) or the cap wouldn't be
// exceeded. Caller must hold sh's write lock and must only call this
// immediately before inserting a genuinely new key (not an update to an
// existing one) into that same shard -- eviction always happens in the
// shard receiving the new key, never a different one.
//
// Caveat this implies (confirmed by manual testing): because the victim
// search never leaves the receiving shard, this eviction attempt is a
// no-op whenever that particular shard happens to be empty, even though
// the global count is over the cap -- entryCount can then grow past
// maxEntries until a later insert lands in a shard that already holds
// something to evict. In the steady state this means the resident key
// count floor is effectively max(maxEntries, roughly one entry per
// populated shard), not an exact maxEntries -- e.g. with the default 32
// shards, --max-entries 5 converges to roughly 32 resident keys, not 5,
// since eviction only "bites" once a shard already has an occupant. This
// is the accepted cost of the same bounded/local-sampling philosophy
// internal/storage/ttl's reaper already uses (no global exact structure);
// callers wanting a cap below the shard count should size numShards
// accordingly.
func (s *Store) evictIfNeededLocked(sh *shard, now time.Time) {
	if s.maxEntries <= 0 {
		return
	}
	if atomic.LoadInt64(&s.entryCount)+1 <= int64(s.maxEntries) {
		return
	}
	victim := sh.pickEvictionVictimLocked(s.policy, now, evictionSampleSize)
	if victim == "" {
		return
	}
	sh.deleteEntryLocked(victim)
	sh.bumpVersionLocked(victim)
	if s.onEvict != nil {
		s.onEvict()
	}
}

// Close stops the background TTL reaper. Safe to call once.
func (s *Store) Close() error {
	if s.reaperCancel != nil {
		s.reaperCancel()
		<-s.reaperDone
	}
	return nil
}

// NumShards implements ttl.Sweepable.
func (s *Store) NumShards() int { return s.n }

// SweepShard implements ttl.Sweepable.
func (s *Store) SweepShard(shardIndex int, sampleSize int) int {
	sh := s.shards[shardIndex]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.sweepSampleLocked(sampleSize, s.now())
}

// --- key routing -----------------------------------------------------------

// nsKey builds the composite, workspace-namespaced key used internally as
// the map key within a shard. NUL is not a legal character in a RESP bulk
// string used as a key in practice, which makes it a safe separator.
func nsKey(workspace, key string) string {
	return workspace + "\x00" + key
}

func (s *Store) shardIndex(ck string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(ck))
	return int(h.Sum32() % uint32(s.n))
}

func (s *Store) shardFor(ck string) *shard {
	return s.shards[s.shardIndex(ck)]
}

func normalizeIndex(idx, n int) int {
	if idx < 0 {
		idx = n + idx
	}
	return idx
}

// ensureKind returns the live entry for ck, creating an empty one of the
// given kind if absent, or storage.ErrWrongType if it exists as a different
// kind. Creating a brand-new entry is the one path here that can grow the
// total entry count, so it's where the eviction trigger runs (see
// Store.evictIfNeededLocked). Caller must hold the shard's write lock.
func ensureKind(s *Store, sh *shard, ck string, kind Kind, now time.Time) (*Entry, error) {
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		s.evictIfNeededLocked(sh, now)
		e = newEmptyEntry(kind, now)
		sh.setEntryLocked(ck, e)
		return e, nil
	}
	if e.Kind != kind {
		return nil, storage.ErrWrongType
	}
	return e, nil
}

func newEmptyEntry(kind Kind, now time.Time) *Entry {
	e := &Entry{Kind: kind, LastAccess: now}
	switch kind {
	case KindHash:
		e.Hash = make(map[string][]byte)
	case KindSet:
		e.Set = make(map[string]struct{})
	case KindZSet:
		e.ZSet = make(map[string]float64)
	}
	return e
}

// --- generic key ops ---------------------------------------------------

func (s *Store) Get(workspace, key string) ([]byte, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, false, nil
	}
	if e.Kind != KindString {
		return nil, false, storage.ErrWrongType
	}
	e.LastAccess = now
	e.AccessCount++
	return append([]byte(nil), e.StringVal...), true, nil
}

func (s *Store) Set(workspace, key string, val []byte, opts storage.SetOpts) (bool, []byte, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()

	existing := sh.getForWriteLocked(ck, now)
	exists := existing != nil

	// Compute the "old value" (for the GET option) before evaluating
	// NX/XX, since Redis's SET ... GET returns the previous value
	// regardless of whether NX/XX ends up blocking the write.
	var prevVal []byte
	hadPrev := false
	if opts.GetOld && exists {
		if existing.Kind != KindString {
			return false, nil, false, storage.ErrWrongType
		}
		prevVal = append([]byte(nil), existing.StringVal...)
		hadPrev = true
	}

	if opts.OnlyIfNX && exists {
		return false, prevVal, hadPrev, nil
	}
	if opts.OnlyIfXX && !exists {
		return false, prevVal, hadPrev, nil
	}

	var expiresAt *time.Time
	if opts.TTL > 0 {
		t := now.Add(opts.TTL)
		expiresAt = &t
	}

	if !exists {
		s.evictIfNeededLocked(sh, now)
	}
	e := &Entry{Kind: KindString, StringVal: append([]byte(nil), val...), ExpiresAt: expiresAt, LastAccess: now, AccessCount: 1}
	sh.setEntryLocked(ck, e)
	return true, prevVal, hadPrev, nil
}

func (s *Store) Del(workspace string, keys ...string) int {
	now := s.now()
	deleted := 0
	for _, key := range keys {
		ck := nsKey(workspace, key)
		sh := s.shardFor(ck)
		sh.mu.Lock()
		if e := sh.getForWriteLocked(ck, now); e != nil {
			sh.deleteEntryLocked(ck)
			sh.bumpVersionLocked(ck)
			deleted++
		}
		sh.mu.Unlock()
	}
	return deleted
}

func (s *Store) Exists(workspace string, keys ...string) int {
	now := s.now()
	count := 0
	for _, key := range keys {
		ck := nsKey(workspace, key)
		sh := s.shardFor(ck)
		if sh.get(ck, now) != nil {
			count++
		}
	}
	return count
}

func (s *Store) Expire(workspace, key string, ttlDur time.Duration) bool {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return false
	}
	if ttlDur <= 0 {
		sh.deleteEntryLocked(ck)
		sh.bumpVersionLocked(ck)
		return true
	}
	t := now.Add(ttlDur)
	sh.setExpiryLocked(ck, e, &t)
	return true
}

func (s *Store) TTL(workspace, key string) (time.Duration, bool, bool) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, false, false
	}
	if e.ExpiresAt == nil {
		return 0, false, true
	}
	remaining := e.ExpiresAt.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true, true
}

func (s *Store) Persist(workspace, key string) bool {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil || e.ExpiresAt == nil {
		return false
	}
	sh.setExpiryLocked(ck, e, nil)
	return true
}

func (s *Store) Type(workspace, key string) (string, bool) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	e := sh.get(ck, now)
	if e == nil {
		return "", false
	}
	return e.Kind.String(), true
}

func (s *Store) Rename(workspace, oldKey, newKey string) bool {
	ock := nsKey(workspace, oldKey)
	nck := nsKey(workspace, newKey)
	now := s.now()

	oi := s.shardIndex(ock)
	ni := s.shardIndex(nck)

	if oi == ni {
		sh := s.shards[oi]
		sh.mu.Lock()
		defer sh.mu.Unlock()
		e := sh.getForWriteLocked(ock, now)
		if e == nil {
			return false
		}
		sh.deleteEntryLocked(ock)
		sh.setEntryLocked(nck, e)
		sh.bumpVersionLocked(ock)
		return true
	}

	// Lock shards in ascending index order to avoid lock-order deadlocks
	// against a concurrent rename in the opposite direction.
	first, second := oi, ni
	if first > second {
		first, second = second, first
	}
	s.shards[first].mu.Lock()
	defer s.shards[first].mu.Unlock()
	s.shards[second].mu.Lock()
	defer s.shards[second].mu.Unlock()

	oldSh := s.shards[oi]
	newSh := s.shards[ni]
	e := oldSh.getForWriteLocked(ock, now)
	if e == nil {
		return false
	}
	oldSh.deleteEntryLocked(ock)
	newSh.setEntryLocked(nck, e)
	oldSh.bumpVersionLocked(ock)
	return true
}

func (s *Store) keysForWorkspace(workspace string, now time.Time) []string {
	prefix := workspace + "\x00"
	var out []string
	for _, sh := range s.shards {
		sh.mu.RLock()
		for ck, e := range sh.data {
			if !strings.HasPrefix(ck, prefix) {
				continue
			}
			if e.expired(now) {
				continue
			}
			out = append(out, ck[len(prefix):])
		}
		sh.mu.RUnlock()
	}
	return out
}

func (s *Store) Keys(workspace, pattern string) []string {
	now := s.now()
	all := s.keysForWorkspace(workspace, now)
	if pattern == "" || pattern == "*" {
		sort.Strings(all)
		return all
	}
	out := make([]string, 0, len(all))
	for _, k := range all {
		if globMatch(pattern, k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Scan implements a simplification of Redis SCAN: it recomputes and
// sorts the full matching keyspace on every call and uses the sort position
// as the cursor. This gives a stable, deterministic ordering (handy for
// tests) at the cost of being O(n log n) per call; Redis's real reverse
// binary iteration (stable under resizes, no full recompute) can replace
// this if key counts get large enough to matter.
func (s *Store) Scan(workspace string, cursor uint64, match string, count int) (uint64, []string) {
	if count <= 0 {
		count = 10
	}
	now := s.now()
	all := s.keysForWorkspace(workspace, now)
	sort.Strings(all)

	if cursor >= uint64(len(all)) {
		return 0, nil
	}
	end := cursor + uint64(count)
	if end > uint64(len(all)) {
		end = uint64(len(all))
	}
	out := make([]string, 0, end-cursor)
	for _, k := range all[cursor:end] {
		if match == "" || match == "*" || globMatch(match, k) {
			out = append(out, k)
		}
	}
	next := end
	if next >= uint64(len(all)) {
		next = 0
	}
	return next, out
}

func (s *Store) FlushDB(workspace string) {
	prefix := workspace + "\x00"
	for _, sh := range s.shards {
		sh.mu.Lock()
		for ck := range sh.data {
			if strings.HasPrefix(ck, prefix) {
				sh.deleteEntryLocked(ck)
				sh.bumpVersionLocked(ck)
			}
		}
		sh.mu.Unlock()
	}
}

// --- hash ---------------------------------------------------------------

func (s *Store) HSet(workspace, key string, fields map[string][]byte) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, err := ensureKind(s, sh, ck, KindHash, now)
	if err != nil {
		return 0, err
	}
	added := 0
	for f, v := range fields {
		if _, exists := e.Hash[f]; !exists {
			added++
		}
		e.Hash[f] = append([]byte(nil), v...)
	}
	e.LastAccess = now
	e.AccessCount++
	sh.bumpVersionLocked(ck)
	return added, nil
}

func (s *Store) HGet(workspace, key, field string) ([]byte, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, false, nil
	}
	if e.Kind != KindHash {
		return nil, false, storage.ErrWrongType
	}
	v, ok := e.Hash[field]
	if !ok {
		return nil, false, nil
	}
	e.LastAccess = now
	e.AccessCount++
	return append([]byte(nil), v...), true, nil
}

func (s *Store) HGetAll(workspace, key string) (map[string][]byte, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, false, nil
	}
	if e.Kind != KindHash {
		return nil, false, storage.ErrWrongType
	}
	out := make(map[string][]byte, len(e.Hash))
	for k, v := range e.Hash {
		out[k] = append([]byte(nil), v...)
	}
	return out, true, nil
}

func (s *Store) HDel(workspace, key string, fields ...string) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, nil
	}
	if e.Kind != KindHash {
		return 0, storage.ErrWrongType
	}
	removed := 0
	for _, f := range fields {
		if _, ok := e.Hash[f]; ok {
			delete(e.Hash, f)
			removed++
		}
	}
	if removed > 0 {
		if len(e.Hash) == 0 {
			sh.deleteEntryLocked(ck)
		}
		sh.bumpVersionLocked(ck)
	}
	return removed, nil
}

func (s *Store) HExists(workspace, key, field string) (bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return false, nil
	}
	if e.Kind != KindHash {
		return false, storage.ErrWrongType
	}
	_, ok := e.Hash[field]
	return ok, nil
}

func (s *Store) HKeys(workspace, key string) ([]string, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, false, nil
	}
	if e.Kind != KindHash {
		return nil, false, storage.ErrWrongType
	}
	out := make([]string, 0, len(e.Hash))
	for k := range e.Hash {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, true, nil
}

func (s *Store) HVals(workspace, key string) ([][]byte, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, false, nil
	}
	if e.Kind != KindHash {
		return nil, false, storage.ErrWrongType
	}
	keys := make([]string, 0, len(e.Hash))
	for k := range e.Hash {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][]byte, 0, len(keys))
	for _, k := range keys {
		out = append(out, append([]byte(nil), e.Hash[k]...))
	}
	return out, true, nil
}

func (s *Store) HLen(workspace, key string) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, nil
	}
	if e.Kind != KindHash {
		return 0, storage.ErrWrongType
	}
	return len(e.Hash), nil
}

func (s *Store) HMGet(workspace, key string, fields ...string) ([][]byte, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	out := make([][]byte, len(fields))
	if e == nil {
		return out, nil
	}
	if e.Kind != KindHash {
		return nil, storage.ErrWrongType
	}
	for i, f := range fields {
		if v, ok := e.Hash[f]; ok {
			out[i] = append([]byte(nil), v...)
		}
	}
	return out, nil
}

func (s *Store) HIncrBy(workspace, key, field string, delta int64) (int64, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, err := ensureKind(s, sh, ck, KindHash, now)
	if err != nil {
		return 0, err
	}
	cur := int64(0)
	if v, ok := e.Hash[field]; ok {
		n, perr := strconv.ParseInt(string(v), 10, 64)
		if perr != nil {
			return 0, storage.ErrNotInteger
		}
		cur = n
	}
	cur += delta
	e.Hash[field] = []byte(strconv.FormatInt(cur, 10))
	e.LastAccess = now
	e.AccessCount++
	sh.bumpVersionLocked(ck)
	return cur, nil
}

// --- list -----------------------------------------------------------------

func (s *Store) LPush(workspace, key string, vals ...[]byte) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, err := ensureKind(s, sh, ck, KindList, now)
	if err != nil {
		return 0, err
	}
	for _, v := range vals {
		e.List = append([][]byte{append([]byte(nil), v...)}, e.List...)
	}
	e.LastAccess = now
	e.AccessCount++
	sh.bumpVersionLocked(ck)
	return len(e.List), nil
}

func (s *Store) RPush(workspace, key string, vals ...[]byte) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, err := ensureKind(s, sh, ck, KindList, now)
	if err != nil {
		return 0, err
	}
	for _, v := range vals {
		e.List = append(e.List, append([]byte(nil), v...))
	}
	e.LastAccess = now
	e.AccessCount++
	sh.bumpVersionLocked(ck)
	return len(e.List), nil
}

func (s *Store) LPop(workspace, key string) ([]byte, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, false, nil
	}
	if e.Kind != KindList {
		return nil, false, storage.ErrWrongType
	}
	if len(e.List) == 0 {
		return nil, false, nil
	}
	v := e.List[0]
	e.List = e.List[1:]
	if len(e.List) == 0 {
		sh.deleteEntryLocked(ck)
	}
	sh.bumpVersionLocked(ck)
	return v, true, nil
}

func (s *Store) RPop(workspace, key string) ([]byte, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, false, nil
	}
	if e.Kind != KindList {
		return nil, false, storage.ErrWrongType
	}
	if len(e.List) == 0 {
		return nil, false, nil
	}
	v := e.List[len(e.List)-1]
	e.List = e.List[:len(e.List)-1]
	if len(e.List) == 0 {
		sh.deleteEntryLocked(ck)
	}
	sh.bumpVersionLocked(ck)
	return v, true, nil
}

func (s *Store) LRange(workspace, key string, start, stop int) ([][]byte, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return [][]byte{}, nil
	}
	if e.Kind != KindList {
		return nil, storage.ErrWrongType
	}
	n := len(e.List)
	start = normalizeIndex(start, n)
	stop = normalizeIndex(stop, n)
	if start < 0 {
		start = 0
	}
	if stop >= n {
		stop = n - 1
	}
	if start > stop || n == 0 {
		return [][]byte{}, nil
	}
	out := make([][]byte, 0, stop-start+1)
	for i := start; i <= stop; i++ {
		out = append(out, append([]byte(nil), e.List[i]...))
	}
	return out, nil
}

func (s *Store) LLen(workspace, key string) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, nil
	}
	if e.Kind != KindList {
		return 0, storage.ErrWrongType
	}
	return len(e.List), nil
}

func (s *Store) LIndex(workspace, key string, index int) ([]byte, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, false, nil
	}
	if e.Kind != KindList {
		return nil, false, storage.ErrWrongType
	}
	n := len(e.List)
	idx := normalizeIndex(index, n)
	if idx < 0 || idx >= n {
		return nil, false, nil
	}
	return append([]byte(nil), e.List[idx]...), true, nil
}

func (s *Store) LSet(workspace, key string, index int, val []byte) error {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return storage.ErrNoSuchKey
	}
	if e.Kind != KindList {
		return storage.ErrWrongType
	}
	n := len(e.List)
	idx := normalizeIndex(index, n)
	if idx < 0 || idx >= n {
		return storage.ErrIndexOutOfRange
	}
	e.List[idx] = append([]byte(nil), val...)
	sh.bumpVersionLocked(ck)
	return nil
}

func (s *Store) LRem(workspace, key string, count int, val []byte) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, nil
	}
	if e.Kind != KindList {
		return 0, storage.ErrWrongType
	}

	removed := 0
	var out [][]byte
	if count >= 0 {
		out = make([][]byte, 0, len(e.List))
		for _, v := range e.List {
			if bytes.Equal(v, val) && (count == 0 || removed < count) {
				removed++
				continue
			}
			out = append(out, v)
		}
	} else {
		limit := -count
		tmp := make([][]byte, 0, len(e.List))
		for i := len(e.List) - 1; i >= 0; i-- {
			v := e.List[i]
			if bytes.Equal(v, val) && removed < limit {
				removed++
				continue
			}
			tmp = append(tmp, v)
		}
		out = make([][]byte, len(tmp))
		for i, v := range tmp {
			out[len(tmp)-1-i] = v
		}
	}

	e.List = out
	if removed > 0 {
		if len(e.List) == 0 {
			sh.deleteEntryLocked(ck)
		}
		sh.bumpVersionLocked(ck)
	}
	return removed, nil
}

// --- set --------------------------------------------------------------

func (s *Store) SAdd(workspace, key string, members ...[]byte) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, err := ensureKind(s, sh, ck, KindSet, now)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, m := range members {
		k := string(m)
		if _, ok := e.Set[k]; !ok {
			e.Set[k] = struct{}{}
			added++
		}
	}
	e.LastAccess = now
	e.AccessCount++
	sh.bumpVersionLocked(ck)
	return added, nil
}

func (s *Store) SRem(workspace, key string, members ...[]byte) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, nil
	}
	if e.Kind != KindSet {
		return 0, storage.ErrWrongType
	}
	removed := 0
	for _, m := range members {
		k := string(m)
		if _, ok := e.Set[k]; ok {
			delete(e.Set, k)
			removed++
		}
	}
	if removed > 0 {
		if len(e.Set) == 0 {
			sh.deleteEntryLocked(ck)
		}
		sh.bumpVersionLocked(ck)
	}
	return removed, nil
}

func (s *Store) SMembers(workspace, key string) ([][]byte, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return [][]byte{}, nil
	}
	if e.Kind != KindSet {
		return nil, storage.ErrWrongType
	}
	out := make([][]byte, 0, len(e.Set))
	for m := range e.Set {
		out = append(out, []byte(m))
	}
	return out, nil
}

func (s *Store) SIsMember(workspace, key string, member []byte) (bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return false, nil
	}
	if e.Kind != KindSet {
		return false, storage.ErrWrongType
	}
	_, ok := e.Set[string(member)]
	return ok, nil
}

func (s *Store) SCard(workspace, key string) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, nil
	}
	if e.Kind != KindSet {
		return 0, storage.ErrWrongType
	}
	return len(e.Set), nil
}

func (s *Store) setSnapshot(workspace, key string) (map[string]struct{}, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return nil, nil
	}
	if e.Kind != KindSet {
		return nil, storage.ErrWrongType
	}
	out := make(map[string]struct{}, len(e.Set))
	for k := range e.Set {
		out[k] = struct{}{}
	}
	return out, nil
}

func setToBytes(m map[string]struct{}) [][]byte {
	out := make([][]byte, 0, len(m))
	for k := range m {
		out = append(out, []byte(k))
	}
	return out
}

func (s *Store) SInter(workspace string, keys ...string) ([][]byte, error) {
	if len(keys) == 0 {
		return [][]byte{}, nil
	}
	result, err := s.setSnapshot(workspace, keys[0])
	if err != nil {
		return nil, err
	}
	for _, k := range keys[1:] {
		snap, err := s.setSnapshot(workspace, k)
		if err != nil {
			return nil, err
		}
		next := make(map[string]struct{})
		for m := range result {
			if _, ok := snap[m]; ok {
				next[m] = struct{}{}
			}
		}
		result = next
	}
	return setToBytes(result), nil
}

func (s *Store) SUnion(workspace string, keys ...string) ([][]byte, error) {
	result := make(map[string]struct{})
	for _, k := range keys {
		snap, err := s.setSnapshot(workspace, k)
		if err != nil {
			return nil, err
		}
		for m := range snap {
			result[m] = struct{}{}
		}
	}
	return setToBytes(result), nil
}

func (s *Store) SDiff(workspace string, keys ...string) ([][]byte, error) {
	if len(keys) == 0 {
		return [][]byte{}, nil
	}
	first, err := s.setSnapshot(workspace, keys[0])
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(first))
	for m := range first {
		result[m] = struct{}{}
	}
	for _, k := range keys[1:] {
		snap, err := s.setSnapshot(workspace, k)
		if err != nil {
			return nil, err
		}
		for m := range snap {
			delete(result, m)
		}
	}
	return setToBytes(result), nil
}

// --- sorted set -------------------------------------------------------

func sortedMembers(e *Entry) []storage.ZMember {
	out := make([]storage.ZMember, 0, len(e.ZSet))
	for m, sc := range e.ZSet {
		out = append(out, storage.ZMember{Member: m, Score: sc})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score < out[j].Score
		}
		return out[i].Member < out[j].Member
	})
	return out
}

func (s *Store) ZAdd(workspace, key string, members map[string]float64) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, err := ensureKind(s, sh, ck, KindZSet, now)
	if err != nil {
		return 0, err
	}
	added := 0
	for m, score := range members {
		if _, ok := e.ZSet[m]; !ok {
			added++
		}
		e.ZSet[m] = score
	}
	e.LastAccess = now
	e.AccessCount++
	sh.bumpVersionLocked(ck)
	return added, nil
}

func (s *Store) ZRem(workspace, key string, members ...string) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, nil
	}
	if e.Kind != KindZSet {
		return 0, storage.ErrWrongType
	}
	removed := 0
	for _, m := range members {
		if _, ok := e.ZSet[m]; ok {
			delete(e.ZSet, m)
			removed++
		}
	}
	if removed > 0 {
		if len(e.ZSet) == 0 {
			sh.deleteEntryLocked(ck)
		}
		sh.bumpVersionLocked(ck)
	}
	return removed, nil
}

func (s *Store) ZRange(workspace, key string, start, stop int, withScores bool) ([]storage.ZMember, error) {
	_ = withScores
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return []storage.ZMember{}, nil
	}
	if e.Kind != KindZSet {
		return nil, storage.ErrWrongType
	}
	all := sortedMembers(e)
	n := len(all)
	start = normalizeIndex(start, n)
	stop = normalizeIndex(stop, n)
	if start < 0 {
		start = 0
	}
	if stop >= n {
		stop = n - 1
	}
	if start > stop || n == 0 {
		return []storage.ZMember{}, nil
	}
	return append([]storage.ZMember(nil), all[start:stop+1]...), nil
}

func (s *Store) ZRevRange(workspace, key string, start, stop int, withScores bool) ([]storage.ZMember, error) {
	_ = withScores
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return []storage.ZMember{}, nil
	}
	if e.Kind != KindZSet {
		return nil, storage.ErrWrongType
	}
	all := sortedMembers(e)
	rev := make([]storage.ZMember, len(all))
	for i, m := range all {
		rev[len(all)-1-i] = m
	}
	n := len(rev)
	start = normalizeIndex(start, n)
	stop = normalizeIndex(stop, n)
	if start < 0 {
		start = 0
	}
	if stop >= n {
		stop = n - 1
	}
	if start > stop || n == 0 {
		return []storage.ZMember{}, nil
	}
	return append([]storage.ZMember(nil), rev[start:stop+1]...), nil
}

func (s *Store) ZScore(workspace, key, member string) (float64, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, false, nil
	}
	if e.Kind != KindZSet {
		return 0, false, storage.ErrWrongType
	}
	sc, ok := e.ZSet[member]
	return sc, ok, nil
}

func (s *Store) ZRank(workspace, key, member string) (int, bool, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, false, nil
	}
	if e.Kind != KindZSet {
		return 0, false, storage.ErrWrongType
	}
	if _, ok := e.ZSet[member]; !ok {
		return 0, false, nil
	}
	all := sortedMembers(e)
	for i, m := range all {
		if m.Member == member {
			return i, true, nil
		}
	}
	return 0, false, nil
}

func (s *Store) ZCard(workspace, key string) (int, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return 0, nil
	}
	if e.Kind != KindZSet {
		return 0, storage.ErrWrongType
	}
	return len(e.ZSet), nil
}

func (s *Store) ZIncrBy(workspace, key, member string, delta float64) (float64, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e, err := ensureKind(s, sh, ck, KindZSet, now)
	if err != nil {
		return 0, err
	}
	e.ZSet[member] += delta
	sh.bumpVersionLocked(ck)
	return e.ZSet[member], nil
}

func (s *Store) ZRangeByScore(workspace, key string, min, max float64) ([]storage.ZMember, error) {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	now := s.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := sh.getForWriteLocked(ck, now)
	if e == nil {
		return []storage.ZMember{}, nil
	}
	if e.Kind != KindZSet {
		return nil, storage.ErrWrongType
	}
	all := sortedMembers(e)
	out := make([]storage.ZMember, 0)
	for _, m := range all {
		if m.Score >= min && m.Score <= max {
			out = append(out, m)
		}
	}
	return out, nil
}

// --- transactions -------------------------------------------------------

// WatchVersion returns the current mutation-version counter for a key (0 if
// the key has never been touched). Used by WATCH/EXEC optimistic locking.
func (s *Store) WatchVersion(workspace, key string) uint64 {
	ck := nsKey(workspace, key)
	sh := s.shardFor(ck)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	return sh.versionLocked(ck)
}

// Exec runs fn while holding the store's global transaction mutex; see the
// Store.globalMu doc comment for the tradeoff this represents.
func (s *Store) Exec(fn func() error) error {
	s.globalMu.Lock()
	defer s.globalMu.Unlock()
	return fn()
}
