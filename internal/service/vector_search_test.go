package service_test

import (
	"math"
	"testing"

	"github.com/memory-engine/memory-engine/internal/service"
)

// ── CosineSimilarity ──────────────────────────────────────────────────────────

func TestCosineSimilarity_IdenticalVectors_ReturnsOne(t *testing.T) {
	v := []float32{0.1, 0.2, 0.3, 0.4}
	sim := service.CosineSimilarity(v, v)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("identical vectors should have similarity 1.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_OppositeVectors_ReturnsNegativeOne(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{-1.0, 0.0}
	sim := service.CosineSimilarity(a, b)
	if math.Abs(sim-(-1.0)) > 0.001 {
		t.Errorf("opposite vectors should have similarity -1.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_OrthogonalVectors_ReturnsZero(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{0.0, 1.0}
	sim := service.CosineSimilarity(a, b)
	if math.Abs(sim) > 0.001 {
		t.Errorf("orthogonal vectors should have similarity 0.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_ZeroVector_ReturnsZero(t *testing.T) {
	a := []float32{0.0, 0.0, 0.0}
	b := []float32{1.0, 2.0, 3.0}
	sim := service.CosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("zero vector should return 0 similarity, got %.4f", sim)
	}
}

func TestCosineSimilarity_DifferentLengths_ReturnsZero(t *testing.T) {
	a := []float32{1.0, 2.0}
	b := []float32{1.0, 2.0, 3.0}
	sim := service.CosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("mismatched lengths should return 0, got %.4f", sim)
	}
}

func TestCosineSimilarity_ScaledVectors_SameDirection(t *testing.T) {
	// Scaling should not affect cosine similarity.
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{2.0, 4.0, 6.0} // a * 2
	sim := service.CosineSimilarity(a, b)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("same-direction vectors should have similarity 1.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_Range(t *testing.T) {
	// Test several random-ish vectors stay in [-1, 1].
	cases := []struct{ a, b []float32 }{
		{[]float32{0.5, 0.5}, []float32{0.7, 0.3}},
		{[]float32{1, 0, 0}, []float32{0, 1, 0}},
		{[]float32{0.9, 0.1}, []float32{0.1, 0.9}},
	}
	for _, c := range cases {
		sim := service.CosineSimilarity(c.a, c.b)
		if sim < -1.001 || sim > 1.001 {
			t.Errorf("similarity %.4f out of [-1,1] for %v, %v", sim, c.a, c.b)
		}
	}
}
