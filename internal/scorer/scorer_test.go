package scorer_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/config"
	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/scorer"
)

// ── helpers ────────────────────────────────────────────────────────────────────

func defaultScorer() *scorer.Scorer {
	return scorer.New(config.ScoringConfig{
		Weights: config.ScoringWeights{
			Relevance:       0.40,
			Importance:      0.20,
			Confidence:      0.15,
			Salience:        0.10,
			Recency:         0.10,
			AccessFrequency: 0.05,
		},
		Decay: config.DecayConfig{
			Lambda: 0.023,
			Floor:  0.05,
		},
		InitialSalience: map[string]float64{
			"FACT":       0.6,
			"PREFERENCE": 0.7,
			"WORKING":    0.9,
		},
	})
}

func makeMemory(importance, confidence, salience float64, memType domain.MemoryType) *domain.Memory {
	now := time.Now()
	return &domain.Memory{
		ID:           uuid.New(),
		UserID:       uuid.New(),
		Content:      "test memory",
		MemoryType:   memType,
		Status:       domain.MemoryStatusActive,
		Importance:   importance,
		Confidence:   confidence,
		Salience:     salience,
		Namespace:    "DEFAULT",
		LastAccessed: now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// ── Score correctness ──────────────────────────────────────────────────────────

func TestScore_AllSignalsZero_ReturnsZero(t *testing.T) {
	s := defaultScorer()
	mem := makeMemory(0, 0, 0, domain.MemoryTypeFact)
	// Zero salience means decay also returns floor (0.05), so score won't be exactly 0.
	// With all zero inputs except salience floor:
	// score = 0*0.40 + 0*0.20 + 0*0.15 + floor*0.10 + recency*0.10 + 0*0.05
	result := s.Score(mem, scorer.ScoreOptions{
		Relevance:      0,
		MaxAccessCount: 1,
		Now:            time.Now(),
	})
	if result.FinalScore < 0 || result.FinalScore > 1 {
		t.Errorf("score out of [0,1] range: %.4f", result.FinalScore)
	}
}

func TestScore_AllSignalsMax_ReturnsOne(t *testing.T) {
	s := defaultScorer()
	mem := makeMemory(1.0, 1.0, 1.0, domain.MemoryTypeFact)
	result := s.Score(mem, scorer.ScoreOptions{
		Relevance:      1.0,
		MaxAccessCount: 1,
		Now:            time.Now(),
	})
	// With all signals at max, score should be close to 1.0.
	// Recency will be ~1.0 (just created) and access_freq = 1/1 = 1.0.
	if result.FinalScore < 0.95 {
		t.Errorf("expected score near 1.0 for all-max inputs, got %.4f", result.FinalScore)
	}
}

func TestScore_WeightedFormula(t *testing.T) {
	s := defaultScorer()
	now := time.Now()
	mem := makeMemory(0.8, 0.9, 0.7, domain.MemoryTypeFact)
	mem.AccessCount = 5

	result := s.Score(mem, scorer.ScoreOptions{
		Relevance:      0.6,
		MaxAccessCount: 10,
		Now:            now,
	})

	breakdown := result.ScoreBreakdown

	// Manually verify the formula.
	expected := 0.40*breakdown.Relevance +
		0.20*breakdown.Importance +
		0.15*breakdown.Confidence +
		0.10*breakdown.Salience +
		0.10*breakdown.Recency +
		0.05*breakdown.AccessFrequency

	if math.Abs(expected-result.FinalScore) > 0.001 {
		t.Errorf("formula mismatch: expected %.4f, got %.4f", expected, result.FinalScore)
	}
}

func TestScore_ScoreInRange(t *testing.T) {
	s := defaultScorer()
	cases := []struct {
		importance, confidence, salience float64
		relevance                        float64
	}{
		{0.5, 0.5, 0.5, 0.5},
		{0.0, 0.0, 0.0, 0.0},
		{1.0, 1.0, 1.0, 1.0},
		{0.3, 0.9, 0.1, 0.7},
		{0.99, 0.01, 0.5, 0.0},
	}

	for _, c := range cases {
		mem := makeMemory(c.importance, c.confidence, c.salience, domain.MemoryTypeFact)
		result := s.Score(mem, scorer.ScoreOptions{Relevance: c.relevance})
		if result.FinalScore < 0 || result.FinalScore > 1 {
			t.Errorf("score %.4f out of [0,1] for inputs %+v", result.FinalScore, c)
		}
	}
}

// ── Salience decay ─────────────────────────────────────────────────────────────

func TestDecay_FreshMemory_NoSignificantDecay(t *testing.T) {
	s := defaultScorer()
	mem := makeMemory(0.5, 0.8, 0.7, domain.MemoryTypeFact)
	mem.LastAccessed = time.Now()

	decayed := s.DecaySalience(mem, time.Now())

	// Just-accessed memory should retain most of its salience.
	if decayed < 0.65 {
		t.Errorf("fresh memory decayed too much: %.4f", decayed)
	}
}

func TestDecay_OldMemory_SignificantDecay(t *testing.T) {
	s := defaultScorer()
	mem := makeMemory(0.5, 0.8, 0.7, domain.MemoryTypeFact)
	// Last accessed 90 days ago.
	mem.LastAccessed = time.Now().AddDate(0, 0, -90)

	decayed := s.DecaySalience(mem, time.Now())

	// 90 days at λ=0.023: s = 0.7 * e^(-0.023*90) ≈ 0.7 * 0.127 ≈ 0.089
	// Floor is 0.05, so it should be between floor and original.
	if decayed >= 0.7 {
		t.Errorf("old memory should have decayed, got %.4f", decayed)
	}
	if decayed < 0.05 {
		t.Errorf("decayed below floor: %.4f", decayed)
	}
}

func TestDecay_NeverBelowFloor(t *testing.T) {
	s := defaultScorer()
	mem := makeMemory(0.5, 0.8, 0.1, domain.MemoryTypeFact)
	// Last accessed 1000 days ago.
	mem.LastAccessed = time.Now().AddDate(0, 0, -1000)

	decayed := s.DecaySalience(mem, time.Now())

	if decayed < 0.05 {
		t.Errorf("salience went below floor: %.4f", decayed)
	}
}

func TestDecay_HalfLife(t *testing.T) {
	// λ=0.023 → half-life = ln(2)/0.023 ≈ 30.1 days
	s := defaultScorer()
	initialSalience := 0.8
	mem := makeMemory(0.5, 0.8, initialSalience, domain.MemoryTypeFact)
	mem.LastAccessed = time.Now().AddDate(0, 0, -30)

	decayed := s.DecaySalience(mem, time.Now())
	expected := initialSalience * math.Exp(-0.023*30) // ≈ 0.4

	if math.Abs(decayed-expected) > 0.02 {
		t.Errorf("half-life decay: expected %.4f, got %.4f", expected, decayed)
	}
}

// ── Recency score ──────────────────────────────────────────────────────────────

func TestScore_RecentMemory_HighRecency(t *testing.T) {
	s := defaultScorer()
	mem := makeMemory(0.5, 0.5, 0.5, domain.MemoryTypeFact)
	mem.LastAccessed = time.Now()
	mem.CreatedAt = time.Now()

	result := s.Score(mem, scorer.ScoreOptions{Now: time.Now()})
	// Recency score should be close to 1.0 for brand-new memory.
	if result.ScoreBreakdown.Recency < 0.95 {
		t.Errorf("brand new memory should have high recency, got %.4f", result.ScoreBreakdown.Recency)
	}
}

func TestScore_OldMemory_LowRecency(t *testing.T) {
	s := defaultScorer()
	mem := makeMemory(0.5, 0.5, 0.5, domain.MemoryTypeFact)
	mem.LastAccessed = time.Now().AddDate(-1, 0, 0) // 1 year ago
	mem.CreatedAt = mem.LastAccessed

	result := s.Score(mem, scorer.ScoreOptions{Now: time.Now()})
	if result.ScoreBreakdown.Recency > 0.05 {
		t.Errorf("1-year-old memory should have low recency, got %.4f", result.ScoreBreakdown.Recency)
	}
}

// ── ScoreAndSort ──────────────────────────────────────────────────────────────

func TestScoreAndSort_OrderedByScore(t *testing.T) {
	s := defaultScorer()
	now := time.Now()

	high := makeMemory(0.9, 0.95, 0.9, domain.MemoryTypeFact)
	high.LastAccessed = now

	low := makeMemory(0.1, 0.2, 0.1, domain.MemoryTypeFact)
	low.LastAccessed = now.AddDate(-1, 0, 0)

	mid := makeMemory(0.5, 0.5, 0.5, domain.MemoryTypeFact)
	mid.LastAccessed = now.AddDate(0, -1, 0)

	memories := []*domain.Memory{low, mid, high}
	scored := s.ScoreAndSort(memories, nil, now)

	if len(scored) != 3 {
		t.Fatalf("expected 3 scored memories, got %d", len(scored))
	}

	// Should be sorted high → mid → low.
	if scored[0].FinalScore < scored[1].FinalScore {
		t.Errorf("first score %.4f < second score %.4f", scored[0].FinalScore, scored[1].FinalScore)
	}
	if scored[1].FinalScore < scored[2].FinalScore {
		t.Errorf("second score %.4f < third score %.4f", scored[1].FinalScore, scored[2].FinalScore)
	}
}

func TestScoreAndSort_WithRelevance(t *testing.T) {
	s := defaultScorer()
	now := time.Now()

	// Two equal memories.
	a := makeMemory(0.5, 0.5, 0.5, domain.MemoryTypeFact)
	a.LastAccessed = now
	b := makeMemory(0.5, 0.5, 0.5, domain.MemoryTypeFact)
	b.LastAccessed = now

	// a has high relevance, b has zero.
	relevance := []float64{0.9, 0.0}
	scored := s.ScoreAndSort([]*domain.Memory{a, b}, relevance, now)

	if scored[0].ScoreBreakdown.Relevance < scored[1].ScoreBreakdown.Relevance {
		t.Error("higher relevance memory should rank first")
	}
}

// ── Access frequency normalisation ────────────────────────────────────────────

func TestScore_AccessFrequency_Normalised(t *testing.T) {
	s := defaultScorer()
	mem := makeMemory(0.5, 0.5, 0.5, domain.MemoryTypeFact)
	mem.AccessCount = 50

	result := s.Score(mem, scorer.ScoreOptions{
		MaxAccessCount: 100,
		Now:            time.Now(),
	})

	// 50/100 = 0.5
	if math.Abs(result.ScoreBreakdown.AccessFrequency-0.5) > 0.001 {
		t.Errorf("expected access_frequency 0.5, got %.4f", result.ScoreBreakdown.AccessFrequency)
	}
}

// ── Classification ────────────────────────────────────────────────────────────

func TestClassifyImportance(t *testing.T) {
	cases := []struct {
		score    float64
		expected scorer.ImportanceLevel
	}{
		{1.0, scorer.ImportanceCritical},
		{0.9, scorer.ImportanceCritical},
		{0.85, scorer.ImportanceHigh},
		{0.7, scorer.ImportanceHigh},
		{0.5, scorer.ImportanceMedium},
		{0.4, scorer.ImportanceMedium},
		{0.2, scorer.ImportanceLow},
		{0.1, scorer.ImportanceLow},
		{0.05, scorer.ImportanceTrivial},
		{0.0, scorer.ImportanceTrivial},
	}

	for _, c := range cases {
		got := scorer.ClassifyImportance(c.score)
		if got != c.expected {
			t.Errorf("ClassifyImportance(%.2f): expected %s, got %s", c.score, c.expected, got)
		}
	}
}

func TestClassifyConfidence(t *testing.T) {
	cases := []struct {
		score    float64
		expected scorer.ConfidenceLevel
	}{
		{1.0, scorer.ConfidenceVerified},
		{0.9, scorer.ConfidenceVerified},
		{0.8, scorer.ConfidenceHigh},
		{0.7, scorer.ConfidenceHigh},
		{0.6, scorer.ConfidenceMedium},
		{0.5, scorer.ConfidenceMedium},
		{0.4, scorer.ConfidenceLow},
		{0.3, scorer.ConfidenceLow},
		{0.2, scorer.ConfidenceUncertain},
		{0.0, scorer.ConfidenceUncertain},
	}

	for _, c := range cases {
		got := scorer.ClassifyConfidence(c.score)
		if got != c.expected {
			t.Errorf("ClassifyConfidence(%.2f): expected %s, got %s", c.score, c.expected, got)
		}
	}
}

// ── InitialSalience ───────────────────────────────────────────────────────────

func TestInitialSalience_KnownType(t *testing.T) {
	s := defaultScorer()

	if got := s.InitialSalience(domain.MemoryTypeWorking); math.Abs(got-0.9) > 0.001 {
		t.Errorf("WORKING initial salience: expected 0.9, got %.4f", got)
	}
	if got := s.InitialSalience(domain.MemoryTypePreference); math.Abs(got-0.7) > 0.001 {
		t.Errorf("PREFERENCE initial salience: expected 0.7, got %.4f", got)
	}
}

func TestInitialSalience_UnknownType_ReturnsDefault(t *testing.T) {
	s := defaultScorer()
	// RELATIONSHIP is not in the test config — should return 0.5 default.
	got := s.InitialSalience(domain.MemoryTypeRelationship)
	if math.Abs(got-0.5) > 0.001 {
		t.Errorf("unknown type initial salience: expected 0.5, got %.4f", got)
	}
}

// ── Temporal validity ──────────────────────────────────────────────────────────

func TestIsTemporallyValid(t *testing.T) {
	now := time.Now()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	cases := []struct {
		name      string
		validFrom *time.Time
		validUntil *time.Time
		checkAt   time.Time
		expected  bool
	}{
		{"no bounds", nil, nil, now, true},
		{"valid_from in past", &past, nil, now, true},
		{"valid_from in future", &future, nil, now, false},
		{"valid_until in future", nil, &future, now, true},
		{"valid_until in past", nil, &past, now, false},
		{"within bounds", &past, &future, now, true},
	}

	for _, c := range cases {
		mem := makeMemory(0.5, 0.5, 0.5, domain.MemoryTypeFact)
		mem.ValidFrom = c.validFrom
		mem.ValidUntil = c.validUntil

		got := scorer.IsTemporallyValid(mem, c.checkAt)
		if got != c.expected {
			t.Errorf("%s: expected %v, got %v", c.name, c.expected, got)
		}
	}
}

// ── Weights ───────────────────────────────────────────────────────────────────

func TestWeights_SumToOne(t *testing.T) {
	s := defaultScorer()
	w := s.Weights()
	sum := w.Relevance + w.Importance + w.Confidence + w.Salience + w.Recency + w.AccessFrequency

	if math.Abs(sum-1.0) > 0.001 {
		t.Errorf("weights should sum to 1.0, got %.4f", sum)
	}
}

func TestWeights_Returned(t *testing.T) {
	s := defaultScorer()
	w := s.Weights()

	if w.Relevance != 0.40 {
		t.Errorf("expected relevance 0.40, got %.2f", w.Relevance)
	}
	if w.Importance != 0.20 {
		t.Errorf("expected importance 0.20, got %.2f", w.Importance)
	}
}
