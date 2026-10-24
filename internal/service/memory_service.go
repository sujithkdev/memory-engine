package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/config"
	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
	"github.com/memory-engine/memory-engine/internal/scorer"
)

// MemoryService handles all memory business logic.
// This is the core of Phase 3+4: lifecycle management, explicit remember/forget,
// content versioning, audit trail, and scoring engine.
type MemoryService struct {
	memories *repository.MemoryRepository
	scorer   *scorer.Scorer
}

// NewMemoryService creates a new MemoryService.
func NewMemoryService(memories *repository.MemoryRepository, scoringCfg config.ScoringConfig) *MemoryService {
	return &MemoryService{
		memories: memories,
		scorer:   scorer.New(scoringCfg),
	}
}

// Create creates a new memory with full validation.
// The initial status depends on source_type and explicit flag.
func (s *MemoryService) Create(ctx context.Context, input domain.CreateMemoryInput) (*domain.Memory, error) {
	// Normalize.
	input.Content = strings.TrimSpace(input.Content)
	input.Namespace = strings.ToUpper(strings.TrimSpace(input.Namespace))
	if input.Namespace == "" {
		input.Namespace = "DEFAULT"
	}

	if errs := validateCreateMemory(input); len(errs) > 0 {
		return nil, domain.Validation(errs)
	}

	return s.memories.Create(ctx, input)
}

// Remember handles the explicit "remember this" command.
// Always creates an ACTIVE memory — skips CANDIDATE/VALIDATED stages.
// This is the fast path for user-initiated explicit memories.
func (s *MemoryService) Remember(ctx context.Context, input domain.RememberInput) (*domain.Memory, error) {
	input.Content = strings.TrimSpace(input.Content)
	input.Namespace = strings.ToUpper(strings.TrimSpace(input.Namespace))
	if input.Namespace == "" {
		input.Namespace = "DEFAULT"
	}

	if errs := validateRemember(input); len(errs) > 0 {
		return nil, domain.Validation(errs)
	}

	createInput := domain.CreateMemoryInput{
		UserID:        input.UserID,
		Content:       input.Content,
		MemoryType:    input.MemoryType,
		Importance:    input.Importance,
		Namespace:     input.Namespace,
		SourceType:    domain.SourceTypeExplicit,
		SourceID:      input.SourceID,
		Explicit:      true, // → ACTIVE immediately
		ValidUntil:    input.ValidUntil,
		RetentionDays: input.RetentionDays,
		PrivacyLevel:  input.PrivacyLevel,
	}

	return s.memories.Create(ctx, createInput)
}

// Forget handles the explicit "forget this" command.
// Transitions memory to DELETED status and records the reason in the audit log.
func (s *MemoryService) Forget(ctx context.Context, id uuid.UUID, input domain.ForgetInput) error {
	reason := input.Reason
	if reason == "" {
		reason = "explicit forget requested by user"
	}

	_, err := s.memories.TransitionStatus(ctx, id, input.UserID,
		domain.MemoryStatusDeleted, "user", reason,
	)
	return err
}

// GetByID fetches a single memory.
func (s *MemoryService) GetByID(ctx context.Context, id, userID uuid.UUID) (*domain.Memory, error) {
	mem, err := s.memories.GetByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	// Touch access counter — async, best-effort.
	go s.memories.TouchAccess(context.Background(), id)

	return mem, nil
}

// List returns a paginated, filtered list of memories.
func (s *MemoryService) List(ctx context.Context, filter domain.MemoryFilter) (*domain.MemoryList, error) {
	filter = normalizeMemoryFilter(filter)
	return s.memories.List(ctx, filter)
}

// Update applies partial changes to a memory.
// Content changes trigger versioning (new memory + old marked UPDATED).
// Metadata changes update in place with audit entries.
func (s *MemoryService) Update(ctx context.Context, id, userID uuid.UUID, input domain.UpdateMemoryInput) (*domain.Memory, error) {
	if errs := validateUpdateMemory(input); len(errs) > 0 {
		return nil, domain.Validation(errs)
	}

	if input.Content != nil {
		trimmed := strings.TrimSpace(*input.Content)
		input.Content = &trimmed
	}
	if input.Namespace != nil {
		ns := strings.ToUpper(strings.TrimSpace(*input.Namespace))
		input.Namespace = &ns
	}

	return s.memories.Update(ctx, id, userID, input)
}

