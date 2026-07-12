package memory

import (
	"context"
	"testing"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/embed"
)

func newTestStore() *Store {
	return New(embed.NewMock(8))
}

func TestPutGeneratesIDWhenOmitted(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	id, err := s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "the sky is blue",
	}, 0)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if id == "" {
		t.Fatal("expected a generated non-empty id")
	}

	got, found, err := s.Get(ctx, "default", id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected the generated id to be retrievable")
	}
	if got.Version != 1 {
		t.Fatalf("Version = %d, want 1 for a fresh Put", got.Version)
	}
}

func TestPutReusesCallerIDAndBumpsVersion(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	id, err := s.Put(ctx, Memory{
		ID:          "my-id",
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "first content",
	}, 0)
	if err != nil {
		t.Fatalf("Put (first): %v", err)
	}
	if id != "my-id" {
		t.Fatalf("id = %q, want %q (the caller-given id)", id, "my-id")
	}

	first, found, err := s.Get(ctx, "default", "my-id")
	if err != nil || !found {
		t.Fatalf("Get (after first Put): found=%v err=%v", found, err)
	}
	if first.Version != 1 {
		t.Fatalf("Version after first Put = %d, want 1", first.Version)
	}

	id2, err := s.Put(ctx, Memory{
		ID:          "my-id",
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "second content",
	}, 0)
	if err != nil {
		t.Fatalf("Put (second): %v", err)
	}
	if id2 != "my-id" {
		t.Fatalf("id (second Put) = %q, want %q", id2, "my-id")
	}

	second, found, err := s.Get(ctx, "default", "my-id")
	if err != nil || !found {
		t.Fatalf("Get (after second Put): found=%v err=%v", found, err)
	}
	if second.Version != 2 {
		t.Fatalf("Version after second Put = %d, want 2 (bumped)", second.Version)
	}
	if second.Content != "second content" {
		t.Fatalf("Content after second Put = %q, want %q (replaced)", second.Content, "second content")
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("CreatedAt changed across a version bump: first=%v second=%v", first.CreatedAt, second.CreatedAt)
	}
}

func TestGetExactFields(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	metadata := map[string]string{"source": "conversation-42"}
	id, err := s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        Episodic,
		Content:     "the user prefers dark mode",
		Metadata:    metadata,
	}, 0)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, found, err := s.Get(ctx, "default", id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if got.ID != id {
		t.Errorf("ID = %q, want %q", got.ID, id)
	}
	if got.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "agent-1")
	}
	if got.Kind != Episodic {
		t.Errorf("Kind = %v, want %v", got.Kind, Episodic)
	}
	if got.Content != "the user prefers dark mode" {
		t.Errorf("Content = %q, want %q", got.Content, "the user prefers dark mode")
	}
	if got.Metadata["source"] != "conversation-42" {
		t.Errorf("Metadata[source] = %q, want %q", got.Metadata["source"], "conversation-42")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestGetUnknownIDNotFound(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	_, found, err := s.Get(ctx, "default", "nope")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("expected found=false for an unknown id")
	}
}

func TestSearchRanksSemanticallyCloseAboveUnrelated(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	closeID, err := s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "the capital of France is Paris",
	}, 0)
	if err != nil {
		t.Fatalf("Put (close): %v", err)
	}
	_, err = s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "penguins live in Antarctica",
	}, 0)
	if err != nil {
		t.Fatalf("Put (unrelated): %v", err)
	}

	results, err := s.Search(ctx, "default", "what is the capital of France?", SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].ID != closeID {
		t.Fatalf("best match = %q, want the semantically-close memory %q", results[0].ID, closeID)
	}
}

func TestSearchAgentFilterExcludesOtherAgents(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	idA, err := s.Put(ctx, Memory{
		AgentID:     "agent-a",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "shared topic: databases",
	}, 0)
	if err != nil {
		t.Fatalf("Put (agent-a): %v", err)
	}
	idB, err := s.Put(ctx, Memory{
		AgentID:     "agent-b",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "shared topic: databases",
	}, 0)
	if err != nil {
		t.Fatalf("Put (agent-b): %v", err)
	}

	// No AGENT filter: both agents' memories show up (shared memory, no silos).
	all, err := s.Search(ctx, "default", "databases", SearchOptions{})
	if err != nil {
		t.Fatalf("Search (no filter): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d results without AGENT filter, want 2", len(all))
	}

	// AGENT filter: only agent-a's memory.
	scoped, err := s.Search(ctx, "default", "databases", SearchOptions{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("Search (AGENT=agent-a): %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != idA {
		t.Fatalf("scoped results = %+v, want exactly [%q]", scoped, idA)
	}
	for _, r := range scoped {
		if r.ID == idB {
			t.Fatalf("agent-b's memory %q leaked into an AGENT=agent-a search", idB)
		}
	}
}

func TestSearchKindFilter(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	longID, err := s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "onboarding preference notes",
	}, 0)
	if err != nil {
		t.Fatalf("Put (long_term): %v", err)
	}
	_, err = s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        Episodic,
		Content:     "onboarding preference notes",
	}, 0)
	if err != nil {
		t.Fatalf("Put (episodic): %v", err)
	}

	kind := LongTerm
	results, err := s.Search(ctx, "default", "onboarding preference notes", SearchOptions{Kind: &kind})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].ID != longID {
		t.Fatalf("KIND-filtered results = %+v, want exactly [%q]", results, longID)
	}
}

func TestSearchKCapsResults(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := s.Put(ctx, Memory{
			AgentID:     "agent-1",
			WorkspaceID: "default",
			Kind:        LongTerm,
			Content:     "note about topic",
		}, 0); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	results, err := s.Search(ctx, "default", "note about topic", SearchOptions{K: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (K cap)", len(results))
	}
}

func TestGetTTLExpiryLazy(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	fakeNow := time.Now()
	s.now = func() time.Time { return fakeNow }

	id, err := s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "ephemeral note",
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Before expiry: still found.
	if _, found, err := s.Get(ctx, "default", id); err != nil || !found {
		t.Fatalf("expected hit before TTL expiry: found=%v err=%v", found, err)
	}

	// Advance the mock clock past expiry.
	s.now = func() time.Time { return fakeNow.Add(31 * time.Second) }

	if _, found, err := s.Get(ctx, "default", id); err != nil || found {
		t.Fatalf("expected miss after TTL expiry: found=%v err=%v", found, err)
	}

	// The entry should also be gone from Search's index now.
	results, err := s.Search(ctx, "default", "ephemeral note", SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.ID == id {
			t.Fatalf("expired memory %q still appeared in Search results", id)
		}
	}
}

func TestHistoryNotImplemented(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	id, err := s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "note",
	}, 0)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := s.History(ctx, "default", id); err != ErrHistoryNotImplemented {
		t.Fatalf("History error = %v, want ErrHistoryNotImplemented", err)
	}
}
