// Package ttl implements active (background) key expiry for
// internal/storage/memstore. Passive expiry (checking ExpiresAt on read)
// handles correctness; the reaper here bounds how long an expired key that
// is never read again can linger in memory.
package ttl

import (
	"context"
	"time"
)

// Sweepable is implemented by a store that supports bounded active-expiry
// sampling. It is deliberately narrow so the reaper has no dependency on
// memstore's internals (or vice versa).
type Sweepable interface {
	// NumShards returns the number of independent shards to sweep.
	NumShards() int
	// SweepShard examines up to sampleSize keys-with-TTL in the given shard
	// index and deletes any that have expired. It returns the number of
	// keys removed.
	SweepShard(shardIndex int, sampleSize int) (expiredRemoved int)
}

// Reaper periodically samples a bounded number of keys-with-TTL per shard
// and deletes expired ones, so expired keys don't linger indefinitely just
// because nothing happens to read them.
type Reaper struct {
	store      Sweepable
	interval   time.Duration
	sampleSize int
}

// New builds a Reaper. interval is how often each shard is swept (e.g.
// 100ms); sampleSize bounds how many keys-with-TTL are examined per shard
// per tick, so a tick never becomes a full-table scan.
func New(store Sweepable, interval time.Duration, sampleSize int) *Reaper {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	if sampleSize <= 0 {
		sampleSize = 20
	}
	return &Reaper{store: store, interval: interval, sampleSize: sampleSize}
}

// Run sweeps every shard once per tick until ctx is canceled. It is meant to
// be started in its own goroutine: `go reaper.Run(ctx)`.
func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick()
		}
	}
}

func (r *Reaper) tick() {
	n := r.store.NumShards()
	for i := 0; i < n; i++ {
		r.store.SweepShard(i, r.sampleSize)
	}
}
