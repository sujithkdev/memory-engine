package retrieval

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
	"github.com/memory-engine/memory-engine/internal/scorer"
	"github.com/memory-engine/memory-engine/internal/search"
)

// EmbedQuerier is a minimal interface the pipeline needs from the embedding service.
// Breaks the import cycle: retrieval → service.
type EmbedQuerier interface {
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
	ModelName() string
}

// Pipeline is the full hybrid retrieval pipeline.
//
//	Query
//	  ↓ classify (episodic vs semantic)
//	  ↓ namespace + type filter
//	  ↓ keyword search (FTS)  ──┐
//	  ↓ vector search (ANN)   ──┴→ merge (RRF blend)
//	  ↓ top-CandidatePool candidates
//	  ↓ reranker (scoring engine)
//	  ↓ top-K results
//	  ↓ conflict check (skip duplicates)
//	  ↓ context compression
//	  ↓ token budget
//	  ↓ final results
type Pipeline struct {
	searchRepo *repository.SearchRepository
	embedRepo  *repository.EmbeddingRepository
	memRepo    *repository.MemoryRepository
	embedSvc   EmbedQuerier
	reranker   Reranker
	budget     *TokenBudget
	compressor *ContextCompressor
	logger     zerolog.Logger
}

// PipelineConfig configures the retrieval pipeline.
type PipelineConfig struct {
	// Alpha blends keyword vs vector scores (0.5 = equal weight).
	Alpha float64
	// MaxTokensPerMemory limits individual memory content size.
	MaxTokensPerMemory int
	// MinScore filters out low-quality candidates.
	MinScore float64
}

// DefaultPipelineConfig returns sensible defaults.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		Alpha:              0.5,
		MaxTokensPerMemory: 300,
		MinScore:           0.0,
	}
}

// NewPipeline creates a new retrieval Pipeline.
func NewPipeline(
	searchRepo *repository.SearchRepository,
	embedRepo *repository.EmbeddingRepository,
	memRepo *repository.MemoryRepository,
	embedSvc EmbedQuerier,
	scorerInstance *scorer.Scorer,
	cfg PipelineConfig,
	logger zerolog.Logger,
) *Pipeline {
	return &Pipeline{
		searchRepo: searchRepo,
		embedRepo:  embedRepo,
		memRepo:    memRepo,
		embedSvc:   embedSvc,
		reranker:   NewScoreReranker(scorerInstance),
		budget:     NewTokenBudget(DefaultTokenBudget),
		compressor: NewContextCompressor(cfg.MaxTokensPerMemory),
		logger:     logger,
	}
}

