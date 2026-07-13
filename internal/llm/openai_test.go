package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestOpenAIProvider builds an openAIProvider pointed at a test server
// instead of the real OpenAI API, bypassing NewOpenAI so tests can inject
// a fake baseURL/client without any network access or real API key.
func newTestOpenAIProvider(baseURL string, client *http.Client) *openAIProvider {
	return &openAIProvider{
		apiKey:  "test-api-key",
		model:   "gpt-4o-mini",
		client:  client,
		baseURL: baseURL,
	}
}

func TestOpenAICompleteSuccess(t *testing.T) {
	var gotAuth, gotContentType string
	var gotReq openAIChatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("server: decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "hello there"}},
			},
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 42},
		})
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	text, usage, err := p.Complete(context.Background(), "you are helpful", "say hi")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "hello there" {
		t.Fatalf("text = %q, want %q", text, "hello there")
	}
	if usage.TotalTokens != 42 {
		t.Fatalf("usage.TotalTokens = %d, want 42", usage.TotalTokens)
	}

	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-api-key")
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type header = %q, want application/json", gotContentType)
	}
	if gotReq.Model != "gpt-4o-mini" {
		t.Fatalf("request model = %q, want gpt-4o-mini", gotReq.Model)
	}
	if len(gotReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(gotReq.Messages))
	}
	if gotReq.Messages[0].Role != "system" || gotReq.Messages[0].Content != "you are helpful" {
		t.Fatalf("system message = %+v, want role=system content=%q", gotReq.Messages[0], "you are helpful")
	}
	if gotReq.Messages[1].Role != "user" || gotReq.Messages[1].Content != "say hi" {
		t.Fatalf("user message = %+v, want role=user content=%q", gotReq.Messages[1], "say hi")
	}
}

func TestOpenAICompleteNon200Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	_, _, err := p.Complete(context.Background(), "sys", "hello")
	if err == nil {
		t.Fatal("expected an error for a non-200 response, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error should mention status code 401, got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("error should include response body snippet, got: %v", err)
	}
}

func TestOpenAICompleteContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be reached: the context is canceled before the
		// request is even sent.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := p.Complete(ctx, "sys", "hello")
	if err == nil {
		t.Fatal("expected an error for a canceled context, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected error to mention context cancellation, got: %v", err)
	}
}

func TestOpenAICompleteNoChoicesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[],"usage":{"total_tokens":5}}`))
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	_, _, err := p.Complete(context.Background(), "sys", "hello")
	if err == nil {
		t.Fatal("expected an error for a response with no choices, got nil")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected error to mention missing choices, got: %v", err)
	}
}

func TestOpenAINewProviderDefaults(t *testing.T) {
	p := NewOpenAI("key", "", "")
	if p.Name() != "openai:gpt-4o-mini" {
		t.Fatalf("Name() = %q, want default model name embedded", p.Name())
	}
}

func TestOpenAINewProviderCustomModel(t *testing.T) {
	p := NewOpenAI("key", "gpt-4o", "")
	if p.Name() != "openai:gpt-4o" {
		t.Fatalf("Name() = %q, want openai:gpt-4o", p.Name())
	}
}

func TestOpenAINewProviderBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"empty defaults to OpenAI", "", "https://api.openai.com/v1/chat/completions"},
		{"no trailing slash", "https://api.example.com/v1", "https://api.example.com/v1/chat/completions"},
		{"trailing slash trimmed", "https://api.example.com/v1/", "https://api.example.com/v1/chat/completions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewOpenAI("key", "", tc.baseURL).(*openAIProvider)
			if p.baseURL != tc.want {
				t.Fatalf("baseURL = %q, want %q", p.baseURL, tc.want)
			}
		})
	}
}
