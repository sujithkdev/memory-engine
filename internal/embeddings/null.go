package embeddings

import (
	"context"
	"math"
	"math/rand"
)

// NullEmbedder returns deterministic pseudo-random unit vectors.
// Used in tests and when Ollama is not available.
// Same text always produces the same vector (seeded from content hash).
type NullEmbedder struct {
	dimensions int
}

// NewNullEmbedder creates a NullEmbedder with the given dimensions.
func NewNullEmbedder(dimensions int) *NullEmbedder {
	if dimensions <= 0 {
		dimensions = 768
	}
	return &NullEmbedder{dimensions: dimensions}
}

func (e *NullEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return pseudoVector(text, e.dimensions), nil
}

func (e *NullEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = pseudoVector(t, e.dimensions)
	}
	return out, nil
}

func (e *NullEmbedder) ModelName() string    { return "null-embedder" }
func (e *NullEmbedder) ModelVersion() string { return "0.0.0" }
func (e *NullEmbedder) Dimensions() int      { return e.dimensions }

// pseudoVector generates a deterministic L2-normalised vector from text.
// Uses float64 throughout for precision, converts to float32 at the end.
func pseudoVector(text string, dims int) []float32 {
	// FNV-1a hash for stable seeding.
	var h uint64 = 14695981039346656037
	for _, b := range []byte(text) {
		h ^= uint64(b)
		h *= 1099511628211
	}

	r := rand.New(rand.NewSource(int64(h))) //nolint:gosec — not cryptographic

	// Generate raw values in float64 for precision.
	raw := make([]float64, dims)
	var sumSq float64
	for i := range raw {
		v := r.NormFloat64()
		raw[i] = v
		sumSq += v * v
	}

	// L2 normalise — divide each element by the L2 norm.
	norm := math.Sqrt(sumSq)
	vec := make([]float32, dims)
	if norm > 0 {
		for i := range raw {
			vec[i] = float32(raw[i] / norm)
		}
	}
	return vec
}