// Archive transitions a memory to ARCHIVED status.
func (s *MemoryService) Archive(ctx context.Context, id, userID uuid.UUID, reason string) (*domain.Memory, error) {
	if reason == "" {
		reason = "archived by user"
	}
	return s.memories.TransitionStatus(ctx, id, userID,
		domain.MemoryStatusArchived, "user", reason,
	)
}

// Restore transitions an ARCHIVED or STALE memory back to ACTIVE.
func (s *MemoryService) Restore(ctx context.Context, id, userID uuid.UUID) (*domain.Memory, error) {
	return s.memories.TransitionStatus(ctx, id, userID,
		domain.MemoryStatusActive, "user", "restored by user",
	)
}

// Activate transitions a VALIDATED or NEW memory to ACTIVE.
func (s *MemoryService) Activate(ctx context.Context, id, userID uuid.UUID) (*domain.Memory, error) {
	return s.memories.TransitionStatus(ctx, id, userID,
		domain.MemoryStatusActive, "user", "activated by user",
	)
}

// GetAuditLog returns the full audit trail for a memory.
func (s *MemoryService) GetAuditLog(ctx context.Context, memoryID, userID uuid.UUID) ([]*domain.MemoryAuditEntry, error) {
	return s.memories.GetAuditLog(ctx, memoryID, userID)
}

// Search returns memories matching a text query (simple content filter for Phase 3;
// replaced by full-text search in Phase 6 and vector search in Phase 7).
func (s *MemoryService) Search(ctx context.Context, userID uuid.UUID, query string, filter domain.MemoryFilter) (*domain.MemoryList, error) {
	filter.UserID = userID
	filter = normalizeMemoryFilter(filter)
	// Phase 3: return all active memories for the user/namespace, client filters.
	// Phase 6 will upgrade this to FTS. Phase 8 to hybrid retrieval.
	activeStatus := domain.MemoryStatusActive
	filter.Status = &activeStatus
	return s.memories.List(ctx, filter)
}

// ForgetByNamespace soft-deletes all memories in a namespace.
// Implements the "forget everything about X" command.
func (s *MemoryService) ForgetByNamespace(ctx context.Context, userID uuid.UUID, namespace, reason string) (int64, error) {
	if namespace == "" {
		return 0, domain.Validation(map[string]string{"namespace": "namespace is required"})
	}
	if reason == "" {
		reason = fmt.Sprintf("bulk forget: namespace %s", namespace)
	}

	ns := strings.ToUpper(strings.TrimSpace(namespace))
	count, err := s.memories.ForgetByNamespace(ctx, userID, ns, reason)
	if err != nil {
		return 0, fmt.Errorf("forget by namespace: %w", err)
	}
	return count, nil
}

// ── Validators ────────────────────────────────────────────────────────────────

func validateCreateMemory(input domain.CreateMemoryInput) map[string]string {
	errs := map[string]string{}

	if input.UserID == uuid.Nil {
		errs["user_id"] = "user_id is required"
	}
	if input.Content == "" {
		errs["content"] = "content is required"
	} else if len(input.Content) > 100_000 {
		errs["content"] = "content must not exceed 100,000 characters"
	}
	if !domain.ValidMemoryTypes[input.MemoryType] {
		errs["memory_type"] = fmt.Sprintf("invalid memory_type: %s", input.MemoryType)
	}
	if input.Importance < 0 || input.Importance > 1 {
		errs["importance"] = "importance must be between 0 and 1"
	}
	if input.Confidence < 0 || input.Confidence > 1 {
		errs["confidence"] = "confidence must be between 0 and 1"
	}

	return errs
}

func validateRemember(input domain.RememberInput) map[string]string {
	errs := map[string]string{}

	if input.UserID == uuid.Nil {
		errs["user_id"] = "user_id is required"
	}
	if input.Content == "" {
		errs["content"] = "content is required"
	} else if len(input.Content) > 100_000 {
		errs["content"] = "content must not exceed 100,000 characters"
	}
	if input.MemoryType != "" && !domain.ValidMemoryTypes[input.MemoryType] {
		errs["memory_type"] = fmt.Sprintf("invalid memory_type: %s", input.MemoryType)
	}
	if input.Importance < 0 || input.Importance > 1 {
		errs["importance"] = "importance must be between 0 and 1"
	}

	return errs
}

