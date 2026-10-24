// Package embeddings defines the embedding provider interface and implementations.
//
// Design: the Embedder interface decouples retrieval from embedding model choice.
// Swapping from nomic-embed-text → Qwen3-Embedding is a config change, not a
// code change. The re-embedding background worker detects model changes automatically.
package embeddings

import (
	"context"
	"fmt"
	"time"
)

// Embedder is the interface every embedding provider must implement.
type Embedder interface {
	// Embed generates an embedding vector for a single text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts in one call.
	// More efficient than calling Embed in a loop for large batches.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// ModelName returns the name of the underlying model.
	ModelName() string

	// ModelVersion returns a version identifier (hash or tag) for re-embed detection.
	ModelVersion() string

	// Dimensions returns the vector dimensions this model produces.
	Dimensions() int
}

// EmbeddingResult holds one embedding with its metadata.
type EmbeddingResult struct {
	Vector     []float32
	Model      string
	Version    string
	Dimensions int
	CreatedAt  time.Time
}

// ErrEmbeddingUnavailable is returned when the embedding service is unreachable.
type ErrEmbeddingUnavailable struct {
	Model  string
	Reason string
}

func (e *ErrEmbeddingUnavailable) Error() string {
	return fmt.Sprintf("embedding model %q unavailable: %s", e.Model, e.Reason)
}
