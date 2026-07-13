package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// defaultOpenAIAPIBase is OpenAI's own API base URL, used when NewOpenAI is
// called with an empty baseURL.
const defaultOpenAIAPIBase = "https://api.openai.com/v1"

// defaultOpenAIModel is used when NewOpenAI is called with an empty model.
const defaultOpenAIModel = "text-embedding-3-small"

// maxOpenAIErrorBodySnippet caps how much of a non-200 response body gets
// embedded in the returned error, so a huge/unexpected response body
// doesn't blow up error messages or logs.
const maxOpenAIErrorBodySnippet = 500

// openAIProvider embeds text using OpenAI's /v1/embeddings HTTP API. It
// uses only the Go standard library (net/http, encoding/json) — no OpenAI
// SDK or third-party HTTP client dependency.
type openAIProvider struct {
	apiKey  string
	model   string
	dims    int
	client  *http.Client
	baseURL string
}

// NewOpenAI constructs a Provider backed by an OpenAI-compatible embeddings
// API. apiKey is sent as a Bearer token on every request. If model is
// empty, it defaults to "text-embedding-3-small" (1536 dimensions). Other
// known OpenAI embedding models are recognized for the purpose of reporting
// Dimensions(); unrecognized models default to 1536 dimensions, which
// callers should override expectations for if using a nonstandard model.
//
// baseURL is the API base (e.g. "https://api.openai.com/v1"), without the
// "/embeddings" suffix — an empty baseURL defaults to OpenAI's own API.
// Overriding it points Cache-Pot at any OpenAI-compatible endpoint (an
// Azure OpenAI deployment, a self-hosted gateway, etc.) instead.
func NewOpenAI(apiKey, model, baseURL string) Provider {
	if model == "" {
		model = defaultOpenAIModel
	}
	if baseURL == "" {
		baseURL = defaultOpenAIAPIBase
	}
	return &openAIProvider{
		apiKey:  apiKey,
		model:   model,
		dims:    openAIModelDimensions(model),
		client:  http.DefaultClient,
		baseURL: strings.TrimRight(baseURL, "/") + "/embeddings",
	}
}

// openAIModelDimensions reports the known output dimensionality of
// OpenAI's published embedding models. Unknown models fall back to 1536
// (the text-embedding-3-small / ada-002 dimensionality) as a reasonable
// default.
func openAIModelDimensions(model string) int {
	switch model {
	case "text-embedding-3-small":
		return 1536
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-ada-002":
		return 1536
	default:
		return 1536
	}
}

func (p *openAIProvider) Name() string    { return "openai:" + p.model }
func (p *openAIProvider) Dimensions() int { return p.dims }

// openAIEmbeddingRequest is the request body for POST /v1/embeddings.
type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// openAIEmbeddingResponse is the relevant subset of the response body for
// POST /v1/embeddings. Usage is real OpenAI response data (the
// "usage.total_tokens" field) that used to be unmarshaled and immediately
// discarded; EmbedBatchWithUsage now surfaces it to callers instead.
type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed embeds a single piece of text.
func (p *openAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	out, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// EmbedBatch embeds multiple texts in a single HTTP request, which is how
// OpenAI's API natively supports batching (the "input" field accepts an
// array). It delegates to EmbedBatchWithUsage and discards the token-usage
// figure, so there is exactly one HTTP call implementation.
func (p *openAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vecs, _, err := p.EmbedBatchWithUsage(ctx, texts)
	return vecs, err
}

// EmbedBatchWithUsage is like EmbedBatch but additionally reports the
// number of tokens OpenAI billed the request for (parsed from the
// response's "usage.total_tokens" field), implementing the optional
// embed.UsageEmbedder capability.
func (p *openAIProvider) EmbedBatchWithUsage(ctx context.Context, texts []string) ([][]float32, TokenUsage, error) {
	if len(texts) == 0 {
		return nil, TokenUsage{}, nil
	}

	reqBody, err := json.Marshal(openAIEmbeddingRequest{Model: p.model, Input: texts})
	if err != nil {
		return nil, TokenUsage{}, fmt.Errorf("embed: marshal openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, TokenUsage{}, fmt.Errorf("embed: build openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, TokenUsage{}, fmt.Errorf("embed: openai request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, TokenUsage{}, fmt.Errorf("embed: read openai response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > maxOpenAIErrorBodySnippet {
			snippet = snippet[:maxOpenAIErrorBodySnippet] + "...(truncated)"
		}
		return nil, TokenUsage{}, fmt.Errorf("embed: openai returned status %d: %s", resp.StatusCode, snippet)
	}

	var parsed openAIEmbeddingResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, TokenUsage{}, fmt.Errorf("embed: decode openai response: %w", err)
	}
	if parsed.Error != nil {
		return nil, TokenUsage{}, fmt.Errorf("embed: openai returned an error: %s", parsed.Error.Message)
	}
	if len(parsed.Data) != len(texts) {
		return nil, TokenUsage{}, fmt.Errorf("embed: expected %d embeddings from openai, got %d", len(texts), len(parsed.Data))
	}

	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, TokenUsage{}, fmt.Errorf("embed: openai response index %d out of range [0,%d)", d.Index, len(out))
		}
		out[d.Index] = d.Embedding
	}
	return out, TokenUsage{TotalTokens: parsed.Usage.TotalTokens}, nil
}