func validateUpdateMemory(input domain.UpdateMemoryInput) map[string]string {
	errs := map[string]string{}

	if input.Content != nil && strings.TrimSpace(*input.Content) == "" {
		errs["content"] = "content cannot be empty"
	}
	if input.Importance != nil && (*input.Importance < 0 || *input.Importance > 1) {
		errs["importance"] = "importance must be between 0 and 1"
	}
	if input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1) {
		errs["confidence"] = "confidence must be between 0 and 1"
	}

	return errs
}

func normalizeMemoryFilter(f domain.MemoryFilter) domain.MemoryFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 20
	}
	return f
}

// ── Phase 4: Scoring methods ──────────────────────────────────────────────────

// ScoreMemories scores and ranks a list of memories using the scoring engine.
// relevanceScores is parallel to memories — pass nil or empty for non-query contexts.
func (s *MemoryService) ScoreMemories(
	memories []*domain.Memory,
	relevanceScores []float64,
) []domain.ScoredMemory {
	now := time.Now()
	scoredList := s.scorer.ScoreAndSort(memories, relevanceScores, now)

	result := make([]domain.ScoredMemory, len(scoredList))
	for i, sm := range scoredList {
		result[i] = domain.ScoredMemory{
			Memory:     sm.Memory,
			FinalScore: sm.FinalScore,
			ScoreBreakdown: domain.ScoreBreakdown{
				Relevance:       sm.ScoreBreakdown.Relevance,
				Importance:      sm.ScoreBreakdown.Importance,
				Confidence:      sm.ScoreBreakdown.Confidence,
				Salience:        sm.ScoreBreakdown.Salience,
				Recency:         sm.ScoreBreakdown.Recency,
				AccessFrequency: sm.ScoreBreakdown.AccessFrequency,
				WeightedTotal:   sm.ScoreBreakdown.WeightedTotal,
			},
		}
	}
	return result
}

// ScoreAndList retrieves, scores, and ranks memories for a user.
// This is the Phase 4 version of List — returns scored results with breakdown.
func (s *MemoryService) ScoreAndList(ctx context.Context, filter domain.MemoryFilter) (*domain.ScoredMemoryList, error) {
	filter = normalizeMemoryFilter(filter)

	// Default to ACTIVE memories for scoring.
	if filter.Status == nil {
		active := domain.MemoryStatusActive
		filter.Status = &active
	}

	list, err := s.memories.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	scored := s.ScoreMemories(list.Memories, nil)

	return &domain.ScoredMemoryList{
		Memories: scored,
		Total:    len(scored),
		ScoringWeights: domain.ScoringWeights{
			Relevance:       s.scorer.Weights().Relevance,
			Importance:      s.scorer.Weights().Importance,
			Confidence:      s.scorer.Weights().Confidence,
			Salience:        s.scorer.Weights().Salience,
			Recency:         s.scorer.Weights().Recency,
			AccessFrequency: s.scorer.Weights().AccessFrequency,
		},
	}, nil
}

// ApplyDecay computes and persists decayed salience for one memory.
// Called by the background worker or on-demand.
func (s *MemoryService) ApplyDecay(ctx context.Context, mem *domain.Memory) error {
	newSalience := s.scorer.DecaySalience(mem, time.Now())
	return s.memories.UpdateScoredSalience(ctx, mem.ID, newSalience)
}

// GetScoringWeights returns the active scoring configuration as a public struct.
func (s *MemoryService) GetScoringWeights() domain.ScoringWeights {
	w := s.scorer.Weights()
	return domain.ScoringWeights{
		Relevance:       w.Relevance,
		Importance:      w.Importance,
		Confidence:      w.Confidence,
		Salience:        w.Salience,
		Recency:         w.Recency,
		AccessFrequency: w.AccessFrequency,
	}
}

// ClassifyMemory returns scoring metadata for a single memory.
func (s *MemoryService) ClassifyMemory(mem *domain.Memory) map[string]interface{} {
	now := time.Now()
	sm := s.scorer.Score(mem, scorer.ScoreOptions{Now: now})
	return map[string]interface{}{
		"final_score":       sm.FinalScore,
		"score_breakdown":   sm.ScoreBreakdown,
		"importance_level":  scorer.ClassifyImportance(mem.Importance),
		"confidence_level":  scorer.ClassifyConfidence(mem.Confidence),
		"is_temporally_valid": scorer.IsTemporallyValid(mem, now),
		"decayed_salience":  s.scorer.DecaySalience(mem, now),
	}
}
