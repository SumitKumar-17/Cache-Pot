package vector

import (
	"math"
	"testing"
)

func TestUpsertSearchClosestFirst(t *testing.T) {
	s := New()
	s.Upsert("ns", "a", []float32{1, 0}, nil, "")
	s.Upsert("ns", "b", []float32{0, 1}, nil, "")
	s.Upsert("ns", "c", []float32{0.9, 0.1}, nil, "")

	results := s.Search("ns", []float32{1, 0}, 10, Cosine, nil, nil)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].ID != "a" {
		t.Fatalf("closest match = %q, want %q", results[0].ID, "a")
	}
	if results[1].ID != "c" {
		t.Fatalf("second closest = %q, want %q", results[1].ID, "c")
	}
	if results[2].ID != "b" {
		t.Fatalf("farthest = %q, want %q", results[2].ID, "b")
	}
}

func TestSearchKCapsResults(t *testing.T) {
	s := New()
	for i := 0; i < 5; i++ {
		s.Upsert("ns", string(rune('a'+i)), []float32{float32(i), 0}, nil, "")
	}
	results := s.Search("ns", []float32{0, 0}, 2, Euclidean, nil, nil)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (K cap)", len(results))
	}
}

func TestSearchMetricSwitchChangesRanking(t *testing.T) {
	s := New()
	// "near" is close in Euclidean distance to the query but has low dot
	// product / different cosine direction; "aligned" points in the exact
	// same direction as the query (best cosine/dot) but is farther away in
	// raw Euclidean terms.
	query := []float32{1, 1}
	s.Upsert("ns", "near", []float32{1.1, 1.1 - 0.05}, nil, "") // close in direction AND distance; control
	s.Upsert("ns", "aligned", []float32{10, 10}, nil, "")       // same direction, large magnitude: best cosine/dot, worst euclidean
	s.Upsert("ns", "close", []float32{1, 0.9}, nil, "")         // closest euclidean distance to query

	byEuclidean := s.Search("ns", query, 1, Euclidean, nil, nil)
	byDot := s.Search("ns", query, 1, Dot, nil, nil)

	if byEuclidean[0].ID != "close" {
		t.Fatalf("Euclidean best = %q, want %q", byEuclidean[0].ID, "close")
	}
	if byDot[0].ID != "aligned" {
		t.Fatalf("Dot best = %q, want %q", byDot[0].ID, "aligned")
	}
	if byEuclidean[0].ID == byDot[0].ID {
		t.Fatalf("expected metric switch to change the top result, got %q both times", byEuclidean[0].ID)
	}
}

func TestNamespaceIsolation(t *testing.T) {
	s := New()
	s.Upsert("ns1", "x", []float32{1, 0}, nil, "")
	s.Upsert("ns2", "x", []float32{0, 1}, nil, "")

	r1 := s.Search("ns1", []float32{1, 0}, 10, Cosine, nil, nil)
	if len(r1) != 1 || r1[0].ID != "x" {
		t.Fatalf("ns1 search = %+v, want single match id=x", r1)
	}
	if math.Abs(r1[0].Score-1.0) > 1e-6 {
		t.Fatalf("ns1 top score = %v, want ~1.0", r1[0].Score)
	}

	r2 := s.Search("ns2", []float32{1, 0}, 10, Cosine, nil, nil)
	if len(r2) != 1 || r2[0].ID != "x" {
		t.Fatalf("ns2 search = %+v, want single match id=x", r2)
	}
	if math.Abs(r2[0].Score-0.0) > 1e-6 {
		t.Fatalf("ns2 top score = %v, want ~0.0 (orthogonal)", r2[0].Score)
	}

	// A namespace that never had anything upserted into it.
	r3 := s.Search("ns3", []float32{1, 0}, 10, Cosine, nil, nil)
	if len(r3) != 0 {
		t.Fatalf("unknown namespace search = %+v, want empty", r3)
	}
}

