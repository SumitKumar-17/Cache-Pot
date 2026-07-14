package llm

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
// gpt-4o-mini is a real, current (as of when this code was written),
// reasonably-priced OpenAI chat-completion model -- a sane default for a
// provider that may be invoked frequently (e.g. per-conversation
// summarization or entity extraction).
const defaultOpenAIModel = "gpt-4o-mini"

// maxOpenAIErrorBodySnippet caps how much of a non-200 response body gets
// embedded in the returned error, so a huge/unexpected response body
// doesn't blow up error messages or logs.
const maxOpenAIErrorBodySnippet = 500

// openAIProvider generates completions using OpenAI's
// /v1/chat/completions HTTP API. It uses only the Go standard library
// (net/http, encoding/json) -- no OpenAI SDK or third-party HTTP client
// dependency, matching internal/embed's openAIProvider.
type openAIProvider struct {
	apiKey  string
	model   string
	client  *http.Client
	baseURL string
}

// NewOpenAI constructs a CompletionProvider backed by an
// OpenAI-compatible chat-completions API. apiKey is sent as a Bearer token
// on every request. If model is empty, it defaults to "gpt-4o-mini".
//
// baseURL is the API base (e.g. "https://api.openai.com/v1"), without the
// "/chat/completions" suffix -- an empty baseURL defaults to OpenAI's own
// API. Overriding it points Cache-Pot at any OpenAI-compatible endpoint
// (an Azure OpenAI deployment, a self-hosted gateway, etc.) instead. This
// follows the exact same default/override convention as embed.NewOpenAI.
func NewOpenAI(apiKey, model, baseURL string) CompletionProvider {
	if model == "" {
		model = defaultOpenAIModel
	}
	if baseURL == "" {
		baseURL = defaultOpenAIAPIBase
	}
	return &openAIProvider{
		apiKey:  apiKey,
		model:   model,
		client:  http.DefaultClient,
		baseURL: strings.TrimRight(baseURL, "/") + "/chat/completions",
	}
}

func (p *openAIProvider) Name() string { return "openai:" + p.model }

// openAIChatMessage is one entry in a chat-completions request's
// "messages" array.
type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIChatRequest is the request body for POST /v1/chat/completions.
type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []openAIChatMessage `json:"messages"`
}

// openAIChatResponse is the relevant subset of the response body for
// POST /v1/chat/completions.
type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends a single chat-completion request with systemPrompt and
// userPrompt as the "system" and "user" messages respectively, and
// returns the first choice's message content plus the response's reported
// token usage.
func (p *openAIProvider) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, TokenUsage, error) {
	reqBody, err := json.Marshal(openAIChatRequest{
		Model: p.model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("llm: marshal openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("llm: build openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("llm: openai request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("llm: read openai response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > maxOpenAIErrorBodySnippet {
			snippet = snippet[:maxOpenAIErrorBodySnippet] + "...(truncated)"
		}
		return "", TokenUsage{}, fmt.Errorf("llm: openai returned status %d: %s", resp.StatusCode, snippet)
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", TokenUsage{}, fmt.Errorf("llm: decode openai response: %w", err)
	}
	if parsed.Error != nil {
		return "", TokenUsage{}, fmt.Errorf("llm: openai returned an error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", TokenUsage{}, fmt.Errorf("llm: openai response contained no choices")
	}

	return parsed.Choices[0].Message.Content, TokenUsage{TotalTokens: parsed.Usage.TotalTokens}, nil
}
