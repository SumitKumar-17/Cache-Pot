package llm

import (
	"context"
	"strings"
	"testing"
)

func TestMockCompleteDeterministic(t *testing.T) {
	m := NewMock()
	got1, usage1, err := m.Complete(context.Background(), "system", "what is kubernetes")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got2, usage2, err := m.Complete(context.Background(), "system", "what is kubernetes")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got1 != got2 {
		t.Fatalf("mock provider not deterministic: %q != %q", got1, got2)
	}
	if usage1 != (TokenUsage{}) || usage2 != (TokenUsage{}) {
		t.Fatalf("expected zero TokenUsage from mock, got %+v and %+v", usage1, usage2)
	}
}

func TestMockCompleteZeroTokenUsageAlways(t *testing.T) {
	m := NewMock()
	inputs := []string{"", "short", strings.Repeat("a very long prompt ", 100)}
	for _, in := range inputs {
		_, usage, err := m.Complete(context.Background(), "sys", in)
		if err != nil {
			t.Fatalf("Complete(%q): %v", in, err)
		}
		if usage.TotalTokens != 0 {
			t.Fatalf("Complete(%q) reported TotalTokens = %d, want 0 (mock makes no real call)", in, usage.TotalTokens)
		}
	}
}

func TestMockCompleteDifferentInputsDifferentOutput(t *testing.T) {
	m := NewMock()
	got1, _, err := m.Complete(context.Background(), "system", "what is kubernetes")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got2, _, err := m.Complete(context.Background(), "system", "what is docker")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got1 == got2 {
		t.Fatalf("different user prompts produced identical mock output: %q", got1)
	}
}

func TestMockCompleteMarkedAsNotReal(t *testing.T) {
	m := NewMock()
	got, _, err := m.Complete(context.Background(), "system", "hello")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(got, "mock") {
		t.Fatalf("mock completion output %q does not clearly mark itself as a mock", got)
	}
}

func TestMockCompleteTruncatesLongPrompt(t *testing.T) {
	m := NewMock()
	longPrompt := strings.Repeat("x", mockEchoLimit*2)
	got, _, err := m.Complete(context.Background(), "system", longPrompt)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(got) >= len(mockPrefix)+len(longPrompt) {
		t.Fatalf("expected mock output to truncate a long prompt, got length %d", len(got))
	}
}

func TestMockCompleteHonorsContextCancellation(t *testing.T) {
	m := NewMock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := m.Complete(ctx, "system", "hello")
	if err == nil {
		t.Fatal("expected an error for a canceled context, got nil")
	}
}

func TestMockName(t *testing.T) {
	m := NewMock()
	if m.Name() != "mock" {
		t.Fatalf("Name() = %q, want %q", m.Name(), "mock")
	}
}
