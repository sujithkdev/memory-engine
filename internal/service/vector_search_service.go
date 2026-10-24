package service

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

// VectorSearchService performs semantic vector search over memory embeddings.
type VectorSearchService struct {
	embedSvc   *EmbeddingService
	embedRepo  *repository.EmbeddingRepository
	memoryRepo *repository.MemoryRepository
}

// NewVectorSearchService creates a new VectorSearchService.
func NewVectorSearchService(
	embedSvc *EmbeddingService,
	embedRepo *repository.EmbeddingRepository,
	memoryRepo *repository.MemoryRepository,
) *VectorSearchService {
	return &VectorSearchService{
		embedSvc:   embedSvc,
		embedRepo:  embedRepo,
		memoryRepo: memoryRepo,
	}
}

// VectorSearchRequest is the input for a vector similarity search.
type VectorSearchRequest struct {
	UserID     uuid.UUID
	Query      string
	Namespace  *string
	MemoryType *domain.MemoryType
	TopK       int
	// MinSimilarity filters results below this cosine similarity threshold.
	MinSimilarity float64
}

// VectorSearchResponse is the result of a vector similarity search.
type VectorSearchResponse struct {
	Results       []domain.ScoredMemory `json:"results"`
	Total         int                   `json:"total"`
	Query         string                `json:"query"`
	Model         string                `json:"model"`
	MinSimilarity float64               `json:"min_similarity"`
}

// Search embeds the query and performs ANN search, then fetches full memory objects.
func (s *VectorSearchService) Search(ctx context.Context, req VectorSearchRequest) (*VectorSearchResponse, error) {
	if req.Query == "" {
		return &VectorSearchResponse{Query: req.Query, Model: s.embedSvc.ModelName()}, nil
	}
	if req.TopK <= 0 {
		req.TopK = 20
	}
	if req.MinSimilarity < 0 {
		req.MinSimilarity = 0
	}

	// Step 1: Embed the query.
	queryVec, err := s.embedSvc.EmbedQuery(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	// Step 2: ANN search with pre-filters.
	filter := domain.MemoryFilter{
		UserID:     req.UserID,
		Namespace:  req.Namespace,
		MemoryType: req.MemoryType,
	}

	rawResults, err := s.embedRepo.VectorSearch(
		ctx, req.UserID, queryVec,
		s.embedSvc.ModelName(),
		filter, req.TopK,
	)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// Step 3: Filter by min similarity and fetch full memory objects.
	var scored []domain.ScoredMemory
	for _, r := range rawResults {
		if r.Similarity < req.MinSimilarity {
			continue
		}
		mem, err := s.memoryRepo.GetByIDAdmin(ctx, r.MemoryID)
		if err != nil {
			continue // memory may have been deleted since indexing
		}
		scored = append(scored, domain.ScoredMemory{
			Memory:     mem,
			FinalScore: r.Similarity,
			ScoreBreakdown: domain.ScoreBreakdown{
				Relevance:     r.Similarity,
				Importance:    mem.Importance,
				Confidence:    mem.Confidence,
				WeightedTotal: r.Similarity,
			},
		})
	}

	return &VectorSearchResponse{
		Results:       scored,
		Total:         len(scored),
		Query:         req.Query,
		Model:         s.embedSvc.ModelName(),
		MinSimilarity: req.MinSimilarity,
	}, nil
}

// CosineSimilarity computes cosine similarity between two float32 vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
