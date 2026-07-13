package observability

import (
	"context"

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
func InstrumentProvider(inner embed.Provider, m *Metrics) embed.Provider {
	return &instrumentedProvider{inner: inner, metrics: m}
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
