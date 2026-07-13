package observability

import (
	"context"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/llm"
)

// instrumentedCompletionProvider wraps an llm.CompletionProvider so every
// Complete call is recorded on Metrics (total issued, errors, in-flight
// gauge) and, when the provider reports real token usage, on an
// *analytics.Tracker -- mirroring instrumentedProvider's role for
// embed.Provider (see embed_instrument.go). Unlike the embedding case,
// there is no optional-capability split here: llm.CompletionProvider's
// Complete already returns a TokenUsage directly (there's no separate
// "with usage" method to type-assert for), so a single wrapper type
// suffices.
type instrumentedCompletionProvider struct {
	inner   llm.CompletionProvider
	metrics *Metrics
	tracker *analytics.Tracker
}

// InstrumentCompletionProvider wraps inner so its calls are recorded on m,
// and any real reported token usage is fed into tracker via
// tracker.RecordCompletionUsage. Callers (internal/server/server.go)
// should wrap whatever llm.CompletionProvider they build (mock or OpenAI)
// exactly once, before handing it to any consumer, so every caller shares
// the same instrumented instance and therefore the same metrics/cost
// tracking. tracker may be nil, in which case usage forwarding is skipped
// entirely (useful for tests that don't care about analytics).
func InstrumentCompletionProvider(inner llm.CompletionProvider, m *Metrics, tracker *analytics.Tracker) llm.CompletionProvider {
	return &instrumentedCompletionProvider{inner: inner, metrics: m, tracker: tracker}
}

func (p *instrumentedCompletionProvider) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, llm.TokenUsage, error) {
	p.metrics.CompletionCallStarted()
	text, usage, err := p.inner.Complete(ctx, systemPrompt, userPrompt)
	p.metrics.CompletionCallFinished(err)
	if err == nil && p.tracker != nil && usage.TotalTokens > 0 {
		p.tracker.RecordCompletionUsage(p.inner.Name(), usage.TotalTokens)
	}
	return text, usage, err
}

func (p *instrumentedCompletionProvider) Name() string { return p.inner.Name() }
