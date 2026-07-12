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
