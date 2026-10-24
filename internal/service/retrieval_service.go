package service

import (
	"context"
	"strings"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/config"
	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
	"github.com/memory-engine/memory-engine/internal/retrieval"
	"github.com/memory-engine/memory-engine/internal/scorer"
)

// RetrievalService is the public API for the hybrid retrieval pipeline.
type RetrievalService struct {
	pipeline *retrieval.Pipeline
}

// NewRetrievalService creates a new RetrievalService.
func NewRetrievalService(
	searchRepo *repository.SearchRepository,
	embedRepo *repository.EmbeddingRepository,
	memRepo *repository.MemoryRepository,
	embedSvc *EmbeddingService,
	scoringCfg config.ScoringConfig,
	logger zerolog.Logger,
) *RetrievalService {
	s := scorer.New(scoringCfg)
	pipe := retrieval.NewPipeline(
		searchRepo, embedRepo, memRepo, embedSvc, s,
		retrieval.DefaultPipelineConfig(),
		logger,
	)
	return &RetrievalService{pipeline: pipe}
}

// Retrieve executes the hybrid retrieval pipeline.
func (s *RetrievalService) Retrieve(ctx context.Context, req domain.RetrievalRequest) (*domain.RetrievalResponse, error) {
	// Normalise.
	req.Query = strings.TrimSpace(req.Query)
	req.Namespace = strings.ToUpper(strings.TrimSpace(req.Namespace))

	return s.pipeline.Retrieve(ctx, req)
}
