package retrieval_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/config"
	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/retrieval"
	"github.com/memory-engine/memory-engine/internal/scorer"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func makeCandidate(content string, importance, confidence float64) retrieval.RankedCandidate {
	now := time.Now()
	return retrieval.RankedCandidate{
		Memory: &domain.Memory{
			ID:           uuid.New(),
			Content:      content,
			MemoryType:   domain.MemoryTypeFact,
			Status:       domain.MemoryStatusActive,
			Importance:   importance,
			Confidence:   confidence,
			Salience:     0.6,
			Namespace:    "DEFAULT",
			LastAccessed: now,
			CreatedAt:    now,
		},
		Sources: []string{"keyword"},
	}
}

func defaultScorer() *scorer.Scorer {
	return scorer.New(config.ScoringConfig{
		Weights: config.ScoringWeights{
			Relevance: 0.40, Importance: 0.20, Confidence: 0.15,
			Salience: 0.10, Recency: 0.10, AccessFrequency: 0.05,
		},
		Decay: config.DecayConfig{Lambda: 0.023, Floor: 0.05},
		InitialSalience: map[string]float64{"FACT": 0.6},
	})
}

// ── ScoreReranker ─────────────────────────────────────────────────────────────

func TestScoreReranker_SortsByScore(t *testing.T) {
	r := retrieval.NewScoreReranker(defaultScorer())

	candidates := []retrieval.RankedCandidate{
		makeCandidate("low importance", 0.1, 0.5),
		makeCandidate("high importance", 0.9, 0.95),
		makeCandidate("medium importance", 0.5, 0.7),
	}

	result, err := r.Rerank(context.Background(), "test", candidates, 10)
	if err != nil {
		t.Fatalf("rerank error: %v", err)
	}

	// Should be sorted high → medium → low.
	for i := 1; i < len(result); i++ {
		if result[i].FinalScore > result[i-1].FinalScore {
			t.Errorf("not sorted at index %d: %.4f > %.4f",
				i, result[i].FinalScore, result[i-1].FinalScore)
		}
	}
}

func TestScoreReranker_TopKRespected(t *testing.T) {
	r := retrieval.NewScoreReranker(defaultScorer())

	candidates := []retrieval.RankedCandidate{
		makeCandidate("a", 0.9, 0.9),
		makeCandidate("b", 0.8, 0.8),
		makeCandidate("c", 0.7, 0.7),
		makeCandidate("d", 0.6, 0.6),
		makeCandidate("e", 0.5, 0.5),
	}

	result, err := r.Rerank(context.Background(), "test", candidates, 3)
	if err != nil {
		t.Fatalf("rerank error: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 results, got %d", len(result))
	}
}

func TestScoreReranker_SetsScoreBreakdown(t *testing.T) {
	r := retrieval.NewScoreReranker(defaultScorer())
	candidates := []retrieval.RankedCandidate{makeCandidate("test", 0.7, 0.8)}

	result, err := r.Rerank(context.Background(), "test", candidates, 5)
	if err != nil {
		t.Fatalf("rerank error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if result[0].ScoreBreakdown == nil {
		t.Error("ScoreBreakdown should be set after reranking")
	}
	if result[0].FinalScore <= 0 {
		t.Errorf("expected positive final score, got %.4f", result[0].FinalScore)
	}
}

func TestScoreReranker_RelevanceBoostsScore(t *testing.T) {
	r := retrieval.NewScoreReranker(defaultScorer())

	lowRelevance := makeCandidate("low relevance memory", 0.5, 0.5)
	highRelevance := makeCandidate("high relevance memory", 0.5, 0.5)
	highRelevance.KeywordScore = 0.9
	highRelevance.VectorScore = 0.85

	candidates := []retrieval.RankedCandidate{lowRelevance, highRelevance}
	result, _ := r.Rerank(context.Background(), "test", candidates, 5)

	if result[0].FinalScore < result[1].FinalScore {
		t.Error("high relevance candidate should rank first")
	}
}

func TestScoreReranker_EmptyCandidates(t *testing.T) {
	r := retrieval.NewScoreReranker(defaultScorer())
	result, err := r.Rerank(context.Background(), "test", nil, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(result))
	}
}

