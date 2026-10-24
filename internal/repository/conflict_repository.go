package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ConflictRepository handles conflict_log and dedup_log persistence.
type ConflictRepository struct {
	db *pgxpool.Pool
}

// NewConflictRepository creates a new ConflictRepository.
func NewConflictRepository(db *pgxpool.Pool) *ConflictRepository {
	return &ConflictRepository{db: db}
}

// LogConflict writes a new entry to conflict_log.
// Uses INSERT ... ON CONFLICT DO UPDATE to handle duplicate detection gracefully.
func (r *ConflictRepository) LogConflict(ctx context.Context, entry domain.ConflictLogEntry) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO conflict_log (
			user_id, memory_a_id, memory_b_id,
			conflict_type, resolution_strategy, status,
			similarity_score, winning_memory_id,
			detection_reason, resolution_reason,
			resolved_by, resolved_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (memory_a_id, memory_b_id, conflict_type)
		DO UPDATE SET
			status             = EXCLUDED.status,
			resolution_strategy = EXCLUDED.resolution_strategy,
			winning_memory_id  = EXCLUDED.winning_memory_id,
			resolution_reason  = EXCLUDED.resolution_reason,
			resolved_by        = EXCLUDED.resolved_by,
			resolved_at        = EXCLUDED.resolved_at
	`,
		entry.UserID, entry.MemoryAID, entry.MemoryBID,
		string(entry.ConflictType), entry.ResolutionStrategy, string(entry.Status),
		entry.SimilarityScore, entry.WinningMemoryID,
		entry.DetectionReason, entry.ResolutionReason,
		entry.ResolvedBy, entry.ResolvedAt,
	)
	return err
}

// LogDedup writes a new entry to dedup_log.
func (r *ConflictRepository) LogDedup(ctx context.Context, entry domain.DedupLogEntry) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO dedup_log (user_id, candidate_id, matched_id, similarity, action, reason)
		VALUES ($1,$2,$3,$4,$5,$6)
	`,
		entry.UserID, entry.CandidateID, entry.MatchedID,
		entry.Similarity, string(entry.Action), entry.Reason,
	)
	return err
}

// ListConflicts returns all conflict log entries for a user.
func (r *ConflictRepository) ListConflicts(
	ctx context.Context,
	userID uuid.UUID,
	statusFilter *domain.ConflictStatus,
	limit int,
) ([]*domain.ConflictLogEntry, error) {
	args := []any{userID, limit}
	whereExtra := ""
	if statusFilter != nil {
		whereExtra = " AND status = $3"
		args = append(args, string(*statusFilter))
	}

	q := fmt.Sprintf(`
		SELECT id, user_id, memory_a_id, memory_b_id,
		       conflict_type, resolution_strategy, status,
		       similarity_score, winning_memory_id,
		       detection_reason, resolution_reason,
		       resolved_by, resolved_at, created_at
		FROM conflict_log
		WHERE user_id = $1%s
		ORDER BY created_at DESC
		LIMIT $2
	`, whereExtra)

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing conflicts: %w", err)
	}
	defer rows.Close()

	var entries []*domain.ConflictLogEntry
	for rows.Next() {
		e := &domain.ConflictLogEntry{}
		var conflictType, status string
		var resStrategy *string

		if err := rows.Scan(
			&e.ID, &e.UserID, &e.MemoryAID, &e.MemoryBID,
			&conflictType, &resStrategy, &status,
			&e.SimilarityScore, &e.WinningMemoryID,
			&e.DetectionReason, &e.ResolutionReason,
			&e.ResolvedBy, &e.ResolvedAt, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning conflict entry: %w", err)
		}

		e.ConflictType = domain.ConflictType(conflictType)
		e.Status = domain.ConflictStatus(status)
		if resStrategy != nil {
			s := domain.ResolutionStrategy(*resStrategy)
			e.ResolutionStrategy = &s
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ResolveConflict updates a conflict_log entry to RESOLVED status.
func (r *ConflictRepository) ResolveConflict(
	ctx context.Context,
	conflictID uuid.UUID,
	winnerID *uuid.UUID,
	strategy domain.ResolutionStrategy,
	reason, resolvedBy string,
) error {
	now := time.Now()
	_, err := r.db.Exec(ctx, `
		UPDATE conflict_log SET
			status             = 'RESOLVED',
			resolution_strategy = $2,
			winning_memory_id  = $3,
			resolution_reason  = $4,
			resolved_by        = $5,
			resolved_at        = $6
		WHERE id = $1 AND status = 'DETECTED'
	`,
		conflictID, string(strategy), winnerID, reason, resolvedBy, now,
	)
	return err
}

// GetUnresolvedCount returns the number of unresolved conflicts for a user.
func (r *ConflictRepository) GetUnresolvedCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conflict_log WHERE user_id = $1 AND status = 'DETECTED'`,
		userID,
	).Scan(&count)
	return count, err
}
