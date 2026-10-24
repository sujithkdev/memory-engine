// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"sort"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/scorer"
)

// Reranker re-scores a candidate set and returns the top-K results.
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []RankedCandidate, topK int) ([]RankedCandidate, error)
}

// RankedCandidate is a memory with its hybrid scores during the pipeline.
type RankedCandidate struct {
	Memory          *domain.Memory
	KeywordScore    float64  // ts_rank_cd from FTS
	VectorScore     float64  // cosine similarity from ANN
	FinalScore      float64  // weighted score after reranking
	ScoreBreakdown  *domain.ScoreBreakdown
	Sources         []string // "keyword", "vector"
}

// ── Score-based reranker (no external model needed) ──────────────────────────

// ScoreReranker reranks using the scoring engine (Phase 4).
// Used when no LLM reranker is available (default).
// Combines keyword rank, vector similarity, and memory scoring signals.
type ScoreReranker struct {
	scorer *scorer.Scorer
}

// NewScoreReranker creates a ScoreReranker.
func NewScoreReranker(s *scorer.Scorer) *ScoreReranker {
	return &ScoreReranker{scorer: s}
}

// Rerank applies the weighted scoring formula and returns top-K candidates.
func (r *ScoreReranker) Rerank(_ context.Context, _ string, candidates []RankedCandidate, topK int) ([]RankedCandidate, error) {
	// Find max access count for normalisation.
	maxAccess := 1
	for _, c := range candidates {
		if c.Memory.AccessCount > maxAccess {
			maxAccess = c.Memory.AccessCount
		}
	}

	// Score each candidate combining FTS rank, vector similarity, and memory signals.
	for i := range candidates {
		c := &candidates[i]

		// Relevance = max of keyword and vector scores (best signal wins).
		relevance := c.KeywordScore
		if c.VectorScore > relevance {
			relevance = c.VectorScore
		}

		sm := r.scorer.Score(c.Memory, scorer.ScoreOptions{
			Relevance:      relevance,
			MaxAccessCount: maxAccess,
		})

		c.FinalScore = sm.FinalScore
		c.ScoreBreakdown = &domain.ScoreBreakdown{
			Relevance:       sm.ScoreBreakdown.Relevance,
			Importance:      sm.ScoreBreakdown.Importance,
			Confidence:      sm.ScoreBreakdown.Confidence,
			Salience:        sm.ScoreBreakdown.Salience,
			Recency:         sm.ScoreBreakdown.Recency,
			AccessFrequency: sm.ScoreBreakdown.AccessFrequency,
			WeightedTotal:   sm.ScoreBreakdown.WeightedTotal,
		}
	}

	// Sort descending by final score.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].FinalScore > candidates[j].FinalScore
	})

	if topK > 0 && len(candidates) > topK {
		return candidates[:topK], nil
	}
	return candidates, nil
}

// ── Null reranker (pass-through) ─────────────────────────────────────────────

// NullReranker returns candidates unchanged (for testing/benchmarking).
type NullReranker struct{}

func (r *NullReranker) Rerank(_ context.Context, _ string, candidates []RankedCandidate, topK int) ([]RankedCandidate, error) {
	if topK > 0 && len(candidates) > topK {
		return candidates[:topK], nil
	}
	return candidates, nil
}
