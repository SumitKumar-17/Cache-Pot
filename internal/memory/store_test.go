package memory

import (
	"context"
	"strconv"
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

func TestListFiltersByAgentAndKind(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	mustPut := func(id, agentID string, kind Kind, content string) {
		if _, err := s.Put(ctx, Memory{
			ID:          id,
			AgentID:     agentID,
			WorkspaceID: "default",
			Kind:        kind,
			Content:     content,
		}, 0); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}

	mustPut("a-epi-1", "agent-a", Episodic, "agent-a episodic 1")
	mustPut("a-epi-2", "agent-a", Episodic, "agent-a episodic 2")
	mustPut("a-long-1", "agent-a", LongTerm, "agent-a long-term")
	mustPut("b-epi-1", "agent-b", Episodic, "agent-b episodic")

	// AgentID + Kind filter: only agent-a's episodic memories.
	kindEpisodic := Episodic
	got, err := s.List(ctx, "default", ListOptions{AgentID: "agent-a", Kind: &kindEpisodic})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	gotIDs := make(map[string]bool, len(got))
	for _, m := range got {
		gotIDs[m.ID] = true
		if m.Embedding == nil {
			t.Errorf("List result %q has no Embedding, want the stored embedding to be included", m.ID)
		}
	}
	want := map[string]bool{"a-epi-1": true, "a-epi-2": true}
	if len(gotIDs) != len(want) {
		t.Fatalf("List(agent-a, episodic) = %v, want %v", gotIDs, want)
	}
	for id := range want {
		if !gotIDs[id] {
			t.Errorf("List(agent-a, episodic) missing %q", id)
		}
	}
	if gotIDs["a-long-1"] || gotIDs["b-epi-1"] {
		t.Errorf("List(agent-a, episodic) leaked an excluded memory: %v", gotIDs)
	}

	// AgentID only (no Kind filter): every one of agent-a's memories.
	gotAgentOnly, err := s.List(ctx, "default", ListOptions{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(gotAgentOnly) != 3 {
		t.Fatalf("List(agent-a, any kind) = %d results, want 3", len(gotAgentOnly))
	}

	// Neither filter: every memory in the workspace.
	gotAll, err := s.List(ctx, "default", ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(gotAll) != 4 {
		t.Fatalf("List(no filters) = %d results, want 4", len(gotAll))
	}
}

func TestListNoMatchReturnsNilSlice(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	got, err := s.List(ctx, "default", ListOptions{AgentID: "nobody"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got != nil {
		t.Fatalf("List (no match) = %v, want nil slice", got)
	}

	// An unknown workspace should behave the same way.
	got, err = s.List(ctx, "no-such-workspace", ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got != nil {
		t.Fatalf("List (unknown workspace) = %v, want nil slice", got)
	}
}

func TestListExcludesExpiredMemories(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	fakeNow := time.Now()
	s.now = func() time.Time { return fakeNow }

	if _, err := s.Put(ctx, Memory{
		ID:          "ephemeral",
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        Episodic,
		Content:     "will expire",
	}, 30*time.Second); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.Put(ctx, Memory{
		ID:          "permanent",
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        Episodic,
		Content:     "will not expire",
	}, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}

	s.now = func() time.Time { return fakeNow.Add(31 * time.Second) }

	got, err := s.List(ctx, "default", ListOptions{AgentID: "agent-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "permanent" {
		t.Fatalf("List after TTL expiry = %+v, want only the still-live \"permanent\" memory", got)
	}
}

func TestHistorySingleVersionHasNoPriorHistory(t *testing.T) {
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

	got, err := s.History(ctx, "default", id)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("History (single Put, never overwritten) = %d entries, want exactly 1 (the current version)", len(got))
	}
	if got[0].Content != "note" || got[0].Version != 1 {
		t.Fatalf("History[0] = %+v, want Content=%q Version=1", got[0], "note")
	}
}

func TestHistoryBuildsUpOldestFirstEndingAtCurrent(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	mustPut := func(content string) {
		if _, err := s.Put(ctx, Memory{
			ID:          "mem-1",
			AgentID:     "agent-1",
			WorkspaceID: "default",
			Kind:        LongTerm,
			Content:     content,
		}, 0); err != nil {
			t.Fatalf("Put(%q): %v", content, err)
		}
	}

	mustPut("v1")
	mustPut("v2")
	mustPut("v3")

	got, err := s.History(ctx, "default", "mem-1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("History = %d entries, want 3", len(got))
	}

	wantContents := []string{"v1", "v2", "v3"}
	wantVersions := []int{1, 2, 3}
	for i, m := range got {
		if m.Content != wantContents[i] {
			t.Errorf("History[%d].Content = %q, want %q", i, m.Content, wantContents[i])
		}
		if m.Version != wantVersions[i] {
			t.Errorf("History[%d].Version = %d, want %d", i, m.Version, wantVersions[i])
		}
	}

	// Guard against the aliasing/shared-pointer bug class: every entry's
	// Content and Embedding must genuinely reflect what was true at that
	// version, not all secretly pointing at the final value.
	if got[0].Content == got[2].Content {
		t.Fatalf("History[0] and History[2] have the same Content %q -- looks like every entry is aliased to the final value", got[0].Content)
	}
	for i := 0; i < len(got); i++ {
		for j := i + 1; j < len(got); j++ {
			if &got[i].Embedding[0] == &got[j].Embedding[0] {
				t.Fatalf("History[%d] and History[%d] share the same Embedding backing array -- aliasing bug", i, j)
			}
		}
	}

	// The last entry must equal the current version as reported by Get.
	current, found, err := s.Get(ctx, "default", "mem-1")
	if err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	if got[2].Content != current.Content || got[2].Version != current.Version {
		t.Fatalf("History's last entry = %+v, want it to match the current version %+v", got[2], current)
	}
}

func TestHistoryUnknownIDReturnsNilNil(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	got, err := s.History(ctx, "default", "does-not-exist")
	if err != nil {
		t.Fatalf("History (unknown id) error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("History (unknown id) = %v, want nil", got)
	}
}

func TestHistoryExpiredCurrentVersionReturnsNilNil(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	fakeNow := time.Now()
	s.now = func() time.Time { return fakeNow }

	id, err := s.Put(ctx, Memory{
		AgentID:     "agent-1",
		WorkspaceID: "default",
		Kind:        LongTerm,
		Content:     "ephemeral",
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	s.now = func() time.Time { return fakeNow.Add(31 * time.Second) }

	got, err := s.History(ctx, "default", id)
	if err != nil {
		t.Fatalf("History (expired current version) error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("History (expired current version) = %v, want nil", got)
	}
}

func TestHistoryBoundedLengthDropsOldest(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	total := maxMemoryHistoryPerRecord + 10
	for i := 0; i < total; i++ {
		if _, err := s.Put(ctx, Memory{
			ID:          "mem-1",
			AgentID:     "agent-1",
			WorkspaceID: "default",
			Kind:        LongTerm,
			Content:     "v" + strconv.Itoa(i),
		}, 0); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	got, err := s.History(ctx, "default", "mem-1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// maxMemoryHistoryPerRecord prior versions + the current version.
	wantLen := maxMemoryHistoryPerRecord + 1
	if len(got) != wantLen {
		t.Fatalf("History length = %d, want %d (bounded to %d prior versions + current)", len(got), wantLen, maxMemoryHistoryPerRecord)
	}
	// The oldest entries should have been dropped: the first entry returned
	// should be the oldest one that survived the cap, not "v0".
	wantFirstContent := "v" + strconv.Itoa(total-wantLen)
	if got[0].Content != wantFirstContent {
		t.Fatalf("History[0].Content = %q, want %q (the oldest surviving version after the cap dropped older ones)", got[0].Content, wantFirstContent)
	}
	// The last entry is always the current version.
	wantLastContent := "v" + strconv.Itoa(total-1)
	if got[len(got)-1].Content != wantLastContent {
		t.Fatalf("History last entry Content = %q, want %q (the current version)", got[len(got)-1].Content, wantLastContent)
	}
}