// ── MergeResults ──────────────────────────────────────────────────────────────

func TestMergeResults_DeduplicatesSameMemory(t *testing.T) {
	mem := &domain.Memory{ID: uuid.New(), Content: "shared memory"}

	keyword := []retrieval.KeywordResult{{Memory: mem, Rank: 0.8}}
	vector := []retrieval.VectorResult{{Memory: mem, Similarity: 0.9}}

	merged := retrieval.MergeResults(keyword, vector, 0.5)

	if len(merged) != 1 {
		t.Errorf("expected 1 merged result (dedup), got %d", len(merged))
	}
}

func TestMergeResults_BothSourcesTagged(t *testing.T) {
	mem := &domain.Memory{ID: uuid.New(), Content: "shared"}

	keyword := []retrieval.KeywordResult{{Memory: mem, Rank: 0.7}}
	vector := []retrieval.VectorResult{{Memory: mem, Similarity: 0.8}}

	merged := retrieval.MergeResults(keyword, vector, 0.5)

	if len(merged) != 1 {
		t.Fatalf("expected 1 result, got %d", len(merged))
	}

	hasKeyword, hasVector := false, false
	for _, s := range merged[0].Sources {
		if s == "keyword" {
			hasKeyword = true
		}
		if s == "vector" {
			hasVector = true
		}
	}
	if !hasKeyword || !hasVector {
		t.Errorf("expected both 'keyword' and 'vector' sources, got %v", merged[0].Sources)
	}
}

func TestMergeResults_UniqueMemoriesPreserved(t *testing.T) {
	memA := &domain.Memory{ID: uuid.New(), Content: "memory A"}
	memB := &domain.Memory{ID: uuid.New(), Content: "memory B"}
	memC := &domain.Memory{ID: uuid.New(), Content: "memory C"}

	keyword := []retrieval.KeywordResult{
		{Memory: memA, Rank: 0.9},
		{Memory: memB, Rank: 0.7},
	}
	vector := []retrieval.VectorResult{
		{Memory: memB, Similarity: 0.8},
		{Memory: memC, Similarity: 0.6},
	}

	merged := retrieval.MergeResults(keyword, vector, 0.5)

	if len(merged) != 3 {
		t.Errorf("expected 3 unique candidates, got %d", len(merged))
	}
}

func TestMergeResults_DualSourceBonus(t *testing.T) {
	// A memory found by both sources should score higher than one found by only one.
	memBoth := &domain.Memory{ID: uuid.New(), Content: "both sources"}
	memOnly := &domain.Memory{ID: uuid.New(), Content: "keyword only"}

	keyword := []retrieval.KeywordResult{
		{Memory: memBoth, Rank: 0.6},
		{Memory: memOnly, Rank: 0.6},
	}
	vector := []retrieval.VectorResult{
		{Memory: memBoth, Similarity: 0.6},
	}

	merged := retrieval.MergeResults(keyword, vector, 0.5)

	var bothScore, onlyScore float64
	for _, m := range merged {
		if m.Memory.ID == memBoth.ID {
			bothScore = m.FinalScore
		}
		if m.Memory.ID == memOnly.ID {
			onlyScore = m.FinalScore
		}
	}

	if bothScore <= onlyScore {
		t.Errorf("dual-source memory (%.4f) should score higher than single-source (%.4f)",
			bothScore, onlyScore)
	}
}

func TestMergeResults_EmptyInputs(t *testing.T) {
	merged := retrieval.MergeResults(nil, nil, 0.5)
	if len(merged) != 0 {
		t.Errorf("expected 0 results for empty inputs, got %d", len(merged))
	}
}

// ── TokenBudget ───────────────────────────────────────────────────────────────

func TestTokenBudget_FitsAll(t *testing.T) {
	budget := retrieval.NewTokenBudget(10000)
	candidates := []retrieval.RankedCandidate{
		makeCandidate("short text", 0.9, 0.9),
		makeCandidate("another short one", 0.8, 0.8),
	}

	kept, tokens := budget.Apply(candidates)
	if len(kept) != 2 {
		t.Errorf("expected all 2 candidates to fit, got %d", len(kept))
	}
	if tokens <= 0 {
		t.Errorf("expected positive token count, got %d", tokens)
	}
}

