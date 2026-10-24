// Package scorer implements the memory scoring engine.
//
// Final score formula:
//
//	score = relevance×0.40 + importance×0.20 + confidence×0.15
//	      + salience×0.10  + recency×0.10    + access_frequency×0.05
//
// All weights are configurable via config.yaml without redeployment.
// Scores are always normalised to [0, 1].
package scorer

import (
	"math"
	"sort"
	"time"

	"github.com/memory-engine/memory-engine/internal/config"
	"github.com/memory-engine/memory-engine/internal/domain"
)

// Scorer computes final scores for memories given a query context.
type Scorer struct {
	weights         config.ScoringWeights
	decay           config.DecayConfig
	initialSalience map[string]float64
}

// New creates a new Scorer from the application scoring config.
func New(cfg config.ScoringConfig) *Scorer {
	return &Scorer{
		weights:         cfg.Weights,
		decay:           cfg.Decay,
		initialSalience: cfg.InitialSalience,
	}
}

// ── Scored Memory ─────────────────────────────────────────────────────────────

// ScoredMemory pairs a memory with its computed score breakdown.
type ScoredMemory struct {
	Memory          *domain.Memory `json:"memory"`
	FinalScore      float64        `json:"final_score"`
	ScoreBreakdown  ScoreBreakdown `json:"score_breakdown"`
}

// ScoreBreakdown shows the contribution of each signal.
// Useful for debugging retrieval quality and tuning weights.
type ScoreBreakdown struct {
	Relevance       float64 `json:"relevance"`        // from query similarity (0 if no query)
	Importance      float64 `json:"importance"`        // raw importance field
	Confidence      float64 `json:"confidence"`        // raw confidence field
	Salience        float64 `json:"salience"`          // current decayed salience
	Recency         float64 `json:"recency"`           // time-decayed recency score
	AccessFrequency float64 `json:"access_frequency"`  // normalised access count
	WeightedTotal   float64 `json:"weighted_total"`    // = final_score
}

// ── Score a single memory ──────────────────────────────────────────────────────

// ScoreOptions carries optional context for scoring.
type ScoreOptions struct {
	// Relevance is the semantic similarity score from vector search [0,1].
	// Pass 0 if no query (e.g., listing all memories).
	Relevance float64
	// MaxAccessCount is the highest access_count in the candidate set,
	// used to normalise access_frequency across the set.
	MaxAccessCount int
	// Now is the reference time for recency calculations.
	// Defaults to time.Now() if zero.
	Now time.Time
}

// Score computes the final weighted score for one memory.
func (s *Scorer) Score(mem *domain.Memory, opts ScoreOptions) ScoredMemory {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.MaxAccessCount < 1 {
		opts.MaxAccessCount = 1
	}

	breakdown := ScoreBreakdown{
		Relevance:       clamp(opts.Relevance),
		Importance:      clamp(mem.Importance),
		Confidence:      clamp(mem.Confidence),
		Salience:        clamp(s.currentSalience(mem, opts.Now)),
		Recency:         s.recencyScore(mem, opts.Now),
		AccessFrequency: s.accessFrequencyScore(mem, opts.MaxAccessCount),
	}

	breakdown.WeightedTotal = s.weights.Relevance*breakdown.Relevance +
		s.weights.Importance*breakdown.Importance +
		s.weights.Confidence*breakdown.Confidence +
		s.weights.Salience*breakdown.Salience +
		s.weights.Recency*breakdown.Recency +
		s.weights.AccessFrequency*breakdown.AccessFrequency

	return ScoredMemory{
		Memory:         mem,
		FinalScore:     clamp(breakdown.WeightedTotal),
		ScoreBreakdown: breakdown,
	}
}

// ScoreAndSort scores a slice of memories and returns them sorted
// by final score descending. Relevance scores are passed in parallel.
func (s *Scorer) ScoreAndSort(memories []*domain.Memory, relevanceScores []float64, now time.Time) []ScoredMemory {
	if now.IsZero() {
		now = time.Now()
	}

	// Find max access count for normalisation.
	maxAccess := 1
	for _, m := range memories {
		if m.AccessCount > maxAccess {
			maxAccess = m.AccessCount
		}
	}

	scored := make([]ScoredMemory, len(memories))
	for i, mem := range memories {
		rel := 0.0
		if i < len(relevanceScores) {
			rel = relevanceScores[i]
		}
		scored[i] = s.Score(mem, ScoreOptions{
			Relevance:      rel,
			MaxAccessCount: maxAccess,
			Now:            now,
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].FinalScore > scored[j].FinalScore
	})

	return scored
}

// ── Salience Decay ────────────────────────────────────────────────────────────

