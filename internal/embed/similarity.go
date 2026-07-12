package embed

import "math"

// Cosine returns the cosine similarity between vectors a and b: a value in
// [-1, 1] where 1 means the vectors point in exactly the same direction, 0
// means they are orthogonal, and -1 means they point in exactly opposite
// directions. This is the similarity metric the semantic cache (Phase 2)
// and vector store (Phase 3) use to compare embeddings.
//
// Behavior on invalid input, documented explicitly since callers rely on
// it:
//   - If len(a) != len(b), or either slice is empty, the comparison is
//     undefined and Cosine returns math.NaN().
//   - If either vector has zero magnitude (all-zero vector), cosine
//     similarity is mathematically undefined (division by zero) and
//     Cosine returns math.NaN().
//
// Callers that need to treat "undefined" distinctly from a real
// similarity score should check the result with math.IsNaN.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.NaN()
	}

	var dot, normA, normB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	if normA == 0 || normB == 0 {
		return math.NaN()
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
