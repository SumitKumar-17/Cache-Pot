package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// openAIEmbeddingsURL is OpenAI's embeddings endpoint.
const openAIEmbeddingsURL = "https://api.openai.com/v1/embeddings"

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

// NewOpenAI constructs a Provider backed by OpenAI's embeddings API. apiKey
// is sent as a Bearer token on every request. If model is empty, it
// defaults to "text-embedding-3-small" (1536 dimensions). Other known
// OpenAI embedding models are recognized for the purpose of reporting
// Dimensions(); unrecognized models default to 1536 dimensions, which
// callers should override expectations for if using a nonstandard model.
func NewOpenAI(apiKey, model string) Provider {
	if model == "" {
		model = defaultOpenAIModel
	}
	return &openAIProvider{
		apiKey:  apiKey,
		model:   model,
		dims:    openAIModelDimensions(model),
		client:  http.DefaultClient,
		baseURL: openAIEmbeddingsURL,
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
// POST /v1/embeddings.
type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
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
// array).
func (p *openAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody, err := json.Marshal(openAIEmbeddingRequest{Model: p.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("embed: build openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: openai request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embed: read openai response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > maxOpenAIErrorBodySnippet {
			snippet = snippet[:maxOpenAIErrorBodySnippet] + "...(truncated)"
		}
		return nil, fmt.Errorf("embed: openai returned status %d: %s", resp.StatusCode, snippet)
	}

	var parsed openAIEmbeddingResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("embed: decode openai response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("embed: openai returned an error: %s", parsed.Error.Message)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embed: expected %d embeddings from openai, got %d", len(texts), len(parsed.Data))
	}

	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("embed: openai response index %d out of range [0,%d)", d.Index, len(out))
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
