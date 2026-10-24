package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/embeddings"
	"github.com/memory-engine/memory-engine/internal/repository"
)

// EmbeddingService orchestrates embedding generation and vector storage.
type EmbeddingService struct {
	embedder    embeddings.Embedder
	embedRepo   *repository.EmbeddingRepository
	memoryRepo  *repository.MemoryRepository
	logger      zerolog.Logger
}

// NewEmbeddingService creates a new EmbeddingService.
func NewEmbeddingService(
	embedder embeddings.Embedder,
	embedRepo *repository.EmbeddingRepository,
	memoryRepo *repository.MemoryRepository,
	logger zerolog.Logger,
) *EmbeddingService {
	return &EmbeddingService{
		embedder:   embedder,
		embedRepo:  embedRepo,
		memoryRepo: memoryRepo,
		logger:     logger,
	}
}

// EmbedMemory generates and stores an embedding for one memory.
// Called after memory creation and after content updates.
func (s *EmbeddingService) EmbedMemory(ctx context.Context, memoryID uuid.UUID) error {
	mem, err := s.memoryRepo.GetByIDAdmin(ctx, memoryID)
	if err != nil {
		return fmt.Errorf("fetching memory for embedding: %w", err)
	}

	vector, err := s.embedder.Embed(ctx, mem.Content)
	if err != nil {
		// Embedding unavailable — mark for retry, don't fail the request.
		s.logger.Warn().
			Err(err).
			Str("memory_id", memoryID.String()).
			Msg("embedding failed — memory flagged for retry")
		return nil
	}

	if err := s.embedRepo.Upsert(
		ctx, memoryID, vector,
		s.embedder.ModelName(),
		s.embedder.ModelVersion(),
		s.embedder.Dimensions(),
	); err != nil {
		return fmt.Errorf("storing embedding: %w", err)
	}

	s.logger.Debug().
		Str("memory_id", memoryID.String()).
		Str("model", s.embedder.ModelName()).
		Int("dims", s.embedder.Dimensions()).
		Msg("memory embedded")

	return nil
}

// EmbedBatch embeds multiple memories. Used by the background re-embedding worker.
func (s *EmbeddingService) EmbedBatch(ctx context.Context, memoryIDs []uuid.UUID) (int, error) {
	if len(memoryIDs) == 0 {
		return 0, nil
	}

	// Fetch contents.
	texts := make([]string, 0, len(memoryIDs))
	validIDs := make([]uuid.UUID, 0, len(memoryIDs))
	for _, id := range memoryIDs {
		mem, err := s.memoryRepo.GetByIDAdmin(ctx, id)
		if err != nil {
			s.logger.Warn().Err(err).Str("id", id.String()).Msg("skipping memory for batch embed")
			continue
		}
		texts = append(texts, mem.Content)
		validIDs = append(validIDs, id)
	}

	if len(texts) == 0 {
		return 0, nil
	}

	vectors, err := s.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("batch embedding failed: %w", err)
	}

	count := 0
	for i, vec := range vectors {
		if i >= len(validIDs) {
			break
		}
		if err := s.embedRepo.Upsert(
			ctx, validIDs[i], vec,
			s.embedder.ModelName(),
			s.embedder.ModelVersion(),
			s.embedder.Dimensions(),
		); err != nil {
			s.logger.Warn().Err(err).Str("id", validIDs[i].String()).Msg("failed to store embedding")
			continue
		}
		count++
	}

	s.logger.Info().
		Int("total", len(memoryIDs)).
		Int("embedded", count).
		Str("model", s.embedder.ModelName()).
		Msg("batch embedding complete")

	return count, nil
}

// EmbedQuery generates an embedding for a search query string (not stored).
func (s *EmbeddingService) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vec, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}
	return vec, nil
}

// ProcessReembeddingQueue processes memories flagged for re-embedding.
// Returns number of memories successfully re-embedded.
func (s *EmbeddingService) ProcessReembeddingQueue(ctx context.Context, batchSize int) (int, error) {
	ids, err := s.embedRepo.ListNeedsReembedding(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("fetching reembedding queue: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	s.logger.Info().Int("count", len(ids)).Msg("processing re-embedding queue")
	return s.EmbedBatch(ctx, ids)
}

// ActivateModel registers a new active model and flags all memories for re-embedding.
// Call this when switching embedding models.
func (s *EmbeddingService) ActivateModel(ctx context.Context) error {
	if err := s.embedRepo.RegisterModel(
		ctx,
		s.embedder.ModelName(),
		s.embedder.ModelVersion(),
		s.embedder.Dimensions(),
	); err != nil {
		return fmt.Errorf("registering model: %w", err)
	}

	flagged, err := s.embedRepo.MarkNeedsReembedding(ctx)
	if err != nil {
		return fmt.Errorf("flagging memories for re-embedding: %w", err)
	}

	s.logger.Info().
		Str("model", s.embedder.ModelName()).
		Int64("flagged", flagged).
		Msg("new embedding model activated — memories flagged for re-embedding")

	return nil
}

// ModelName returns the active embedding model name.
func (s *EmbeddingService) ModelName() string { return s.embedder.ModelName() }

// Dimensions returns the active embedding model's vector dimensions.
func (s *EmbeddingService) Dimensions() int { return s.embedder.Dimensions() }
