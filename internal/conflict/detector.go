// Package conflict implements memory conflict detection and resolution.
//
// Three conflict strategies (as designed):
//
//  1. Direct contradiction  — "I use Python" vs "I use Go"
//     → Keep newer, archive older, set valid_until on archived
//
//  2. Temporal update       — "I live in NYC" → "I live in London"
//     → Treat as versioned update: new memory supersedes old
//
//  3. Partial overlap       — "I like coffee" vs "I stopped drinking coffee"
//     → Keep both, mark CONTRADICTS relationship, decay confidence on older
package conflict

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/memory-engine/memory-engine/internal/dedup"
	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

const (
	// ContradictionThreshold — similarity above this with opposite sentiment signals contradiction.
	ContradictionThreshold = 0.60
	// TemporalKeywords — presence of these in content suggests a temporal update.
	// Used as a heuristic before Phase 11 LLM-based detection.
)

var temporalKeywords = []string{
	"now", "currently", "today", "recently", "switched", "moved",
	"changed", "no longer", "instead", "replaced", "updated", "new",
	"started", "stopped", "quit", "began",
}

var contradictionPairs = [][]string{
	{"prefer", "don't prefer"}, {"prefer", "hate"},
	{"like", "dislike"}, {"like", "don't like"},
	{"use", "don't use"}, {"use", "stopped using"},
	{"love", "hate"}, {"always", "never"},
	{"will", "won't"}, {"can", "cannot"},
}

// Detector checks a candidate memory against existing ones for conflicts.
type Detector struct {
	memories  *repository.MemoryRepository
	conflicts *repository.ConflictRepository
}

// New creates a new conflict Detector.
func New(memories *repository.MemoryRepository, conflicts *repository.ConflictRepository) *Detector {
	return &Detector{memories: memories, conflicts: conflicts}
}

// Check examines a candidate memory for conflicts with existing ACTIVE memories.
// Returns a ConflictCheckResult with all detected conflicts.
func (d *Detector) Check(ctx context.Context, candidate *domain.Memory) (*domain.ConflictCheckResult, error) {
	// Only check memory types that can logically conflict.
	if !isConflictableType(candidate.MemoryType) {
		return &domain.ConflictCheckResult{HasConflicts: false}, nil
	}

	// Fetch existing active memories of the same type and namespace.
	existing, err := d.memories.ListActiveByTypeAndNamespace(
		ctx, candidate.UserID, candidate.MemoryType, candidate.Namespace, 100,
	)
	if err != nil {
		return nil, fmt.Errorf("fetching memories for conflict check: %w", err)
	}

	var detected []domain.DetectedConflict

	for _, mem := range existing {
		if mem.ID == candidate.ID {
			continue
		}

		conflict := d.detectConflict(candidate, mem)
		if conflict != nil {
			detected = append(detected, *conflict)
		}
	}

	return &domain.ConflictCheckResult{
		HasConflicts: len(detected) > 0,
		Conflicts:    detected,
	}, nil
}

// detectConflict checks a pair of memories for conflict.
// Returns nil if no conflict found.
func (d *Detector) detectConflict(candidate, existing *domain.Memory) *domain.DetectedConflict {
	candLower := strings.ToLower(candidate.Content)
	existLower := strings.ToLower(existing.Content)

	sim := dedup.CosineSimilarityPlaceholder(candidate.Content, existing.Content)

	// ── 1. Temporal update detection ──────────────────────────────────────────
	if hasTemporalKeyword(candLower) && sim >= 0.40 {
		return &domain.DetectedConflict{
			ExistingMemory:    existing,
			ConflictType:      domain.ConflictTemporalUpdate,
			SimilarityScore:   sim,
			SuggestedStrategy: domain.ResolutionKeepNewer,
			DetectionReason: fmt.Sprintf(
				"temporal keyword detected in candidate with %.2f similarity to existing memory",
				sim,
			),
		}
	}

	// ── 2. Direct contradiction detection ─────────────────────────────────────
	if sim >= ContradictionThreshold && isContradiction(candLower, existLower) {
		return &domain.DetectedConflict{
			ExistingMemory:    existing,
			ConflictType:      domain.ConflictDirectContradiction,
			SimilarityScore:   sim,
			SuggestedStrategy: domain.ResolutionKeepNewer,
			DetectionReason: fmt.Sprintf(
				"direct contradiction detected (%.2f similarity, opposite sentiment)",
				sim,
			),
		}
	}

	// ── 3. Partial overlap detection ─────────────────────────────────────────
	if sim >= 0.50 && sim < ContradictionThreshold {
		return &domain.DetectedConflict{
			ExistingMemory:    existing,
			ConflictType:      domain.ConflictPartialOverlap,
			SimilarityScore:   sim,
			SuggestedStrategy: domain.ResolutionKeepBoth,
			DetectionReason: fmt.Sprintf(
				"partial overlap detected (%.2f similarity) — keeping both",
				sim,
			),
		}
	}

	return nil
}

// Resolve applies the resolution strategy for a detected conflict.
// Returns the winning memory and logs the resolution.
func (d *Detector) Resolve(
	ctx context.Context,
	conflict domain.DetectedConflict,
	candidate *domain.Memory,
	resolvedBy string,
) (*domain.Memory, error) {
	switch conflict.SuggestedStrategy {
	case domain.ResolutionKeepNewer:
		return d.resolveKeepNewer(ctx, candidate, conflict.ExistingMemory, conflict, resolvedBy)

	case domain.ResolutionKeepHigherConf:
		return d.resolveKeepHigherConf(ctx, candidate, conflict.ExistingMemory, conflict, resolvedBy)

	case domain.ResolutionKeepBoth:
		// Log the relationship and return candidate (both survive).
		if err := d.conflicts.LogConflict(ctx, domain.ConflictLogEntry{
			UserID:             candidate.UserID,
			MemoryAID:          candidate.ID,
			MemoryBID:          conflict.ExistingMemory.ID,
			ConflictType:       conflict.ConflictType,
			Status:             domain.ConflictDetected,
			SimilarityScore:    &conflict.SimilarityScore,
			DetectionReason:    &conflict.DetectionReason,
			ResolutionStrategy: resStratPtr(domain.ResolutionKeepBoth),
			ResolvedBy:         &resolvedBy,
		}); err != nil {
			return nil, fmt.Errorf("logging keep-both conflict: %w", err)
		}
		return candidate, nil

	default:
		return candidate, nil
	}
}

