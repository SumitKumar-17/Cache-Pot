package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/llm"
)

// fakeCompletionProvider is a minimal llm.CompletionProvider double for
// testing InstrumentCompletionProvider without any real network/mock
// behavior.
type fakeCompletionProvider struct {
	err    error
	tokens int
	text   string
	name   string
}

func (f *fakeCompletionProvider) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, llm.TokenUsage, error) {
	if f.err != nil {
		return "", llm.TokenUsage{}, f.err
	}
	return f.text, llm.TokenUsage{TotalTokens: f.tokens}, nil
}

func (f *fakeCompletionProvider) Name() string {
	if f.name != "" {
		return f.name
	}
	return "fake"
}

func TestInstrumentCompletionProviderSuccess(t *testing.T) {
	m := NewMetrics()
	p := InstrumentCompletionProvider(&fakeCompletionProvider{text: "hello"}, m, nil)

	text, _, err := p.Complete(context.Background(), "sys", "hi")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "hello" {
		t.Fatalf("text = %q, want %q", text, "hello")
	}

	snap := m.Snapshot()
	if snap.CompletionCallsTotal != 1 {
		t.Fatalf("CompletionCallsTotal = %d, want 1", snap.CompletionCallsTotal)
	}
	if snap.CompletionCallsErrors != 0 {
		t.Fatalf("CompletionCallsErrors = %d, want 0", snap.CompletionCallsErrors)
	}
	if snap.CompletionCallsInFlight != 0 {
		t.Fatalf("CompletionCallsInFlight = %d, want 0 after completion", snap.CompletionCallsInFlight)
	}
	if got := p.Name(); got != "fake" {
		t.Fatalf("Name() = %q, want %q", got, "fake")
	}
}

func TestInstrumentCompletionProviderError(t *testing.T) {
	m := NewMetrics()
	p := InstrumentCompletionProvider(&fakeCompletionProvider{err: errors.New("boom")}, m, nil)

	if _, _, err := p.Complete(context.Background(), "sys", "hi"); err == nil {
		t.Fatal("expected an error")
	}

	snap := m.Snapshot()
	if snap.CompletionCallsTotal != 1 {
		t.Fatalf("CompletionCallsTotal = %d, want 1", snap.CompletionCallsTotal)
	}
	if snap.CompletionCallsErrors != 1 {
		t.Fatalf("CompletionCallsErrors = %d, want 1", snap.CompletionCallsErrors)
	}
	if snap.CompletionCallsInFlight != 0 {
		t.Fatalf("CompletionCallsInFlight = %d, want 0 even after an error", snap.CompletionCallsInFlight)
	}
}

// TestInstrumentCompletionProviderForwardsUsageToTracker verifies that
// InstrumentCompletionProvider forwards a successful call's TokenUsage
// into the given *analytics.Tracker via RecordCompletionUsage.
func TestInstrumentCompletionProviderForwardsUsageToTracker(t *testing.T) {
	m := NewMetrics()
	tracker := analytics.New()
	p := InstrumentCompletionProvider(&fakeCompletionProvider{text: "hi", tokens: 42, name: "openai:gpt-4o-mini"}, m, tracker)

	_, usage, err := p.Complete(context.Background(), "sys", "hi")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if usage.TotalTokens != 42 {
		t.Fatalf("usage.TotalTokens = %d, want 42", usage.TotalTokens)
	}

	snap := tracker.Snapshot()
	got, ok := snap.CompletionByModel["gpt-4o-mini"]
	if !ok {
		t.Fatalf("tracker did not record usage for gpt-4o-mini; snapshot: %+v", snap)
	}
	if got.Tokens != 42 {
		t.Fatalf("recorded tokens = %d, want 42", got.Tokens)
	}
	if !got.PricingKnown || got.CostUSD <= 0 {
		t.Fatalf("expected known pricing and a positive cost for gpt-4o-mini, got %+v", got)
	}
}

// TestInstrumentCompletionProviderZeroUsageNoRecording verifies a
// provider reporting zero TokenUsage (e.g. llm.NewMock's mock provider)
// never triggers a tracker recording, even with a non-nil tracker
// supplied.
func TestInstrumentCompletionProviderZeroUsageNoRecording(t *testing.T) {
	m := NewMetrics()
	tracker := analytics.New()
	p := InstrumentCompletionProvider(&fakeCompletionProvider{text: "hi", tokens: 0}, m, tracker)

	if _, _, err := p.Complete(context.Background(), "sys", "hi"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	snap := tracker.Snapshot()
	if len(snap.CompletionByModel) != 0 {
		t.Fatalf("expected no analytics recording for zero token usage, got %+v", snap.CompletionByModel)
	}
}

// TestInstrumentCompletionProviderUsageForwardingWithNilTracker verifies a
// nil tracker doesn't panic even when the wrapped provider does report
// usage -- usage forwarding is simply skipped.
func TestInstrumentCompletionProviderUsageForwardingWithNilTracker(t *testing.T) {
	m := NewMetrics()
	p := InstrumentCompletionProvider(&fakeCompletionProvider{text: "hi", tokens: 10}, m, nil)

	if _, _, err := p.Complete(context.Background(), "sys", "hi"); err != nil {
		t.Fatalf("Complete with nil tracker: %v", err)
	}
}

// TestInstrumentCompletionProviderErrorDoesNotRecordUsage verifies a
// failed call never forwards usage into the tracker, even if the fake
// provider were to report nonzero tokens alongside an error (which a real
// provider shouldn't, but the wrapper should still be defensive about it).
func TestInstrumentCompletionProviderErrorDoesNotRecordUsage(t *testing.T) {
	m := NewMetrics()
	tracker := analytics.New()
	p := InstrumentCompletionProvider(&fakeCompletionProvider{err: errors.New("boom")}, m, tracker)

	if _, _, err := p.Complete(context.Background(), "sys", "hi"); err == nil {
		t.Fatal("expected an error")
	}

	snap := tracker.Snapshot()
	if len(snap.CompletionByModel) != 0 {
		t.Fatalf("expected no analytics recording on error, got %+v", snap.CompletionByModel)
	}
}