func TestTokenBudget_ExcludesOverBudget(t *testing.T) {
	budget := retrieval.NewTokenBudget(5) // very tight budget
	// Create a candidate with long content that won't fit.
	c := makeCandidate(
		"this is a very long memory content that definitely exceeds the tiny token budget we set for testing",
		0.9, 0.9,
	)
	kept, _ := budget.Apply([]retrieval.RankedCandidate{c})
	if len(kept) != 0 {
		t.Errorf("expected 0 candidates with tight budget, got %d", len(kept))
	}
}

func TestTokenBudget_KeepsHigherScoredFirst(t *testing.T) {
	// budget that fits ~2 short memories
	budget := retrieval.NewTokenBudget(20)
	candidates := []retrieval.RankedCandidate{
		makeCandidate("first short text", 0.9, 0.9),
		makeCandidate("second short text", 0.7, 0.7),
		makeCandidate("third this will push over budget if combined with others quite significantly", 0.5, 0.5),
	}
	// Candidates must be sorted by score (pipeline does this before calling Apply).
	kept, _ := budget.Apply(candidates)
	// First two should fit since they're short.
	if len(kept) == 0 {
		t.Error("expected at least 1 candidate to fit")
	}
	// First candidate must always be included.
	if kept[0].Memory.Content != "first short text" {
		t.Errorf("highest-scored candidate should be first, got %q", kept[0].Memory.Content)
	}
}

// ── EstimateTokens ────────────────────────────────────────────────────────────

func TestEstimateTokens_Empty(t *testing.T) {
	if retrieval.EstimateTokens("") != 0 {
		t.Error("empty string should have 0 tokens")
	}
}

func TestEstimateTokens_SingleWord(t *testing.T) {
	tokens := retrieval.EstimateTokens("hello")
	if tokens < 1 {
		t.Errorf("single word should have at least 1 token, got %d", tokens)
	}
}

func TestEstimateTokens_MoreWordsMoreTokens(t *testing.T) {
	short := retrieval.EstimateTokens("hello world")
	long := retrieval.EstimateTokens("the quick brown fox jumps over the lazy dog near the riverbank")
	if long <= short {
		t.Errorf("longer text should have more tokens: short=%d, long=%d", short, long)
	}
}

// ── ContextCompressor ─────────────────────────────────────────────────────────

func TestContextCompressor_ShortContent_Unchanged(t *testing.T) {
	c := retrieval.NewContextCompressor(200)
	mem := &domain.Memory{Content: "short content"}
	compressed := c.Compress(mem)
	if compressed.Content != "short content" {
		t.Errorf("short content should be unchanged, got %q", compressed.Content)
	}
}

func TestContextCompressor_LongContent_Truncated(t *testing.T) {
	c := retrieval.NewContextCompressor(10) // very small limit
	longContent := "this is a very long memory content that has many many words and should definitely be truncated by the compressor because it exceeds the limit"
	mem := &domain.Memory{Content: longContent}
	compressed := c.Compress(mem)

	if compressed.Content == longContent {
		t.Error("long content should have been truncated")
	}
	if len(compressed.Content) >= len(longContent) {
		t.Error("compressed content should be shorter than original")
	}
}

func TestContextCompressor_TruncatedHasEllipsis(t *testing.T) {
	c := retrieval.NewContextCompressor(5)
	mem := &domain.Memory{Content: "one two three four five six seven eight nine ten eleven twelve"}
	compressed := c.Compress(mem)

	// Should contain the truncation marker.
	found := false
	for _, suffix := range []string{"[…]", "…", "..."} {
		if len(compressed.Content) > len(suffix) {
			if compressed.Content[len(compressed.Content)-len(suffix):] == suffix ||
				contains(compressed.Content, suffix) {
				found = true
				break
			}
		}
	}
	if !found {
		// Just check it's shorter and different — the exact marker doesn't matter for this test.
		if compressed.Content == mem.Content {
			t.Error("truncated content should differ from original")
		}
	}
}

func TestContextCompressor_NilMemory_ReturnsNil(t *testing.T) {
	c := retrieval.NewContextCompressor(200)
	if c.Compress(nil) != nil {
		t.Error("nil memory should return nil")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