// Retrieve executes the full hybrid retrieval pipeline.
func (p *Pipeline) Retrieve(ctx context.Context, req domain.RetrievalRequest) (*domain.RetrievalResponse, error) {
	if req.Query == "" {
		return &domain.RetrievalResponse{Query: req.Query}, nil
	}

	// Apply defaults.
	if req.TopK <= 0 {
		req.TopK = DefaultTopK
	}
	if req.CandidatePool <= 0 {
		req.CandidatePool = DefaultCandidatePool
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = DefaultTokenBudget
	}

	trace := &domain.RetrievalTrace{QueryType: "hybrid"}
	budget := NewTokenBudget(req.MaxTokens)

	// ── Stage 1: Build filters ────────────────────────────────────────────────
	filter := domain.MemoryFilter{
		UserID: req.UserID,
		Page:   1, PageSize: req.CandidatePool,
	}
	if req.Namespace != "" {
		ns := strings.ToUpper(req.Namespace)
		filter.Namespace = &ns
	}
	if len(req.MemoryTypes) == 1 {
		filter.MemoryType = &req.MemoryTypes[0]
	}

	// ── Stage 2: Keyword search (FTS) ─────────────────────────────────────────
	parsedQuery := search.ParseQueryAuto(req.Query)
	ftsResp, err := p.searchRepo.Search(ctx, req.UserID, parsedQuery, filter, 0)
	if err != nil {
		p.logger.Warn().Err(err).Msg("keyword search failed — continuing with vector only")
		ftsResp = &domain.SearchResponse{}
	}

	keywordResults := make([]KeywordResult, 0, len(ftsResp.Results))
	for _, r := range ftsResp.Results {
		keywordResults = append(keywordResults, KeywordResult{
			Memory: r.Memory,
			Rank:   r.Rank,
		})
	}
	trace.KeywordHits = len(keywordResults)

	// ── Stage 3: Vector search (ANN) ─────────────────────────────────────────
	vectorResults := make([]VectorResult, 0)
	queryVec, embedErr := p.embedSvc.EmbedQuery(ctx, req.Query)
	if embedErr != nil {
		p.logger.Warn().Err(embedErr).Msg("query embedding failed — using keyword only")
	} else {
		rawVec, vsErr := p.embedRepo.VectorSearch(
			ctx, req.UserID, queryVec,
			p.embedSvc.ModelName(), filter, req.CandidatePool,
		)
		if vsErr != nil {
			p.logger.Warn().Err(vsErr).Msg("vector search failed — using keyword only")
		} else {
			for _, r := range rawVec {
				mem, fetchErr := p.memRepo.GetByIDAdmin(ctx, r.MemoryID)
				if fetchErr != nil {
					continue
				}
				vectorResults = append(vectorResults, VectorResult{
					Memory:     mem,
					Similarity: r.Similarity,
				})
			}
		}
	}
	trace.VectorHits = len(vectorResults)

	// ── Stage 4: Merge (RRF blend) ────────────────────────────────────────────
	candidates := MergeResults(keywordResults, vectorResults, 0.5)
	trace.MergedTotal = len(candidates)

	if len(candidates) == 0 {
		return &domain.RetrievalResponse{
			Query:                req.Query,
			Namespace:            req.Namespace,
			TokenBudget:          req.MaxTokens,
			CandidatesConsidered: 0,
			Trace:                trace,
		}, nil
	}

	// ── Stage 5: Rerank ───────────────────────────────────────────────────────
	reranked, rerankErr := p.reranker.Rerank(ctx, req.Query, candidates, req.CandidatePool)
	if rerankErr != nil {
		p.logger.Warn().Err(rerankErr).Msg("reranking failed — using merged order")
		reranked = candidates
	}

	// Filter by minimum score.
	if req.MinScore > 0 {
		filtered := reranked[:0]
		for _, c := range reranked {
			if c.FinalScore >= req.MinScore {
				filtered = append(filtered, c)
			}
		}
		reranked = filtered
	}

	// Take only top TopK after reranking.
	if len(reranked) > req.TopK {
		reranked = reranked[:req.TopK]
	}
	trace.AfterRerank = len(reranked)

	// ── Stage 6: Conflict deduplication ──────────────────────────────────────
	// Remove near-duplicate memories (same content hash or very similar content).
	reranked = deduplicateCandidates(reranked)

	// ── Stage 7: Context compression ─────────────────────────────────────────
	for i := range reranked {
		reranked[i].Memory = p.compressor.Compress(reranked[i].Memory)
	}

	// ── Stage 8: Token budget ─────────────────────────────────────────────────
	budgeted, tokensUsed := budget.Apply(reranked)
	trace.AfterBudget = len(budgeted)

	// ── Stage 9: Build response ───────────────────────────────────────────────
	results := make([]domain.RetrievalResult, 0, len(budgeted))
	for _, c := range budgeted {
		r := domain.RetrievalResult{
			Memory:     c.Memory,
			FinalScore: c.FinalScore,
			Sources:    c.Sources,
			TokenCount: EstimateTokens(c.Memory.Content),
		}
		if req.IncludeScoreBreakdown && c.ScoreBreakdown != nil {
			r.ScoreBreakdown = c.ScoreBreakdown
		}
		results = append(results, r)
	}

	p.logger.Debug().
		Str("query", req.Query).
		Int("keyword_hits", trace.KeywordHits).
		Int("vector_hits", trace.VectorHits).
		Int("merged", trace.MergedTotal).
		Int("final", len(results)).
		Int("tokens", tokensUsed).
		Msg("retrieval pipeline complete")

	return &domain.RetrievalResponse{
		Results:              results,
		TokensUsed:           tokensUsed,
		TokenBudget:          req.MaxTokens,
		CandidatesConsidered: trace.MergedTotal,
		Query:                req.Query,
		Namespace:            req.Namespace,
		Trace:                trace,
	}, nil
}

// deduplicateCandidates removes candidates with identical content hashes.
func deduplicateCandidates(candidates []RankedCandidate) []RankedCandidate {
	seen := make(map[string]bool)
	out := candidates[:0]
	for _, c := range candidates {
		key := c.Memory.ContentHash
		if key == "" {
			key = fmt.Sprintf("id:%s", c.Memory.ID)
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}
