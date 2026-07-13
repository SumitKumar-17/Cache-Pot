package embed

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
		model:   "text-embedding-3-small",
		dims:    3,
		client:  client,
		baseURL: baseURL,
	}
}

func TestOpenAIEmbedSuccess(t *testing.T) {
	var gotAuth, gotContentType string
	var gotReq openAIEmbeddingRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("server: decode request body: %v", err)
		}

		// Respond with data out of input order, to verify the client maps
		// by Index rather than assuming ordering.
		resp := openAIEmbeddingResponse{}
		for i := len(gotReq.Input) - 1; i >= 0; i-- {
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float32{float32(i) + 0.1, float32(i) + 0.2, float32(i) + 0.3},
				Index:     i,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	vecs, err := p.EmbedBatch(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if vecs[0][0] != 0.1 || vecs[1][0] != 1.1 {
		t.Fatalf("vectors not mapped to correct index: %v", vecs)
	}

	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-api-key")
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type header = %q, want application/json", gotContentType)
	}
	if gotReq.Model != "text-embedding-3-small" {
		t.Fatalf("request model = %q, want text-embedding-3-small", gotReq.Model)
	}
	if len(gotReq.Input) != 2 || gotReq.Input[0] != "first" || gotReq.Input[1] != "second" {
		t.Fatalf("request input = %v, want [first second]", gotReq.Input)
	}

	// Single Embed should also work and hit the same batch path.
	single, err := p.Embed(context.Background(), "first")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(single) != 3 {
		t.Fatalf("Embed returned vector of length %d, want 3", len(single))
	}
}

func TestOpenAINon200Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	_, err := p.Embed(context.Background(), "hello")
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

func TestOpenAIContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be reached: the context is canceled before the
		// request is even sent.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Embed(ctx, "hello")
	if err == nil {
		t.Fatal("expected an error for a canceled context, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected error to mention context cancellation, got: %v", err)
	}
}

func TestOpenAINewProviderDefaults(t *testing.T) {
	p := NewOpenAI("key", "", "")
	if p.Name() != "openai:text-embedding-3-small" {
		t.Fatalf("Name() = %q, want default model name embedded", p.Name())
	}
	if p.Dimensions() != 1536 {
		t.Fatalf("Dimensions() = %d, want 1536 for default model", p.Dimensions())
	}
}

func TestOpenAINewProviderCustomModel(t *testing.T) {
	p := NewOpenAI("key", "text-embedding-3-large", "")
	if p.Dimensions() != 3072 {
		t.Fatalf("Dimensions() = %d, want 3072 for text-embedding-3-large", p.Dimensions())
	}
	if p.Name() != "openai:text-embedding-3-large" {
		t.Fatalf("Name() = %q, want openai:text-embedding-3-large", p.Name())
	}
}

func TestOpenAINewProviderBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"empty defaults to OpenAI", "", "https://api.openai.com/v1/embeddings"},
		{"no trailing slash", "https://api.example.com/v1", "https://api.example.com/v1/embeddings"},
		{"trailing slash trimmed", "https://api.example.com/v1/", "https://api.example.com/v1/embeddings"},
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

// TestOpenAIEmbedBatchWithUsageReturnsTokenCount verifies EmbedBatchWithUsage
// parses the response's "usage.total_tokens" field -- the field the old
// EmbedBatch implementation unmarshaled and immediately discarded.
func TestOpenAIEmbedBatchWithUsageReturnsTokenCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"embedding": [0.1, 0.2, 0.3], "index": 0},
				{"embedding": [0.4, 0.5, 0.6], "index": 1}
			],
			"usage": {"prompt_tokens": 7, "total_tokens": 7}
		}`))
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	vecs, usage, err := p.EmbedBatchWithUsage(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("EmbedBatchWithUsage: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if usage.TotalTokens != 7 {
		t.Fatalf("usage.TotalTokens = %d, want 7", usage.TotalTokens)
	}
}

// TestOpenAIEmbedBatchDiscardsUsageButStillWorks verifies the refactored
// EmbedBatch (now delegating to EmbedBatchWithUsage) still returns the
// right vectors when the response carries a usage field it doesn't
// surface.
func TestOpenAIEmbedBatchDiscardsUsageButStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [{"embedding": [1, 2, 3], "index": 0}],
			"usage": {"prompt_tokens": 3, "total_tokens": 3}
		}`))
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	vecs, err := p.EmbedBatch(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 1 || vecs[0][0] != 1 {
		t.Fatalf("EmbedBatch returned unexpected vectors: %v", vecs)
	}
}

// TestOpenAIEmbedBatchWithUsageMissingUsageField verifies a response with
// no usage field at all reports TotalTokens 0 rather than erroring --
// absence of usage data is honestly reported as zero, not guessed at.
func TestOpenAIEmbedBatchWithUsageMissingUsageField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"embedding": [1, 2, 3], "index": 0}]}`))
	}))
	defer srv.Close()

	p := newTestOpenAIProvider(srv.URL, srv.Client())

	_, usage, err := p.EmbedBatchWithUsage(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("EmbedBatchWithUsage: %v", err)
	}
	if usage.TotalTokens != 0 {
		t.Fatalf("usage.TotalTokens = %d, want 0 for a response with no usage field", usage.TotalTokens)
	}
}
