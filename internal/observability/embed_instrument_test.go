package observability

import (
	"context"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/embed"
)

// fakeUsageProvider is a minimal embed.Provider that also implements
// embed.UsageEmbedder, for testing InstrumentProvider's optional-capability
// forwarding without a real OpenAI call.
type fakeUsageProvider struct {
	fakeProvider
	tokens int
}

func (f *fakeUsageProvider) EmbedBatchWithUsage(ctx context.Context, texts []string) ([][]float32, embed.TokenUsage, error) {
	if f.err != nil {
		return nil, embed.TokenUsage{}, f.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 2, 3}
	}
	return out, embed.TokenUsage{TotalTokens: f.tokens}, nil
}

func (f *fakeUsageProvider) Name() string { return "openai:text-embedding-3-small" }

// TestInstrumentProviderForwardsUsageToTracker verifies that when the
// wrapped provider implements embed.UsageEmbedder, InstrumentProvider
// returns a Provider that both forwards EmbedBatchWithUsage AND records
// the reported token usage into the given *analytics.Tracker.
func TestInstrumentProviderForwardsUsageToTracker(t *testing.T) {
	m := NewMetrics()
	tracker := analytics.New()
	p := InstrumentProvider(&fakeUsageProvider{tokens: 42}, m, tracker)

	usageP, ok := p.(embed.UsageEmbedder)
	if !ok {
		t.Fatal("InstrumentProvider did not forward embed.UsageEmbedder for a provider that implements it")
	}

	vecs, usage, err := usageP.EmbedBatchWithUsage(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("EmbedBatchWithUsage: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if usage.TotalTokens != 42 {
		t.Fatalf("usage.TotalTokens = %d, want 42", usage.TotalTokens)
	}

	snap := tracker.Snapshot()
	got, ok := snap.EmbeddingByModel["text-embedding-3-small"]
	if !ok {
		t.Fatalf("tracker did not record usage for text-embedding-3-small; snapshot: %+v", snap)
	}
	if got.Tokens != 42 {
		t.Fatalf("recorded tokens = %d, want 42", got.Tokens)
	}
	if !got.PricingKnown || got.CostUSD <= 0 {
		t.Fatalf("expected known pricing and a positive cost for text-embedding-3-small, got %+v", got)
	}

	// Metrics should still be recorded exactly like the plain wrapper.
	msnap := m.Snapshot()
	if msnap.EmbeddingCallsTotal != 1 {
		t.Fatalf("EmbeddingCallsTotal = %d, want 1", msnap.EmbeddingCallsTotal)
	}
}

// TestInstrumentProviderNoUsageCapabilityNoRecording verifies that when
// the wrapped provider does NOT implement embed.UsageEmbedder (the real
// mock provider's situation), InstrumentProvider returns a plain wrapper:
// no usage forwarding, no analytics recording, and critically no panic
// even though a non-nil tracker was supplied.
func TestInstrumentProviderNoUsageCapabilityNoRecording(t *testing.T) {
	m := NewMetrics()
	tracker := analytics.New()
	p := InstrumentProvider(&fakeProvider{}, m, tracker)

	if _, ok := p.(embed.UsageEmbedder); ok {
		t.Fatal("InstrumentProvider should not forward embed.UsageEmbedder for a provider that doesn't implement it")
	}

	if _, err := p.EmbedBatch(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	snap := tracker.Snapshot()
	if len(snap.EmbeddingByModel) != 0 {
		t.Fatalf("expected no analytics recording for a non-usage provider, got %+v", snap.EmbeddingByModel)
	}
}

// TestInstrumentProviderUsageForwardingWithNilTracker verifies a nil
// tracker doesn't panic even when the wrapped provider does report usage
// -- usage forwarding is simply skipped.
func TestInstrumentProviderUsageForwardingWithNilTracker(t *testing.T) {
	m := NewMetrics()
	p := InstrumentProvider(&fakeUsageProvider{tokens: 10}, m, nil)

	usageP, ok := p.(embed.UsageEmbedder)
	if !ok {
		t.Fatal("expected usage capability to be forwarded")
	}
	if _, _, err := usageP.EmbedBatchWithUsage(context.Background(), []string{"a"}); err != nil {
		t.Fatalf("EmbedBatchWithUsage with nil tracker: %v", err)
	}
}
