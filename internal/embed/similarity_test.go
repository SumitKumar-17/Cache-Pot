package embed

import (
	"math"
	"testing"
)

func TestCosineIdentical(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2, 3}
	got := Cosine(a, b)
	if math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("Cosine(identical) = %v, want ~1.0", got)
	}
}

func TestCosineOrthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	got := Cosine(a, b)
	if math.Abs(got-0.0) > 1e-6 {
		t.Fatalf("Cosine(orthogonal) = %v, want ~0.0", got)
	}
}

func TestCosineOpposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	got := Cosine(a, b)
	if math.Abs(got-(-1.0)) > 1e-6 {
		t.Fatalf("Cosine(opposite) = %v, want ~-1.0", got)
	}
}

func TestCosineDimensionMismatch(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	got := Cosine(a, b)
	if !math.IsNaN(got) {
		t.Fatalf("Cosine(mismatched dims) = %v, want NaN", got)
	}
}

func TestCosineEmptyVectors(t *testing.T) {
	got := Cosine(nil, nil)
	if !math.IsNaN(got) {
		t.Fatalf("Cosine(empty) = %v, want NaN", got)
	}
}

func TestCosineZeroMagnitude(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	got := Cosine(a, b)
	if !math.IsNaN(got) {
		t.Fatalf("Cosine(zero-magnitude vector) = %v, want NaN", got)
	}
}

func TestCosineScaleInvariant(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{2, 4, 6} // same direction, different magnitude
	got := Cosine(a, b)
	if math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("Cosine(scaled same-direction vectors) = %v, want ~1.0", got)
	}
}

func TestDotIdentical(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2, 3}
	got := Dot(a, b)
	if math.Abs(got-14.0) > 1e-6 {
		t.Fatalf("Dot(identical) = %v, want 14.0", got)
	}
}

func TestDotOrthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	got := Dot(a, b)
	if math.Abs(got-0.0) > 1e-6 {
		t.Fatalf("Dot(orthogonal) = %v, want 0.0", got)
	}
}

func TestDotKnownValue(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{4, 5, 6}
	// 1*4 + 2*5 + 3*6 = 4 + 10 + 18 = 32
	got := Dot(a, b)
	if math.Abs(got-32.0) > 1e-6 {
		t.Fatalf("Dot(known) = %v, want 32.0", got)
	}
}

func TestDotDimensionMismatch(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	got := Dot(a, b)
	if !math.IsNaN(got) {
		t.Fatalf("Dot(mismatched dims) = %v, want NaN", got)
	}
}

func TestDotEmptyVectors(t *testing.T) {
	got := Dot(nil, nil)
	if !math.IsNaN(got) {
		t.Fatalf("Dot(empty) = %v, want NaN", got)
	}
}

func TestEuclideanIdentical(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2, 3}
	got := Euclidean(a, b)
	if math.Abs(got-0.0) > 1e-6 {
		t.Fatalf("Euclidean(identical) = %v, want 0.0", got)
	}
}

func TestEuclideanKnownValue(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{3, 4}
	// classic 3-4-5 triangle: distance == 5
	got := Euclidean(a, b)
	if math.Abs(got-5.0) > 1e-6 {
		t.Fatalf("Euclidean(3-4-5) = %v, want 5.0", got)
	}
}

func TestEuclideanDimensionMismatch(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	got := Euclidean(a, b)
	if !math.IsNaN(got) {
		t.Fatalf("Euclidean(mismatched dims) = %v, want NaN", got)
	}
}

func TestEuclideanEmptyVectors(t *testing.T) {
	got := Euclidean(nil, nil)
	if !math.IsNaN(got) {
		t.Fatalf("Euclidean(empty) = %v, want NaN", got)
	}
}
