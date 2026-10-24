package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OllamaEmbedder calls the Ollama HTTP API to generate embeddings locally.
// Supports any model served by Ollama (nomic-embed-text, qwen3-embedding, etc.)
//
// Ollama embed API: POST http://localhost:11434/api/embed
//
//	{"model": "nomic-embed-text", "input": "text here"}
type OllamaEmbedder struct {
	baseURL    string
	model      string
	version    string
	dimensions int
	httpClient *http.Client
}

// OllamaConfig holds configuration for the Ollama embedder.
type OllamaConfig struct {
	BaseURL    string        // default: http://localhost:11434
	Model      string        // e.g. "nomic-embed-text", "qwen3-embedding"
	Version    string        // model tag/version for re-embed detection
	Dimensions int           // vector dimensions for this model
	Timeout    time.Duration // HTTP request timeout
}

// NewOllamaEmbedder creates a new OllamaEmbedder.
func NewOllamaEmbedder(cfg OllamaConfig) *OllamaEmbedder {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Dimensions == 0 {
		cfg.Dimensions = 768 // nomic-embed-text default
	}

	return &OllamaEmbedder{
		baseURL:    cfg.BaseURL,
		model:      cfg.Model,
		version:    cfg.Version,
		dimensions: cfg.Dimensions,
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}
}

// Embed generates an embedding for a single text.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}
	return results[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
// Ollama's /api/embed endpoint accepts an array of inputs.
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]interface{}{
		"model": e.model,
		"input": texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, &ErrEmbeddingUnavailable{Model: e.model, Reason: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &ErrEmbeddingUnavailable{
			Model:  e.model,
			Reason: fmt.Sprintf("HTTP %d from Ollama", resp.StatusCode),
		}
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding embed response: %w", err)
	}

	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama returned no embeddings for batch of %d", len(texts))
	}

	return result.Embeddings, nil
}

func (e *OllamaEmbedder) ModelName() string  { return e.model }
func (e *OllamaEmbedder) ModelVersion() string { return e.version }
func (e *OllamaEmbedder) Dimensions() int    { return e.dimensions }
