package embeddings_test

import (
	"context"
	"math"
	"testing"

	"github.com/memory-engine/memory-engine/internal/embeddings"
)

// ── NullEmbedder ──────────────────────────────────────────────────────────────

func TestNullEmbedder_ReturnsCorrectDimensions(t *testing.T) {
	e := embeddings.NewNullEmbedder(768)
	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 768 {
		t.Errorf("expected 768 dimensions, got %d", len(vec))
	}
}

func TestNullEmbedder_CustomDimensions(t *testing.T) {
	e := embeddings.NewNullEmbedder(1024)
	if e.Dimensions() != 1024 {
		t.Errorf("expected 1024, got %d", e.Dimensions())
	}
	vec, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 1024 {
		t.Errorf("expected 1024 dimensions, got %d", len(vec))
	}
}

func TestNullEmbedder_DefaultDimensions_WhenZero(t *testing.T) {
	e := embeddings.NewNullEmbedder(0)
	if e.Dimensions() != 768 {
		t.Errorf("zero dims should default to 768, got %d", e.Dimensions())
	}
}

func TestNullEmbedder_ModelName(t *testing.T) {
	e := embeddings.NewNullEmbedder(768)
	if e.ModelName() != "null-embedder" {
		t.Errorf("expected 'null-embedder', got %q", e.ModelName())
	}
}

func TestNullEmbedder_ModelVersion(t *testing.T) {
	e := embeddings.NewNullEmbedder(768)
	if e.ModelVersion() == "" {
		t.Error("model version should not be empty")
	}
}

func TestNullEmbedder_SameText_SameVector(t *testing.T) {
	e := embeddings.NewNullEmbedder(128)
	v1, _ := e.Embed(context.Background(), "python is great")
	v2, _ := e.Embed(context.Background(), "python is great")

	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("same text should produce same vector at index %d", i)
			break
		}
	}
}

func TestNullEmbedder_DifferentText_DifferentVector(t *testing.T) {
	e := embeddings.NewNullEmbedder(128)
	v1, _ := e.Embed(context.Background(), "python")
	v2, _ := e.Embed(context.Background(), "golang")

	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different text should produce different vectors")
	}
}

func TestNullEmbedder_VectorIsNormalized(t *testing.T) {
	e := embeddings.NewNullEmbedder(64)
	vec, _ := e.Embed(context.Background(), "test normalization")

	// Compute L2 norm in float64 for accurate check.
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)

	// float32 precision means norm won't be exactly 1.0 but should be very close.
	if math.Abs(norm-1.0) > 0.01 {
		t.Errorf("expected unit vector (norm≈1.0), got norm=%.6f", norm)
	}
}

func TestNullEmbedder_EmbedBatch(t *testing.T) {
	e := embeddings.NewNullEmbedder(64)
	texts := []string{"python", "golang", "rust", "java"}

	vecs, err := e.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch error: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Errorf("expected %d vectors, got %d", len(texts), len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 64 {
			t.Errorf("vector %d: expected 64 dims, got %d", i, len(v))
		}
	}
}

func TestNullEmbedder_EmbedBatch_Empty(t *testing.T) {
	e := embeddings.NewNullEmbedder(64)
	vecs, err := e.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("expected 0 vectors for empty input, got %d", len(vecs))
	}
}

func TestNullEmbedder_BatchMatchesSingle(t *testing.T) {
	e := embeddings.NewNullEmbedder(32)
	texts := []string{"hello", "world"}

	single1, _ := e.Embed(context.Background(), texts[0])
	single2, _ := e.Embed(context.Background(), texts[1])
	batch, _ := e.EmbedBatch(context.Background(), texts)

	for i := range single1 {
		if single1[i] != batch[0][i] {
			t.Errorf("batch[0] differs from single embed at index %d", i)
			break
		}
	}
	for i := range single2 {
		if single2[i] != batch[1][i] {
			t.Errorf("batch[1] differs from single embed at index %d", i)
			break
		}
	}
}

// ── OllamaEmbedder config ─────────────────────────────────────────────────────

func TestOllamaEmbedder_ModelInfo(t *testing.T) {
	e := embeddings.NewOllamaEmbedder(embeddings.OllamaConfig{
		Model:      "nomic-embed-text",
		Version:    "v1.5",
		Dimensions: 768,
	})
	if e.ModelName() != "nomic-embed-text" {
		t.Errorf("expected 'nomic-embed-text', got %q", e.ModelName())
	}
	if e.ModelVersion() != "v1.5" {
		t.Errorf("expected 'v1.5', got %q", e.ModelVersion())
	}
	if e.Dimensions() != 768 {
		t.Errorf("expected 768, got %d", e.Dimensions())
	}
}

func TestOllamaEmbedder_DefaultBaseURL(t *testing.T) {
	// Just tests construction doesn't panic with empty URL.
	e := embeddings.NewOllamaEmbedder(embeddings.OllamaConfig{
		Model:      "nomic-embed-text",
		Dimensions: 768,
	})
	if e.ModelName() != "nomic-embed-text" {
		t.Errorf("expected model name to be set")
	}
}
