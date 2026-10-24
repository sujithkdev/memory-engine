package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/conflict"
	"github.com/memory-engine/memory-engine/internal/dedup"
	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

// ConflictService orchestrates deduplication, conflict detection, and resolution.
type ConflictService struct {
	deduplicator *dedup.Deduplicator
	detector     *conflict.Detector
	conflicts    *repository.ConflictRepository
	memories     *repository.MemoryRepository
	logger       zerolog.Logger
}

// NewConflictService creates a new ConflictService.
func NewConflictService(
	memories *repository.MemoryRepository,
	conflicts *repository.ConflictRepository,
	logger zerolog.Logger,
) *ConflictService {
	return &ConflictService{
		deduplicator: dedup.New(memories, conflicts),
		detector:     conflict.New(memories, conflicts),
		conflicts:    conflicts,
		memories:     memories,
		logger:       logger,
	}
}

// ProcessNew runs dedup + conflict detection on a newly created memory.
// Must be called AFTER the memory has been inserted (we have its ID).
// Returns the final memory to use (may be different if dedup merged/dropped).
func (s *ConflictService) ProcessNew(ctx context.Context, mem *domain.Memory) (*domain.Memory, error) {
	// ── Step 1: Content hash ──────────────────────────────────────────────────
	hash := dedup.HashContent(mem.Content)
	if err := s.memories.SetContentHash(ctx, mem.ID, hash); err != nil {
		s.logger.Warn().Err(err).Str("memory_id", mem.ID.String()).Msg("failed to set content hash")
	}
	mem.ContentHash = hash

	// ── Step 2: Deduplication ─────────────────────────────────────────────────
	dedupResult, err := s.deduplicator.Check(ctx, mem)
	if err != nil {
		s.logger.Warn().Err(err).Msg("dedup check failed — skipping")
	} else {
		switch dedupResult.Action {

		case domain.DedupDropped, domain.DedupMerged:
			s.logger.Info().
				Str("action", string(dedupResult.Action)).
				Str("matched", dedupResult.MatchedMemoryID.String()).
				Float64("similarity", dedupResult.Similarity).
				Msg("dedup: dropping candidate")

			// Discard both return values — best-effort soft-delete.
			_, _ = s.memories.TransitionStatus(ctx, mem.ID, mem.UserID,
				domain.MemoryStatusDeleted, "system", dedupResult.Reason,
			)
			_ = s.conflicts.LogDedup(ctx, domain.DedupLogEntry{
				UserID:      mem.UserID,
				CandidateID: mem.ID,
				MatchedID:   *dedupResult.MatchedMemoryID,
				Similarity:  dedupResult.Similarity,
				Action:      dedupResult.Action,
				Reason:      &dedupResult.Reason,
			})
			return dedupResult.FinalMemory, nil

		case domain.DedupUpdatedConfidence:
			existing := dedupResult.FinalMemory
			newConf := dedup.BoostConfidence(existing.Confidence)
			updated, updateErr := s.memories.Update(ctx, existing.ID, existing.UserID, domain.UpdateMemoryInput{
				Confidence: &newConf,
			})
			if updateErr != nil {
				s.logger.Warn().Err(updateErr).Msg("failed to boost confidence on dedup")
			}

			_, _ = s.memories.TransitionStatus(ctx, mem.ID, mem.UserID,
				domain.MemoryStatusDeleted, "system", dedupResult.Reason,
			)
			_ = s.conflicts.LogDedup(ctx, domain.DedupLogEntry{
				UserID:      mem.UserID,
				CandidateID: mem.ID,
				MatchedID:   *dedupResult.MatchedMemoryID,
				Similarity:  dedupResult.Similarity,
				Action:      dedupResult.Action,
				Reason:      &dedupResult.Reason,
			})

			if updated != nil {
				return updated, nil
			}
			return existing, nil
		}
	}

	// ── Step 3: Conflict detection (ACTIVE memories only) ─────────────────────
	if mem.Status != domain.MemoryStatusActive {
		return mem, nil
	}

	conflictResult, err := s.detector.Check(ctx, mem)
	if err != nil {
		s.logger.Warn().Err(err).Msg("conflict check failed — skipping")
		return mem, nil
	}

	if !conflictResult.HasConflicts {
		return mem, nil
	}

	s.logger.Info().
		Int("conflicts", len(conflictResult.Conflicts)).
		Str("memory_id", mem.ID.String()).
		Msg("conflicts detected — auto-resolving")

	finalMem := mem
	for _, c := range conflictResult.Conflicts {
		resolved, resolveErr := s.detector.Resolve(ctx, c, finalMem, "system")
		if resolveErr != nil {
			s.logger.Warn().Err(resolveErr).Msg("conflict resolution failed")
			continue
		}
		finalMem = resolved
	}

	return finalMem, nil
}

