package semantic

import (
	"context"
	"testing"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/embed"
)

func newTestCache() *SemanticCache {
	return New(embed.NewMock(8))
}

func TestSemanticCacheExactDuplicateHit(t *testing.T) {
	c := newTestCache()
	ctx := context.Background()

	if err := c.Set(ctx, "What is the capital of France?", "gpt-4", "0.7", "Paris", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	resp, found, err := c.Get(ctx, "What is the capital of France?", "gpt-4", "0.7", 0.85)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected exact-duplicate prompt to be a hit")
	}
	if resp != "Paris" {
		t.Fatalf("response = %q, want %q", resp, "Paris")
	}
}

func TestSemanticCacheCaseWhitespaceVariantHit(t *testing.T) {
	// The mock provider is documented to put same-words-different-case-or-
	// whitespace prompts close together (see internal/embed/mock.go). Verify
	// that actually holds and clears the default 0.85 threshold.
	c := newTestCache()
	ctx := context.Background()

	if err := c.Set(ctx, "What is the capital of France?", "gpt-4", "0.7", "Paris", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	resp, found, err := c.Get(ctx, "  what IS the   capital of France?  ", "gpt-4", "0.7", 0.85)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected a same-words-different-case/whitespace prompt to be a hit")
	}
	if resp != "Paris" {
		t.Fatalf("response = %q, want %q", resp, "Paris")
	}
}

func TestSemanticCacheUnrelatedPromptMiss(t *testing.T) {
	c := newTestCache()
	ctx := context.Background()

	if err := c.Set(ctx, "What is the capital of France?", "gpt-4", "0.7", "Paris", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	_, found, err := c.Get(ctx, "Tell me a joke about penguins", "gpt-4", "0.7", 0.85)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("expected an unrelated prompt to be a miss")
	}
}

func TestSemanticCacheDifferentModelPartition(t *testing.T) {
	c := newTestCache()
	ctx := context.Background()

	if err := c.Set(ctx, "What is the capital of France?", "gpt-4", "0.7", "Paris", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	_, found, err := c.Get(ctx, "What is the capital of France?", "claude", "0.7", 0.85)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("expected identical prompt under a different model to be a miss")
	}
}

func TestSemanticCacheDifferentTempPartition(t *testing.T) {
	c := newTestCache()
	ctx := context.Background()

	if err := c.Set(ctx, "What is the capital of France?", "gpt-4", "0.7", "Paris", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	_, found, err := c.Get(ctx, "What is the capital of France?", "gpt-4", "0.9", 0.85)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("expected identical prompt under a different temperature to be a miss")
	}
}

func TestSemanticCacheTTLExpiry(t *testing.T) {
	c := newTestCache()
	ctx := context.Background()

	if err := c.Set(ctx, "What is the capital of France?", "gpt-4", "0.7", "Paris", 30*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Before expiry: still a hit.
	if _, found, err := c.Get(ctx, "What is the capital of France?", "gpt-4", "0.7", 0.85); err != nil || !found {
		t.Fatalf("expected hit before TTL expiry: found=%v err=%v", found, err)
	}

	time.Sleep(60 * time.Millisecond)

	if _, found, err := c.Get(ctx, "What is the capital of France?", "gpt-4", "0.7", 0.85); err != nil || found {
		t.Fatalf("expected miss after TTL expiry: found=%v err=%v", found, err)
	}
}
