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

// Dot returns the raw dot (inner) product between vectors a and b: sum of
// a[i]*b[i]. Unlike Cosine it is not scale-invariant or bounded to [-1, 1],
// so it only makes sense to compare Dot scores against each other when
// every vector involved is (approximately) unit length; comparing Dot
// scores across differently-scaled vectors is not meaningful.
//
// Behavior on invalid input, matching Cosine's convention: if len(a) !=
// len(b), or either slice is empty, the comparison is undefined and Dot
// returns math.NaN().
func Dot(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.NaN()
	}

	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

// Euclidean returns the Euclidean (L2) distance between vectors a and b: a
// value in [0, +Inf) where 0 means the vectors are identical and larger
// values mean the vectors are farther apart. Note this is a *distance*, not
// a similarity: unlike Cosine/Dot, lower is better when ranking by this
// metric.
//
// Behavior on invalid input, matching Cosine's convention: if len(a) !=
// len(b), or either slice is empty, the comparison is undefined and
// Euclidean returns math.NaN().
func Euclidean(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.NaN()
	}

	var sumSq float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sumSq += d * d
	}
	return math.Sqrt(sumSq)
}