func TestSearchFilterExcludesNonMatching(t *testing.T) {
	s := New()
	s.Upsert("ns", "a", []float32{1, 0}, map[string]string{"category": "fruit"}, "")
	s.Upsert("ns", "b", []float32{1, 0}, map[string]string{"category": "veg"}, "")
	s.Upsert("ns", "c", []float32{1, 0}, nil, "")

	results := s.Search("ns", []float32{1, 0}, 10, Cosine, map[string]string{"category": "fruit"}, nil)
	if len(results) != 1 || results[0].ID != "a" {
		t.Fatalf("filtered search = %+v, want single match id=a", results)
	}
}

func TestSearchDimensionMismatchSkipped(t *testing.T) {
	s := New()
	s.Upsert("ns", "good", []float32{1, 0}, nil, "")
	s.Upsert("ns", "wrongdim", []float32{1, 0, 0}, nil, "")

	results := s.Search("ns", []float32{1, 0}, 10, Cosine, nil, nil)
	if len(results) != 1 || results[0].ID != "good" {
		t.Fatalf("search with mixed dims = %+v, want single match id=good", results)
	}
}

func TestDeleteRemovesEntry(t *testing.T) {
	s := New()
	s.Upsert("ns", "a", []float32{1, 0}, nil, "")

	if !s.Delete("ns", "a") {
		t.Fatalf("Delete(existing) = false, want true")
	}
	if s.Delete("ns", "a") {
		t.Fatalf("Delete(already deleted) = true, want false")
	}

	results := s.Search("ns", []float32{1, 0}, 10, Cosine, nil, nil)
	if len(results) != 0 {
		t.Fatalf("search after delete = %+v, want empty", results)
	}
}

func TestDeleteUnknownNamespace(t *testing.T) {
	s := New()
	if s.Delete("nope", "a") {
		t.Fatalf("Delete(unknown namespace) = true, want false")
	}
}

func TestUpsertReplacesEntirely(t *testing.T) {
	s := New()
	s.Upsert("ns", "a", []float32{1, 0}, map[string]string{"k": "v1"}, "hello world")
	s.Upsert("ns", "a", []float32{0, 1}, map[string]string{"k": "v2"}, "goodbye")

	// Old metadata should no longer match.
	r := s.Search("ns", []float32{0, 1}, 10, Cosine, map[string]string{"k": "v1"}, nil)
	if len(r) != 0 {
		t.Fatalf("search on stale metadata = %+v, want empty (metadata should be fully replaced)", r)
	}
	r = s.Search("ns", []float32{0, 1}, 10, Cosine, map[string]string{"k": "v2"}, nil)
	if len(r) != 1 || r[0].ID != "a" {
		t.Fatalf("search on new metadata = %+v, want single match id=a", r)
	}
}

func TestHybridSearchChangesRanking(t *testing.T) {
	s := New()
	query := []float32{1, 0}

	// "vecwinner" is a near-perfect vector match but has irrelevant text.
	s.Upsert("ns", "vecwinner", []float32{1, 0.01}, nil, "completely unrelated text")
	// "kwwinner" is a weaker vector match (still positive cosine) but its
	// text matches the hybrid query exactly.
	s.Upsert("ns", "kwwinner", []float32{1, 0.5}, nil, "golang cache pot vector search")

	pure := s.Search("ns", query, 1, Cosine, nil, nil)
	if pure[0].ID != "vecwinner" {
		t.Fatalf("pure-vector best = %q, want %q", pure[0].ID, "vecwinner")
	}

	hybrid := s.Search("ns", query, 1, Cosine, nil, &HybridOpts{
		QueryText: "golang cache pot vector search",
		Alpha:     0.2, // weight keyword overlap heavily
	})
	if hybrid[0].ID != "kwwinner" {
		t.Fatalf("hybrid best = %q, want %q (keyword overlap should flip ranking)", hybrid[0].ID, "kwwinner")
	}
}

func TestSearchKZeroOrNegative(t *testing.T) {
	s := New()
	s.Upsert("ns", "a", []float32{1, 0}, nil, "")
	if r := s.Search("ns", []float32{1, 0}, 0, Cosine, nil, nil); r != nil {
		t.Fatalf("Search with K=0 = %+v, want nil", r)
	}
	if r := s.Search("ns", []float32{1, 0}, -1, Cosine, nil, nil); r != nil {
		t.Fatalf("Search with K=-1 = %+v, want nil", r)
	}
}