// resolveKeepNewer archives the existing memory and returns the candidate.
func (d *Detector) resolveKeepNewer(
	ctx context.Context,
	candidate, existing *domain.Memory,
	conflict domain.DetectedConflict,
	resolvedBy string,
) (*domain.Memory, error) {
	// Set valid_until on existing to now (temporal end).
	now := time.Now()
	if err := d.memories.SetValidUntil(ctx, existing.ID, now); err != nil {
		return nil, fmt.Errorf("setting valid_until on existing: %w", err)
	}

	// Transition existing to ARCHIVED.
	reason := fmt.Sprintf("superseded by memory %s (%s)", candidate.ID, conflict.DetectionReason)
	if _, err := d.memories.TransitionStatus(
		ctx, existing.ID, existing.UserID,
		domain.MemoryStatusArchived, resolvedBy, reason,
	); err != nil {
		return nil, fmt.Errorf("archiving existing memory: %w", err)
	}

	// Log the resolution.
	winnerID := candidate.ID
	resolutionReason := "newer memory kept, existing archived with valid_until set"
	strategy := domain.ResolutionKeepNewer
	if err := d.conflicts.LogConflict(ctx, domain.ConflictLogEntry{
		UserID:             candidate.UserID,
		MemoryAID:          candidate.ID,
		MemoryBID:          existing.ID,
		ConflictType:       conflict.ConflictType,
		Status:             domain.ConflictResolved,
		SimilarityScore:    &conflict.SimilarityScore,
		WinningMemoryID:    &winnerID,
		DetectionReason:    &conflict.DetectionReason,
		ResolutionReason:   &resolutionReason,
		ResolutionStrategy: resStratPtr(strategy),
		ResolvedBy:         &resolvedBy,
	}); err != nil {
		return nil, fmt.Errorf("logging resolution: %w", err)
	}

	return candidate, nil
}

// resolveKeepHigherConf keeps the memory with higher confidence.
func (d *Detector) resolveKeepHigherConf(
	ctx context.Context,
	candidate, existing *domain.Memory,
	conflict domain.DetectedConflict,
	resolvedBy string,
) (*domain.Memory, error) {
	var winner, loser *domain.Memory
	if candidate.Confidence >= existing.Confidence {
		winner, loser = candidate, existing
	} else {
		winner, loser = existing, candidate
	}

	// Archive the loser.
	reason := fmt.Sprintf("lower confidence (%.2f vs %.2f) — archived", loser.Confidence, winner.Confidence)
	if _, err := d.memories.TransitionStatus(
		ctx, loser.ID, loser.UserID,
		domain.MemoryStatusArchived, resolvedBy, reason,
	); err != nil {
		return nil, fmt.Errorf("archiving lower-confidence memory: %w", err)
	}

	winnerID := winner.ID
	strategy := domain.ResolutionKeepHigherConf
	resReason := fmt.Sprintf("kept memory with higher confidence (%.2f)", winner.Confidence)
	if err := d.conflicts.LogConflict(ctx, domain.ConflictLogEntry{
		UserID:             candidate.UserID,
		MemoryAID:          candidate.ID,
		MemoryBID:          existing.ID,
		ConflictType:       conflict.ConflictType,
		Status:             domain.ConflictResolved,
		SimilarityScore:    &conflict.SimilarityScore,
		WinningMemoryID:    &winnerID,
		ResolutionStrategy: resStratPtr(strategy),
		ResolutionReason:   &resReason,
		ResolvedBy:         &resolvedBy,
	}); err != nil {
		return nil, fmt.Errorf("logging resolution: %w", err)
	}

	return winner, nil
}

// ── Uncertainty propagation ───────────────────────────────────────────────────

// PropagateUncertainty reduces the confidence of a memory based on
// the confidence of its source memory.
//
// Rule: a derived memory's confidence cannot exceed its source's confidence.
// If source confidence < derived confidence → cap the derived memory.
func PropagateUncertainty(sourceConfidence, derivedConfidence float64) float64 {
	if derivedConfidence > sourceConfidence {
		return sourceConfidence
	}
	return derivedConfidence
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func isConflictableType(mt domain.MemoryType) bool {
	switch mt {
	case domain.MemoryTypePreference, domain.MemoryTypeFact,
		domain.MemoryTypeGoal, domain.MemoryTypeDecision,
		domain.MemoryTypeInstruction, domain.MemoryTypeRelationship:
		return true
	default:
		return false
	}
}

func hasTemporalKeyword(content string) bool {
	for _, kw := range temporalKeywords {
		if strings.Contains(content, kw) {
			return true
		}
	}
	return false
}

func isContradiction(a, b string) bool {
	for _, pair := range contradictionPairs {
		posA := strings.Contains(a, pair[0]) && strings.Contains(b, pair[1])
		posB := strings.Contains(b, pair[0]) && strings.Contains(a, pair[1])
		if posA || posB {
			return true
		}
	}
	return false
}

func strPtr(s string) *string { return &s }

func resStratPtr(s domain.ResolutionStrategy) *domain.ResolutionStrategy { return &s }
