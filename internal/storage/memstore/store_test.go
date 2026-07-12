package memstore

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

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
