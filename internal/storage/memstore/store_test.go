package memstore

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/eviction"
	"github.com/SumitKumar-17/cache-pot/internal/storage"
)

// testClock lets tests control "now" deterministically instead of relying
// on real time.Sleep for TTL-expiry assertions. It's safe for concurrent
// use since the -race-checked concurrency test below calls Now() from
// many goroutines via Store's internal s.now().
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock(start time.Time) *testClock {
	return &testClock{now: start}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestStore(t *testing.T, clock *testClock, numShards int) *Store {
	t.Helper()
	s := NewWithClock(numShards, clock.Now)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestBasicSetGet(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	ok, _, hadPrev, err := s.Set("default", "foo", []byte("bar"), storage.SetOpts{})
	if err != nil || !ok || hadPrev {
		t.Fatalf("Set failed: ok=%v hadPrev=%v err=%v", ok, hadPrev, err)
	}

	val, found, err := s.Get("default", "foo")
	if err != nil || !found || string(val) != "bar" {
		t.Fatalf("Get mismatch: val=%q found=%v err=%v", val, found, err)
	}

	// Different workspaces are isolated.
	_, found, err = s.Get("other", "foo")
	if err != nil || found {
		t.Fatalf("expected key not visible in a different workspace, found=%v err=%v", found, err)
	}
}

func TestSetOptsNXXX(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	ok, _, _, err := s.Set("default", "k", []byte("v1"), storage.SetOpts{OnlyIfXX: true})
	if err != nil || ok {
		t.Fatalf("SET XX on missing key should not set, ok=%v err=%v", ok, err)
	}

	ok, _, _, err = s.Set("default", "k", []byte("v1"), storage.SetOpts{OnlyIfNX: true})
	if err != nil || !ok {
		t.Fatalf("SET NX on missing key should set, ok=%v err=%v", ok, err)
	}

	ok, _, _, err = s.Set("default", "k", []byte("v2"), storage.SetOpts{OnlyIfNX: true})
	if err != nil || ok {
		t.Fatalf("SET NX on existing key should not set, ok=%v err=%v", ok, err)
	}

	ok, prev, hadPrev, err := s.Set("default", "k", []byte("v3"), storage.SetOpts{OnlyIfXX: true, GetOld: true})
	if err != nil || !ok || !hadPrev || string(prev) != "v1" {
		t.Fatalf("SET XX GET on existing key mismatch: ok=%v hadPrev=%v prev=%q err=%v", ok, hadPrev, prev, err)
	}
}

func TestShardingDistributesKeys(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	const n = 200
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%d", i)
		if _, _, _, err := s.Set("default", key, []byte("v"), storage.SetOpts{}); err != nil {
			t.Fatalf("Set(%s) failed: %v", key, err)
		}
	}

	total := 0
	shardsUsed := 0
	for _, sh := range s.shards {
		sh.mu.RLock()
		count := len(sh.data)
		sh.mu.RUnlock()
		total += count
		if count > 0 {
			shardsUsed++
		}
	}
	if total != n {
		t.Fatalf("expected %d keys total across shards, got %d", n, total)
	}
	if shardsUsed < 2 {
		t.Fatalf("expected keys spread across multiple shards, only %d shard(s) used", shardsUsed)
	}
}

func TestTTLExpiryWithInjectableClock(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	if _, _, _, err := s.Set("default", "k", []byte("v"), storage.SetOpts{TTL: 5 * time.Second}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, found, err := s.Get("default", "k")
	if err != nil || !found || string(val) != "v" {
		t.Fatalf("expected key present before expiry: found=%v err=%v", found, err)
	}

	ttl, hasTTL, exists := s.TTL("default", "k")
	if !exists || !hasTTL || ttl <= 0 {
		t.Fatalf("expected a positive TTL, got ttl=%v hasTTL=%v exists=%v", ttl, hasTTL, exists)
	}

	clock.Advance(6 * time.Second)

	_, found, err = s.Get("default", "k")
	if err != nil || found {
		t.Fatalf("expected key expired via passive expiry, found=%v err=%v", found, err)
	}

	_, _, exists = s.TTL("default", "k")
	if exists {
		t.Fatalf("expected TTL to report key gone after expiry")
	}
}

func TestActiveReaperSweepsExpiredKeys(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	if _, _, _, err := s.Set("default", "k", []byte("v"), storage.SetOpts{TTL: 10 * time.Millisecond}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	clock.Advance(20 * time.Millisecond)

	ck := nsKey("default", "k")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gone := true
		for _, sh := range s.shards {
			sh.mu.RLock()
			_, ok := sh.data[ck]
			sh.mu.RUnlock()
			if ok {
				gone = false
				break
			}
		}
		if gone {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected background TTL reaper to sweep the expired key within the deadline")
}

func TestWrongType(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	if _, err := s.HSet("default", "h", map[string][]byte{"f": []byte("v")}); err != nil {
		t.Fatalf("HSet failed: %v", err)
	}

	_, _, err := s.Get("default", "h")
	if !errors.Is(err, storage.ErrWrongType) {
		t.Fatalf("expected ErrWrongType from GET on a hash key, got %v", err)
	}

	if _, err := s.LPush("default", "h", []byte("x")); !errors.Is(err, storage.ErrWrongType) {
		t.Fatalf("expected ErrWrongType from LPUSH on a hash key, got %v", err)
	}
}

func TestWatchVersionBumpsOnMutation(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	v0 := s.WatchVersion("default", "k")
	if _, _, _, err := s.Set("default", "k", []byte("v"), storage.SetOpts{}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	v1 := s.WatchVersion("default", "k")
	if v1 == v0 {
		t.Fatalf("expected WatchVersion to change after Set")
	}

	if n := s.Del("default", "k"); n != 1 {
		t.Fatalf("expected Del to remove 1 key, got %d", n)
	}
	v2 := s.WatchVersion("default", "k")
	if v2 == v1 {
		t.Fatalf("expected WatchVersion to change after Del")
	}
}

func TestListHashSetZSetBasics(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	if n, err := s.RPush("default", "l", []byte("a"), []byte("b"), []byte("c")); err != nil || n != 3 {
		t.Fatalf("RPush: n=%d err=%v", n, err)
	}
	vals, err := s.LRange("default", "l", 0, -1)
	if err != nil || len(vals) != 3 || string(vals[0]) != "a" || string(vals[2]) != "c" {
		t.Fatalf("LRange mismatch: %v %v", vals, err)
	}

	if n, err := s.SAdd("default", "s", []byte("x"), []byte("y")); err != nil || n != 2 {
		t.Fatalf("SAdd: n=%d err=%v", n, err)
	}
	ok, err := s.SIsMember("default", "s", []byte("x"))
	if err != nil || !ok {
		t.Fatalf("SIsMember mismatch: %v %v", ok, err)
	}

	if n, err := s.ZAdd("default", "z", map[string]float64{"m1": 1, "m2": 2}); err != nil || n != 2 {
		t.Fatalf("ZAdd: n=%d err=%v", n, err)
	}
	members, err := s.ZRange("default", "z", 0, -1, false)
	if err != nil || len(members) != 2 || members[0].Member != "m1" {
		t.Fatalf("ZRange mismatch: %v %v", members, err)
	}
}

// --- Eviction tests -----------------------------------------------------

// TestMaxEntriesZeroDisablesEviction confirms the "0 means off" convention:
// with no WithMaxEntries option (defaulting to 0), inserting far more keys
// than any reasonable cap never evicts anything.
func TestMaxEntriesZeroDisablesEviction(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4)

	const n = 100
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("k%d", i)
		if _, _, _, err := s.Set("default", key, []byte("v"), storage.SetOpts{}); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
	}
	if got := s.EntryCount(); got != n {
		t.Fatalf("expected all %d keys retained with maxEntries=0 (default), EntryCount()=%d", n, got)
	}
}

// TestMaxEntriesEvictsLRUVictim uses a single shard (so which shard
// receives the new key is never in question) and the default LRU policy:
// inserting past the cap evicts exactly the least-recently-used existing
// entry, and the running EntryCount() reflects it.
func TestMaxEntriesEvictsLRUVictim(t *testing.T) {
	clock := newTestClock(time.Now())
	s := NewWithClock(1, clock.Now, WithMaxEntries(3))
	t.Cleanup(func() { _ = s.Close() })

	for _, k := range []string{"a", "b", "c"} {
		if _, _, _, err := s.Set("default", k, []byte("v"), storage.SetOpts{}); err != nil {
			t.Fatalf("Set(%s): %v", k, err)
		}
		clock.Advance(time.Second)
	}
	// Touch b and c so "a" is unambiguously the least-recently-used entry.
	if _, _, err := s.Get("default", "b"); err != nil {
		t.Fatalf("Get(b): %v", err)
	}
	if _, _, err := s.Get("default", "c"); err != nil {
		t.Fatalf("Get(c): %v", err)
	}
	clock.Advance(time.Second)

	// A 4th distinct key pushes the count over the cap of 3.
	if _, _, _, err := s.Set("default", "d", []byte("v"), storage.SetOpts{}); err != nil {
		t.Fatalf("Set(d): %v", err)
	}

	if got := s.EntryCount(); got != 3 {
		t.Fatalf("expected EntryCount()==3 after eviction, got %d", got)
	}
	if _, found, _ := s.Get("default", "a"); found {
		t.Fatalf("expected 'a' (least recently used) to have been evicted")
	}
	for _, k := range []string{"b", "c", "d"} {
		if _, found, _ := s.Get("default", k); !found {
			t.Fatalf("expected %q to survive eviction, but it's gone", k)
		}
	}
}

// TestOnEvictCalledOncePerEviction inserts well past a small cap (using a
// single shard, again to keep the eviction math exact regardless of key
// hashing) and confirms the onEvict callback fires exactly once per entry
// actually evicted -- no more, no less.
func TestOnEvictCalledOncePerEviction(t *testing.T) {
	clock := newTestClock(time.Now())
	var mu sync.Mutex
	evictions := 0
	onEvict := func() {
		mu.Lock()
		evictions++
		mu.Unlock()
	}

	s := NewWithClock(1, clock.Now, WithMaxEntries(5), WithOnEvict(onEvict))
	t.Cleanup(func() { _ = s.Close() })

	const n = 20
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("k%d", i)
		if _, _, _, err := s.Set("default", key, []byte("v"), storage.SetOpts{}); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
		clock.Advance(time.Millisecond)
	}

	mu.Lock()
	got := evictions
	mu.Unlock()

	wantEvictions := n - 5
	if got != wantEvictions {
		t.Fatalf("expected %d onEvict calls, got %d", wantEvictions, got)
	}
	if entries := s.EntryCount(); entries != 5 {
		t.Fatalf("expected EntryCount()==5 once the cap is saturated, got %d", entries)
	}
}

// TestEntryCountExactAcrossDelFlushAndTTLExpiry confirms the running
// entry-count tracker (Store.EntryCount) stays exact across every code
// path that removes keys, not just the maxEntries-triggered eviction path:
// explicit DEL, TTL-reaper-driven expiry, and FLUSHDB.
func TestEntryCountExactAcrossDelFlushAndTTLExpiry(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 4) // maxEntries=0 (unbounded) -- isolate the counter from eviction.

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k%d", i)
		if _, _, _, err := s.Set("default", key, []byte("v"), storage.SetOpts{}); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
	}
	if got := s.EntryCount(); got != 10 {
		t.Fatalf("EntryCount()=%d after 10 inserts, want 10", got)
	}

	if n := s.Del("default", "k0", "k1"); n != 2 {
		t.Fatalf("Del removed %d keys, want 2", n)
	}
	if got := s.EntryCount(); got != 8 {
		t.Fatalf("EntryCount()=%d after Del, want 8", got)
	}

	// Overwriting an existing key (k2, k3) with a short TTL is an update,
	// not an insert -- EntryCount must not change.
	if _, _, _, err := s.Set("default", "k2", []byte("v"), storage.SetOpts{TTL: 10 * time.Millisecond}); err != nil {
		t.Fatalf("re-Set k2 with TTL: %v", err)
	}
	if _, _, _, err := s.Set("default", "k3", []byte("v"), storage.SetOpts{TTL: 10 * time.Millisecond}); err != nil {
		t.Fatalf("re-Set k3 with TTL: %v", err)
	}
	if got := s.EntryCount(); got != 8 {
		t.Fatalf("EntryCount()=%d after re-Set with TTL (an update, not an insert), want 8", got)
	}

	clock.Advance(20 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && s.EntryCount() != 6 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := s.EntryCount(); got != 6 {
		t.Fatalf("expected EntryCount()==6 once the TTL reaper swept k2/k3, got %d", got)
	}

	s.FlushDB("default")
	if got := s.EntryCount(); got != 0 {
		t.Fatalf("EntryCount()=%d after FlushDB, want 0", got)
	}
}

// TestWeightedPolicyEvictionDiffersFromLRU is the actual point of the
// Weighted policy: prove it makes a different eviction choice than LRU on a
// constructed access-count-vs-recency conflict, not just unit-test
// Weighted.Score in isolation.
//
// "hot" is read 50 times right after creation (driving its AccessCount up)
// and then left untouched while the clock advances 100s, so by the time a
// 3rd key is inserted it's the least-recently-used entry. "cold" is
// created right at that later instant -- very fresh LastAccess, but only
// ever touched once. Under pure LRU, staleness alone decides: "hot" (the
// frequently-accessed one) is evicted. Under a Weighted policy configured
// to weigh frequency more heavily, "hot" survives because its high
// AccessCount outweighs its staleness, and "cold" is evicted instead.
func TestWeightedPolicyEvictionDiffersFromLRU(t *testing.T) {
	setup := func(t *testing.T, policy eviction.Policy) *Store {
		t.Helper()
		clock := newTestClock(time.Now())
		s := NewWithClock(1, clock.Now, WithMaxEntries(2), WithEvictionPolicy(policy))
		t.Cleanup(func() { _ = s.Close() })

		if _, _, _, err := s.Set("default", "hot", []byte("v"), storage.SetOpts{}); err != nil {
			t.Fatalf("Set(hot): %v", err)
		}
		for i := 0; i < 50; i++ {
			if _, _, err := s.Get("default", "hot"); err != nil {
				t.Fatalf("Get(hot) #%d: %v", i, err)
			}
		}

		clock.Advance(100 * time.Second)

		if _, _, _, err := s.Set("default", "cold", []byte("v"), storage.SetOpts{}); err != nil {
			t.Fatalf("Set(cold): %v", err)
		}

		if _, _, _, err := s.Set("default", "third", []byte("v"), storage.SetOpts{}); err != nil {
			t.Fatalf("Set(third): %v", err)
		}
		return s
	}

	t.Run("lru evicts the stale-but-frequently-accessed entry", func(t *testing.T) {
		s := setup(t, eviction.NewLRU())
		if _, found, _ := s.Get("default", "hot"); found {
			t.Fatalf("expected pure LRU to evict the stale 'hot' entry")
		}
		if _, found, _ := s.Get("default", "cold"); !found {
			t.Fatalf("expected 'cold' to survive under pure LRU")
		}
	})

	t.Run("weighted keeps the frequently-accessed entry despite staleness", func(t *testing.T) {
		policy := eviction.NewWeighted(map[string]float64{"recency": 0.3, "frequency": 0.7})
		s := setup(t, policy)
		if _, found, _ := s.Get("default", "hot"); !found {
			t.Fatalf("expected the weighted policy to keep the frequently-accessed 'hot' entry")
		}
		if _, found, _ := s.Get("default", "cold"); found {
			t.Fatalf("expected the weighted policy to evict 'cold' instead")
		}
	})
}

// TestConcurrentAccess exercises the store from many goroutines to catch
// data races (run with `go test -race`).
func TestConcurrentAccess(t *testing.T) {
	clock := newTestClock(time.Now())
	s := newTestStore(t, clock, 8)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", n%10)
			for j := 0; j < 50; j++ {
				_, _, _, _ = s.Set("default", key, []byte("v"), storage.SetOpts{})
				_, _, _ = s.Get("default", key)
				_, _ = s.HSet("default", key+"-h", map[string][]byte{"f": []byte("v")})
				_, _, _ = s.HGet("default", key+"-h", "f")
				s.Del("default", key)
			}
		}(i)
	}
	wg.Wait()
}
