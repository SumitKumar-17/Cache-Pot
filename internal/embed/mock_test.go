package embed

import (
	"context"
	"testing"
)

func TestMockDeterministic(t *testing.T) {
	p := NewMock(16)
	ctx := context.Background()

	v1, err := p.Embed(ctx, "the quick brown fox")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	v2, err := p.Embed(ctx, "the quick brown fox")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(v1) != len(v2) {
		t.Fatalf("length mismatch: %d vs %d", len(v1), len(v2))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("same input produced different vectors at index %d: %v vs %v", i, v1[i], v2[i])
		}
	}
}

func TestMockDifferentInputsDiffer(t *testing.T) {
	p := NewMock(16)
	ctx := context.Background()

	v1, err := p.Embed(ctx, "the quick brown fox")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	v2, err := p.Embed(ctx, "a completely unrelated sentence about oceans")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("different inputs produced identical vectors: %v", v1)
	}

	// Different inputs should not just differ, they should be quite
	// dissimilar in cosine terms (mock uses disjoint bags of words here).
	sim := Cosine(v1, v2)
	if sim > 0.5 {
		t.Fatalf("expected dissimilar vectors for unrelated text, got cosine similarity %v", sim)
	}
}

func TestMockDimensions(t *testing.T) {
	for _, dims := range []int{1, 4, 16, 384, 1536} {
		p := NewMock(dims)
		if got := p.Dimensions(); got != dims {
			t.Fatalf("Dimensions() = %d, want %d", got, dims)
		}
		v, err := p.Embed(context.Background(), "some text")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if len(v) != dims {
			t.Fatalf("Embed produced vector of length %d, want %d", len(v), dims)
		}
	}
}

func TestMockDimensionsDefault(t *testing.T) {
	p := NewMock(0)
	if p.Dimensions() <= 0 {
		t.Fatalf("Dimensions() with dims<=0 requested should default to something positive, got %d", p.Dimensions())
	}
}

func TestMockEmbedBatch(t *testing.T) {
	p := NewMock(8)
	texts := []string{"alpha", "beta", "alpha"}
	vecs, err := p.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("EmbedBatch returned %d vectors, want %d", len(vecs), len(texts))
	}
	// texts[0] and texts[2] are identical ("alpha"), so their vectors must
	// be identical too.
	for i := range vecs[0] {
		if vecs[0][i] != vecs[2][i] {
			t.Fatalf("EmbedBatch: duplicate inputs produced different vectors at index %d", i)
		}
	}
	single, err := p.Embed(context.Background(), "beta")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	for i := range single {
		if single[i] != vecs[1][i] {
			t.Fatalf("EmbedBatch result for %q diverges from single Embed result at index %d", texts[1], i)
		}
	}
}

func TestMockNearDuplicatesAreCloseButNotIdentical(t *testing.T) {
	p := NewMock(32)
	ctx := context.Background()

	a, err := p.Embed(ctx, "Hello World")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	b, err := p.Embed(ctx, "  hello   world  ")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	sim := Cosine(a, b)
	if sim < 0.9 {
		t.Fatalf("expected near-duplicate strings to have high cosine similarity, got %v", sim)
	}

	identical := true
	for i := range a {
		if a[i] != b[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Fatalf("expected near-duplicate (not exact) strings to differ slightly, but vectors were bit-identical")
	}
}

func TestMockExactDuplicatesAreIdentical(t *testing.T) {
	p := NewMock(32)
	ctx := context.Background()

	a, err := p.Embed(ctx, "exact same string")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	b, err := p.Embed(ctx, "exact same string")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("expected byte-identical inputs to produce identical vectors, differed at index %d", i)
		}
	}
}

func TestMockName(t *testing.T) {
	p := NewMock(4)
	if p.Name() == "" {
		t.Fatal("Name() should not be empty")
	}
}

func TestMockRespectsCanceledContext(t *testing.T) {
	p := NewMock(4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Embed(ctx, "text"); err == nil {
		t.Fatal("expected error for canceled context")
	}
}
