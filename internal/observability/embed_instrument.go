package observability

import (
	"context"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/embed"
)

// instrumentedProvider wraps an embed.Provider so every Embed/EmbedBatch
// call is recorded on Metrics (total issued, errors, in-flight gauge)
// without internal/semantic or internal/memory needing their own
// instrumentation calls -- one decorator here covers every current and
// future caller of the wrapped Provider.
type instrumentedProvider struct {
	inner   embed.Provider
	metrics *Metrics
}

// InstrumentProvider wraps inner so its calls are recorded on m. Callers
// (internal/server/server.go) should wrap whatever embed.Provider they
// build (mock or OpenAI) exactly once, before handing it to
// internal/semantic.New / internal/memory.New, so both share the same
// instrumented instance and therefore the same metrics.
//
// If inner also implements embed.UsageEmbedder (real token-usage
// reporting -- currently only the OpenAI provider), the returned Provider
// forwards that capability too, feeding each call's reported token usage
// into tracker (see instrumentedUsageProvider below). If inner doesn't
// implement it (e.g. the deterministic mock provider), the plain wrapper
// is returned unchanged and tracker is never touched -- there's nothing
// real to record. tracker may be nil, in which case usage forwarding is
// skipped entirely (useful for tests that don't care about analytics).
func InstrumentProvider(inner embed.Provider, m *Metrics, tracker *analytics.Tracker) embed.Provider {
	base := &instrumentedProvider{inner: inner, metrics: m}
	if usageInner, ok := inner.(embed.UsageEmbedder); ok {
		return &instrumentedUsageProvider{
			instrumentedProvider: base,
			innerUsage:           usageInner,
			tracker:              tracker,
		}
	}
	return base
}

func (p *instrumentedProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	p.metrics.EmbeddingCallStarted()
	v, err := p.inner.Embed(ctx, text)
	p.metrics.EmbeddingCallFinished(err)
	return v, err
}

func (p *instrumentedProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	p.metrics.EmbeddingCallStarted()
	v, err := p.inner.EmbedBatch(ctx, texts)
	p.metrics.EmbeddingCallFinished(err)
	return v, err
}

func (p *instrumentedProvider) Dimensions() int { return p.inner.Dimensions() }
func (p *instrumentedProvider) Name() string    { return p.inner.Name() }

// instrumentedUsageProvider extends instrumentedProvider with the optional
// embed.UsageEmbedder capability. Go doesn't forward optional interfaces
// through a wrapper struct automatically -- embedding *instrumentedProvider
// here gives this type Embed/EmbedBatch/Dimensions/Name "for free" via
// promotion, while EmbedBatchWithUsage is implemented explicitly below.
type instrumentedUsageProvider struct {
	*instrumentedProvider
	innerUsage embed.UsageEmbedder
	tracker    *analytics.Tracker
}

// EmbedBatchWithUsage records the same in-flight/error metrics as
// EmbedBatch, plus feeds the returned embed.TokenUsage into tracker (when
// non-nil and the call succeeded with a positive token count).
func (p *instrumentedUsageProvider) EmbedBatchWithUsage(ctx context.Context, texts []string) ([][]float32, embed.TokenUsage, error) {
	p.metrics.EmbeddingCallStarted()
	vecs, usage, err := p.innerUsage.EmbedBatchWithUsage(ctx, texts)
	p.metrics.EmbeddingCallFinished(err)
	if err == nil && p.tracker != nil && usage.TotalTokens > 0 {
		p.tracker.RecordEmbeddingUsage(p.inner.Name(), usage.TotalTokens)
	}
	return vecs, usage, err
}
