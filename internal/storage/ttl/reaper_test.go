package ttl

import (
	"context"
	"maps"
	"sync"
	"testing"
	"time"
)

// fakeSweepable is a Sweepable test double that just records how many times
// SweepShard was called per shard index (and with what sampleSize), without
// needing a real memstore.
type fakeSweepable struct {
	mu             sync.Mutex
	shards         int
	calls          map[int]int
	lastSampleSize int
}

func newFakeSweepable(shards int) *fakeSweepable {
	return &fakeSweepable{shards: shards, calls: make(map[int]int)}
}

func (f *fakeSweepable) NumShards() int { return f.shards }

func (f *fakeSweepable) SweepShard(shardIndex int, sampleSize int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[shardIndex]++
	f.lastSampleSize = sampleSize
	return 0
}

func (f *fakeSweepable) callCounts() map[int]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[int]int, len(f.calls))
	maps.Copy(out, f.calls)
	return out
}

func (f *fakeSweepable) getLastSampleSize() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSampleSize
}

func TestRunSweepsEveryShardEachTick(t *testing.T) {
	fake := newFakeSweepable(4)
	r := New(fake, 10*time.Millisecond, 5)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	r.Run(ctx)

	counts := fake.callCounts()
	if len(counts) != 4 {
		t.Fatalf("expected all 4 shards to have been swept at least once, got calls=%v", counts)
	}
	for i := range 4 {
		if counts[i] < 1 {
			t.Fatalf("shard %d was never swept: calls=%v", i, counts)
		}
	}
	// At a 10ms interval, a 120ms run should have ticked several times.
	if counts[0] < 3 {
		t.Fatalf("shard 0 was only swept %d times in 120ms at a 10ms interval, want several", counts[0])
	}
	if fake.getLastSampleSize() != 5 {
		t.Fatalf("SweepShard sampleSize = %d, want 5 (as passed to New)", fake.getLastSampleSize())
	}
}

func TestRunStopsCleanlyWhenContextCanceled(t *testing.T) {
	fake := newFakeSweepable(2)
	r := New(fake, 5*time.Millisecond, 5)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	// Let it tick a few times, then cancel and make sure Run returns
	// promptly rather than continuing to tick or blocking forever.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Reaper.Run did not return within 500ms of its context being canceled")
	}

	countsAtStop := fake.callCounts()
	time.Sleep(50 * time.Millisecond)
	countsAfterWait := fake.callCounts()
	for i, n := range countsAtStop {
		if countsAfterWait[i] != n {
			t.Fatalf("shard %d was swept again after Run returned (at-stop=%d, after-wait=%d): Run did not really stop", i, n, countsAfterWait[i])
		}
	}
}

func TestNewDefaultsIntervalAndSampleSize(t *testing.T) {
	fake := newFakeSweepable(1)
	// interval<=0 should default to 100ms; sampleSize<=0 should default to 20.
	r := New(fake, 0, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	r.Run(ctx)

	counts := fake.callCounts()
	if counts[0] < 1 {
		t.Fatalf("expected the default ~100ms interval to have ticked at least once in 250ms, got calls=%v", counts)
	}
	if counts[0] > 4 {
		t.Fatalf("shard 0 ticked %d times in 250ms, which suggests the interval did not default to ~100ms", counts[0])
	}
	if fake.getLastSampleSize() != 20 {
		t.Fatalf("SweepShard sampleSize = %d, want 20 (New's default for sampleSize<=0)", fake.getLastSampleSize())
	}
}

func TestNewNegativeValuesAlsoDefault(t *testing.T) {
	fake := newFakeSweepable(1)
	r := New(fake, -5*time.Millisecond, -1)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	r.Run(ctx)

	counts := fake.callCounts()
	if counts[0] < 1 {
		t.Fatalf("expected a negative interval to also default to ~100ms and tick at least once in 250ms, got calls=%v", counts)
	}
	if fake.getLastSampleSize() != 20 {
		t.Fatalf("SweepShard sampleSize = %d, want 20 (New's default for a negative sampleSize)", fake.getLastSampleSize())
	}
}