// ListConflicts returns conflict log entries for a user.
func (s *ConflictService) ListConflicts(
	ctx context.Context,
	userID uuid.UUID,
	statusFilter *domain.ConflictStatus,
) ([]*domain.ConflictLogEntry, error) {
	return s.conflicts.ListConflicts(ctx, userID, statusFilter, 100)
}

// ResolveConflict manually resolves a detected conflict.
func (s *ConflictService) ResolveConflict(
	ctx context.Context,
	conflictID uuid.UUID,
	winnerID *uuid.UUID,
	strategy domain.ResolutionStrategy,
	reason, resolvedBy string,
) error {
	return s.conflicts.ResolveConflict(ctx, conflictID, winnerID, strategy, reason, resolvedBy)
}

// GetUnresolvedCount returns the number of unresolved conflicts for a user.
func (s *ConflictService) GetUnresolvedCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.conflicts.GetUnresolvedCount(ctx, userID)
}

// ── Relationship Service ──────────────────────────────────────────────────────

// RelationshipService manages directed edges between memories.
type RelationshipService struct {
	memories  *repository.MemoryRepository
	relations *repository.RelationshipRepository
}

// NewRelationshipService creates a new RelationshipService.
func NewRelationshipService(
	memories *repository.MemoryRepository,
	relations *repository.RelationshipRepository,
) *RelationshipService {
	return &RelationshipService{memories: memories, relations: relations}
}

// Link creates a directed relationship between two memories.
func (s *RelationshipService) Link(
	ctx context.Context,
	userID, sourceID, targetID uuid.UUID,
	relType domain.RelationshipType,
	confidence float64,
) (*domain.MemoryRelationship, error) {
	for _, id := range []uuid.UUID{sourceID, targetID} {
		belongs, err := s.memories.BelongsToUser(ctx, id, userID)
		if err != nil {
			return nil, fmt.Errorf("checking ownership of %s: %w", id, err)
		}
		if !belongs {
			return nil, domain.NotFound("memory", id.String())
		}
	}
	if sourceID == targetID {
		return nil, domain.Validation(map[string]string{
			"target_memory_id": "cannot link a memory to itself",
		})
	}
	return s.relations.Create(ctx, sourceID, targetID, relType, confidence)
}

// GetRelationships returns all relationships for a memory.
func (s *RelationshipService) GetRelationships(
	ctx context.Context,
	memoryID, userID uuid.UUID,
) ([]*domain.MemoryRelationship, error) {
	belongs, err := s.memories.BelongsToUser(ctx, memoryID, userID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, domain.NotFound("memory", memoryID.String())
	}
	return s.relations.GetForMemory(ctx, memoryID)
}

// PropagateConfidence caps confidence on all DERIVED_FROM children when source confidence drops.
func (s *RelationshipService) PropagateConfidence(
	ctx context.Context,
	sourceID uuid.UUID,
	newSourceConfidence float64,
) error {
	derived, err := s.relations.GetDerivedMemories(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("getting derived memories: %w", err)
	}
	for _, rel := range derived {
		mem, err := s.memories.GetByIDAdmin(ctx, rel.TargetMemoryID)
		if err != nil {
			continue
		}
		if mem.Confidence > newSourceConfidence {
			capped := conflict.PropagateUncertainty(newSourceConfidence, mem.Confidence)
			_, _ = s.memories.Update(ctx, mem.ID, mem.UserID, domain.UpdateMemoryInput{
				Confidence: &capped,
			})
		}
	}
	return nil
}