// currentSalience returns the time-decayed salience for a memory.
//
// Formula: s(t) = max(floor, s0 × e^(-λ × days_since_access))
//
// Where:
//   s0 = stored salience value
//   λ  = decay.lambda (configurable)
//   floor = minimum salience (configurable)
func (s *Scorer) currentSalience(mem *domain.Memory, now time.Time) float64 {
	daysSince := now.Sub(mem.LastAccessed).Hours() / 24.0
	if daysSince < 0 {
		daysSince = 0
	}
	decayed := mem.Salience * math.Exp(-s.decay.Lambda*daysSince)
	return math.Max(s.decay.Floor, decayed)
}

// DecaySalience computes the new salience value to persist for a memory.
// Called by the background worker (Phase 9) or on-demand.
func (s *Scorer) DecaySalience(mem *domain.Memory, now time.Time) float64 {
	return s.currentSalience(mem, now)
}

// InitialSalience returns the starting salience for a memory type.
// Used when a memory is first created.
func (s *Scorer) InitialSalience(memType domain.MemoryType) float64 {
	if v, ok := s.initialSalience[string(memType)]; ok {
		return v
	}
	return 0.5 // safe default
}

// ── Recency score ─────────────────────────────────────────────────────────────

// recencyScore converts memory age into a [0,1] score using exponential decay.
// Very recent memories score near 1.0; very old memories approach 0.
//
// Half-life ≈ 7 days (lambda = 0.099).
func (s *Scorer) recencyScore(mem *domain.Memory, now time.Time) float64 {
	reference := mem.LastAccessed
	if mem.CreatedAt.After(reference) {
		reference = mem.CreatedAt
	}
	daysSince := now.Sub(reference).Hours() / 24.0
	if daysSince < 0 {
		daysSince = 0
	}
	// Recency uses a faster decay than salience (half-life ≈ 7 days).
	return math.Exp(-0.099 * daysSince)
}

// ── Access frequency score ────────────────────────────────────────────────────

// accessFrequencyScore normalises access_count against the set maximum.
// Returns [0,1]: 0 = never accessed, 1 = most accessed in the set.
func (s *Scorer) accessFrequencyScore(mem *domain.Memory, maxAccess int) float64 {
	if maxAccess <= 0 {
		return 0
	}
	return clamp(float64(mem.AccessCount) / float64(maxAccess))
}

// ── Temporal validity ──────────────────────────────────────────────────────────

// IsTemporallyValid returns true if the memory is valid at the given time.
func IsTemporallyValid(mem *domain.Memory, at time.Time) bool {
	if mem.ValidFrom != nil && at.Before(*mem.ValidFrom) {
		return false
	}
	if mem.ValidUntil != nil && at.After(*mem.ValidUntil) {
		return false
	}
	return true
}

// ── Importance classification ──────────────────────────────────────────────────

// ImportanceLevel classifies a numeric importance score into a human-readable tier.
type ImportanceLevel string

const (
	ImportanceCritical ImportanceLevel = "CRITICAL" // 0.9 – 1.0
	ImportanceHigh     ImportanceLevel = "HIGH"     // 0.7 – 0.9
	ImportanceMedium   ImportanceLevel = "MEDIUM"   // 0.4 – 0.7
	ImportanceLow      ImportanceLevel = "LOW"      // 0.1 – 0.4
	ImportanceTrivial  ImportanceLevel = "TRIVIAL"  // 0.0 – 0.1
)

// ClassifyImportance maps a [0,1] importance score to a tier.
func ClassifyImportance(importance float64) ImportanceLevel {
	switch {
	case importance >= 0.9:
		return ImportanceCritical
	case importance >= 0.7:
		return ImportanceHigh
	case importance >= 0.4:
		return ImportanceMedium
	case importance >= 0.1:
		return ImportanceLow
	default:
		return ImportanceTrivial
	}
}

// ── Confidence tiers ──────────────────────────────────────────────────────────

// ConfidenceLevel classifies a confidence score.
type ConfidenceLevel string

const (
	ConfidenceVerified   ConfidenceLevel = "VERIFIED"   // 0.9 – 1.0
	ConfidenceHigh       ConfidenceLevel = "HIGH"       // 0.7 – 0.9
	ConfidenceMedium     ConfidenceLevel = "MEDIUM"     // 0.5 – 0.7
	ConfidenceLow        ConfidenceLevel = "LOW"        // 0.3 – 0.5
	ConfidenceUncertain  ConfidenceLevel = "UNCERTAIN"  // 0.0 – 0.3
)

// ClassifyConfidence maps a [0,1] confidence score to a tier.
func ClassifyConfidence(confidence float64) ConfidenceLevel {
	switch {
	case confidence >= 0.9:
		return ConfidenceVerified
	case confidence >= 0.7:
		return ConfidenceHigh
	case confidence >= 0.5:
		return ConfidenceMedium
	case confidence >= 0.3:
		return ConfidenceLow
	default:
		return ConfidenceUncertain
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// clamp constrains a value to [0, 1].
func clamp(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// Weights returns the active scoring weight configuration.
func (s *Scorer) Weights() config.ScoringWeights {
	return s.weights
}
