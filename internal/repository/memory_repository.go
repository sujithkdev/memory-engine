package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// MemoryRepository handles all memory persistence.
type MemoryRepository struct {
	db *pgxpool.Pool
}

// NewMemoryRepository creates a new MemoryRepository.
func NewMemoryRepository(db *pgxpool.Pool) *MemoryRepository {
	return &MemoryRepository{db: db}
}

// memoryScanFields returns the standard column list and scanner for a memory row.
const memorySelectCols = `
	id, user_id, content, memory_type, status,
	importance, confidence, salience,
	valid_from, valid_until,
	source_type, source_id,
	namespace, memory_level,
	privacy_level, retention_days, delete_after,
	version, parent_id,
	last_accessed, access_count,
	importance_level, scored_at,
	content_hash,
	created_at, updated_at
`

func scanMemory(row pgx.Row) (*domain.Memory, error) {
	m := &domain.Memory{}
	err := row.Scan(
		&m.ID, &m.UserID, &m.Content, &m.MemoryType, &m.Status,
		&m.Importance, &m.Confidence, &m.Salience,
		&m.ValidFrom, &m.ValidUntil,
		&m.SourceType, &m.SourceID,
		&m.Namespace, &m.MemoryLevel,
		&m.PrivacyLevel, &m.RetentionDays, &m.DeleteAfter,
		&m.Version, &m.ParentID,
		&m.LastAccessed, &m.AccessCount,
		&m.ImportanceLevel, &m.ScoredAt,
		&m.ContentHash,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func scanMemoryRow(rows pgx.Rows) (*domain.Memory, error) {
	m := &domain.Memory{}
	err := rows.Scan(
		&m.ID, &m.UserID, &m.Content, &m.MemoryType, &m.Status,
		&m.Importance, &m.Confidence, &m.Salience,
		&m.ValidFrom, &m.ValidUntil,
		&m.SourceType, &m.SourceID,
		&m.Namespace, &m.MemoryLevel,
		&m.PrivacyLevel, &m.RetentionDays, &m.DeleteAfter,
		&m.Version, &m.ParentID,
		&m.LastAccessed, &m.AccessCount,
		&m.ImportanceLevel, &m.ScoredAt,
		&m.ContentHash,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Create inserts a new memory and writes an audit log entry in a single transaction.
func (r *MemoryRepository) Create(ctx context.Context, input domain.CreateMemoryInput) (*domain.Memory, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Determine initial status.
	status := domain.MemoryStatusNew
	if input.Explicit {
		status = domain.MemoryStatusActive // "remember this" skips CANDIDATE
	}

	// Set defaults.
	if input.Importance == 0 {
		input.Importance = 0.5
	}
	if input.Confidence == 0 {
		input.Confidence = 0.8
	}
	if input.Namespace == "" {
		input.Namespace = "DEFAULT"
	}
	if input.PrivacyLevel == "" {
		input.PrivacyLevel = domain.PrivacyPrivate
	}
	if input.MemoryLevel == "" {
		input.MemoryLevel = domain.MemoryLevelRaw
	}
	if input.SourceType == "" {
		input.SourceType = domain.SourceTypeExplicit
	}

	const q = `
		INSERT INTO memories (
			user_id, content, memory_type, status,
			importance, confidence, salience,
			valid_until, source_type, source_id,
			namespace, memory_level, privacy_level,
			retention_days, delete_after
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, 0.5,
			$7, $8, $9,
			$10, $11, $12,
			$13, $14
		)
		RETURNING ` + memorySelectCols

	row := tx.QueryRow(ctx, q,
		input.UserID, input.Content, string(input.MemoryType), string(status),
		input.Importance, input.Confidence,
		input.ValidUntil, string(input.SourceType), input.SourceID,
		input.Namespace, string(input.MemoryLevel), string(input.PrivacyLevel),
		input.RetentionDays, computeDeleteAfter(input.RetentionDays),
	)

	mem, err := scanMemory(row)
	if err != nil {
		return nil, fmt.Errorf("inserting memory: %w", err)
	}

	// Write audit log entry.
	if err := insertAuditLog(ctx, tx, domain.MemoryAuditEntry{
		MemoryID:  mem.ID,
		UserID:    mem.UserID,
		Action:    domain.AuditCreated,
		NewStatus: &mem.Status,
		ChangedBy: "user",
	}); err != nil {
		return nil, fmt.Errorf("writing audit log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return mem, nil
}

// GetByID fetches a single memory by ID, checking user ownership.
func (r *MemoryRepository) GetByID(ctx context.Context, id, userID uuid.UUID) (*domain.Memory, error) {
	q := `SELECT ` + memorySelectCols + ` FROM memories WHERE id = $1 AND user_id = $2`

	mem, err := scanMemory(r.db.QueryRow(ctx, q, id, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("memory", id.String())
		}
		return nil, fmt.Errorf("getting memory %s: %w", id, err)
	}
	return mem, nil
}

// List returns paginated memories with optional filters.
// Always filters by user_id. Excludes DELETED by default.
func (r *MemoryRepository) List(ctx context.Context, filter domain.MemoryFilter) (*domain.MemoryList, error) {
	args := []any{filter.UserID}
	argIdx := 2
	wheres := []string{"user_id = $1", "status != 'DELETED'"}

	if filter.Status != nil {
		wheres = append(wheres, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, string(*filter.Status))
		argIdx++
	}
	if filter.MemoryType != nil {
		wheres = append(wheres, fmt.Sprintf("memory_type = $%d", argIdx))
		args = append(args, string(*filter.MemoryType))
		argIdx++
	}
	if filter.Namespace != nil {
		wheres = append(wheres, fmt.Sprintf("namespace = $%d", argIdx))
		args = append(args, *filter.Namespace)
		argIdx++
	}
	if filter.MemoryLevel != nil {
		wheres = append(wheres, fmt.Sprintf("memory_level = $%d", argIdx))
		args = append(args, string(*filter.MemoryLevel))
		argIdx++
	}
	if filter.ValidAt != nil {
		wheres = append(wheres, fmt.Sprintf(
			"(valid_from IS NULL OR valid_from <= $%d) AND (valid_until IS NULL OR valid_until >= $%d)",
			argIdx, argIdx,
		))
		args = append(args, *filter.ValidAt)
		argIdx++
	}

	whereSQL := "WHERE " + strings.Join(wheres, " AND ")

	// Count.
	var total int64
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM memories "+whereSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting memories: %w", err)
	}

	// Data.
	offset := (filter.Page - 1) * filter.PageSize
	dataArgs := append(args, filter.PageSize, offset)
	dataQ := fmt.Sprintf(
		`SELECT %s FROM memories %s ORDER BY importance DESC, created_at DESC LIMIT $%d OFFSET $%d`,
		memorySelectCols, whereSQL, argIdx, argIdx+1,
	)

	rows, err := r.db.Query(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("listing memories: %w", err)
	}
	defer rows.Close()

	mems := make([]*domain.Memory, 0)
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning memory: %w", err)
		}
		mems = append(mems, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating memories: %w", err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(filter.PageSize)))
	if totalPages == 0 {
		totalPages = 1
	}

	return &domain.MemoryList{
		Memories:   mems,
		Total:      total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
		TotalPages: totalPages,
	}, nil
}

// Update applies content/metadata changes to a memory and writes an audit entry.
// Content changes create a new version (the old memory is marked UPDATED).
func (r *MemoryRepository) Update(ctx context.Context, id, userID uuid.UUID, input domain.UpdateMemoryInput) (*domain.Memory, error) {
	// Fetch current state for audit diff and version bump.
	current, err := r.GetByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var updated *domain.Memory

	if input.Content != nil && *input.Content != current.Content {
		// Content change = create new version, archive old one.
		updated, err = r.createNewVersion(ctx, tx, current, input)
	} else {
		// Metadata-only change = update in place.
		updated, err = r.updateInPlace(ctx, tx, current, input)
	}
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing update: %w", err)
	}

	return updated, nil
}

// createNewVersion marks the current memory as UPDATED and inserts a new version.
func (r *MemoryRepository) createNewVersion(
	ctx context.Context,
	tx pgx.Tx,
	current *domain.Memory,
	input domain.UpdateMemoryInput,
) (*domain.Memory, error) {
	// Mark old version as UPDATED.
	oldStatus := current.Status
	updatedStatus := domain.MemoryStatusUpdated
	_, err := tx.Exec(ctx,
		`UPDATE memories SET status = 'UPDATED', updated_at = NOW() WHERE id = $1`, current.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("marking old version: %w", err)
	}

	// Audit the status change on old version.
	if err := insertAuditLog(ctx, tx, domain.MemoryAuditEntry{
		MemoryID:   current.ID,
		UserID:     current.UserID,
		Action:     domain.AuditStatusChanged,
		OldStatus:  &oldStatus,
		NewStatus:  &updatedStatus,
		ChangedBy:  "user",
		Reason:     strPtr("content updated — new version created"),
	}); err != nil {
		return nil, err
	}

	// Insert new version.
	importance := current.Importance
	if input.Importance != nil {
		importance = *input.Importance
	}
	confidence := current.Confidence
	if input.Confidence != nil {
		confidence = *input.Confidence
	}
	namespace := current.Namespace
	if input.Namespace != nil {
		namespace = *input.Namespace
	}
	privacy := current.PrivacyLevel
	if input.Privacy != nil {
		privacy = *input.Privacy
	}

	activeStatus := domain.MemoryStatusActive
	const q = `
		INSERT INTO memories (
			user_id, content, memory_type, status,
			importance, confidence, salience,
			valid_until, source_type, source_id,
			namespace, memory_level, privacy_level,
			version, parent_id
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9, $10,
			$11, $12, $13,
			$14, $15
		)
		RETURNING ` + memorySelectCols

	row := tx.QueryRow(ctx, q,
		current.UserID, *input.Content, string(current.MemoryType), string(activeStatus),
		importance, confidence, current.Salience,
		input.ValidUntil, string(current.SourceType), current.SourceID,
		namespace, string(current.MemoryLevel), string(privacy),
		current.Version+1, current.ID,
	)

	newMem, err := scanMemory(row)
	if err != nil {
		return nil, fmt.Errorf("inserting new version: %w", err)
	}

	// Audit the new version creation.
	if err := insertAuditLog(ctx, tx, domain.MemoryAuditEntry{
		MemoryID:   newMem.ID,
		UserID:     newMem.UserID,
		Action:     domain.AuditContentUpdated,
		OldContent: &current.Content,
		NewContent: input.Content,
		NewStatus:  &activeStatus,
		ChangedBy:  "user",
		Reason:     strPtr(fmt.Sprintf("version %d created from %s", newMem.Version, current.ID)),
	}); err != nil {
		return nil, err
	}

	return newMem, nil
}

// updateInPlace updates metadata fields without versioning.
func (r *MemoryRepository) updateInPlace(
	ctx context.Context,
	tx pgx.Tx,
	current *domain.Memory,
	input domain.UpdateMemoryInput,
) (*domain.Memory, error) {
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	var audits []domain.MemoryAuditEntry

	if input.Importance != nil && *input.Importance != current.Importance {
		setClauses = append(setClauses, fmt.Sprintf("importance = $%d", argIdx))
		args = append(args, *input.Importance)
		argIdx++
		field := "importance"
		audits = append(audits, domain.MemoryAuditEntry{
			MemoryID:     current.ID,
			UserID:       current.UserID,
			Action:       domain.AuditImportanceUpdated,
			OldValue:     &current.Importance,
			NewValue:     input.Importance,
			FieldChanged: &field,
			ChangedBy:    "user",
		})
	}
	if input.Confidence != nil && *input.Confidence != current.Confidence {
		setClauses = append(setClauses, fmt.Sprintf("confidence = $%d", argIdx))
		args = append(args, *input.Confidence)
		argIdx++
		field := "confidence"
		audits = append(audits, domain.MemoryAuditEntry{
			MemoryID:     current.ID,
			UserID:       current.UserID,
			Action:       domain.AuditConfidenceUpdated,
			OldValue:     &current.Confidence,
			NewValue:     input.Confidence,
			FieldChanged: &field,
			ChangedBy:    "user",
		})
	}
	if input.Namespace != nil {
		setClauses = append(setClauses, fmt.Sprintf("namespace = $%d", argIdx))
		args = append(args, *input.Namespace)
		argIdx++
	}
	if input.ValidUntil != nil {
		setClauses = append(setClauses, fmt.Sprintf("valid_until = $%d", argIdx))
		args = append(args, *input.ValidUntil)
		argIdx++
	}
	if input.Privacy != nil {
		setClauses = append(setClauses, fmt.Sprintf("privacy_level = $%d", argIdx))
		args = append(args, string(*input.Privacy))
		argIdx++
	}

	if len(setClauses) == 1 {
		// Nothing changed — return current state.
		row := tx.QueryRow(ctx, `SELECT `+memorySelectCols+` FROM memories WHERE id = $1`, current.ID)
		return scanMemory(row)
	}

	args = append(args, current.ID)
	q := fmt.Sprintf(
		`UPDATE memories SET %s WHERE id = $%d RETURNING %s`,
		strings.Join(setClauses, ", "), argIdx, memorySelectCols,
	)

	updated, err := scanMemory(tx.QueryRow(ctx, q, args...))
	if err != nil {
		return nil, fmt.Errorf("updating memory metadata: %w", err)
	}

	// Write all audit entries.
	for _, a := range audits {
		if err := insertAuditLog(ctx, tx, a); err != nil {
			return nil, err
		}
	}

	return updated, nil
}

// TransitionStatus moves a memory through the lifecycle state machine.
// Validates the transition is allowed before applying it.
func (r *MemoryRepository) TransitionStatus(
	ctx context.Context,
	id, userID uuid.UUID,
	newStatus domain.MemoryStatus,
	changedBy, reason string,
) (*domain.Memory, error) {
	current, err := r.GetByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	if !current.Status.CanTransitionTo(newStatus) {
		return nil, domain.Validation(map[string]string{
			"status": fmt.Sprintf("cannot transition from %s to %s", current.Status, newStatus),
		})
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx,
		`UPDATE memories SET status = $1, updated_at = NOW() WHERE id = $2 RETURNING `+memorySelectCols,
		string(newStatus), id,
	)
	updated, err := scanMemory(row)
	if err != nil {
		return nil, fmt.Errorf("transitioning status: %w", err)
	}

	oldStatus := current.Status
	action := statusToAuditAction(newStatus)
	if err := insertAuditLog(ctx, tx, domain.MemoryAuditEntry{
		MemoryID:  updated.ID,
		UserID:    updated.UserID,
		Action:    action,
		OldStatus: &oldStatus,
		NewStatus: &newStatus,
		ChangedBy: changedBy,
		Reason:    &reason,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing status transition: %w", err)
	}

	return updated, nil
}

// GetAuditLog returns the audit history for a memory.
func (r *MemoryRepository) GetAuditLog(ctx context.Context, memoryID, userID uuid.UUID) ([]*domain.MemoryAuditEntry, error) {
	// Verify ownership first.
	if _, err := r.GetByID(ctx, memoryID, userID); err != nil {
		return nil, err
	}

	const q = `
		SELECT id, memory_id, user_id, action,
		       old_status, new_status,
		       old_content, new_content,
		       old_value, new_value, field_changed,
		       changed_by, reason, created_at
		FROM memory_audit_log
		WHERE memory_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, q, memoryID)
	if err != nil {
		return nil, fmt.Errorf("fetching audit log: %w", err)
	}
	defer rows.Close()

	entries := make([]*domain.MemoryAuditEntry, 0)
	for rows.Next() {
		e := &domain.MemoryAuditEntry{}
		if err := rows.Scan(
			&e.ID, &e.MemoryID, &e.UserID, &e.Action,
			&e.OldStatus, &e.NewStatus,
			&e.OldContent, &e.NewContent,
			&e.OldValue, &e.NewValue, &e.FieldChanged,
			&e.ChangedBy, &e.Reason, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// TouchAccess updates last_accessed and increments access_count.
// Called by the retrieval layer — lightweight, fire-and-forget acceptable.
func (r *MemoryRepository) TouchAccess(ctx context.Context, id uuid.UUID) {
	_, _ = r.db.Exec(ctx,
		`UPDATE memories SET last_accessed = NOW(), access_count = access_count + 1 WHERE id = $1`,
		id,
	)
}

// BelongsToUser checks ownership without loading the full memory.
func (r *MemoryRepository) BelongsToUser(ctx context.Context, memoryID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM memories WHERE id = $1 AND user_id = $2 AND status != 'DELETED')`,
		memoryID, userID,
	).Scan(&exists)
	return exists, err
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// insertAuditLog writes one audit entry inside a transaction.
func insertAuditLog(ctx context.Context, tx pgx.Tx, e domain.MemoryAuditEntry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO memory_audit_log (
			memory_id, user_id, action,
			old_status, new_status,
			old_content, new_content,
			old_value, new_value, field_changed,
			changed_by, reason
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		e.MemoryID, e.UserID, string(e.Action),
		statusStrPtr(e.OldStatus), statusStrPtr(e.NewStatus),
		e.OldContent, e.NewContent,
		e.OldValue, e.NewValue, e.FieldChanged,
		e.ChangedBy, e.Reason,
	)
	return err
}

func statusStrPtr(s *domain.MemoryStatus) *string {
	if s == nil {
		return nil
	}
	str := string(*s)
	return &str
}

func strPtr(s string) *string { return &s }

func computeDeleteAfter(retentionDays *int) *time.Time {
	if retentionDays == nil {
		return nil
	}
	t := time.Now().AddDate(0, 0, *retentionDays)
	return &t
}

func statusToAuditAction(s domain.MemoryStatus) domain.AuditAction {
	switch s {
	case domain.MemoryStatusArchived:
		return domain.AuditArchived
	case domain.MemoryStatusDeleted:
		return domain.AuditDeleted
	case domain.MemoryStatusActive:
		return domain.AuditRestored
	default:
		return domain.AuditStatusChanged
	}
}

// ForgetByNamespace soft-deletes all active memories in a namespace for a user.
// Returns the number of memories deleted.
func (r *MemoryRepository) ForgetByNamespace(ctx context.Context, userID uuid.UUID, namespace, reason string) (int64, error) {
	// Fetch IDs to bulk-delete.
	rows, err := r.db.Query(ctx,
		`SELECT id FROM memories WHERE user_id = $1 AND namespace = $2 AND status NOT IN ('DELETED', 'ARCHIVED')`,
		userID, namespace,
	)
	if err != nil {
		return 0, fmt.Errorf("fetching memories for bulk forget: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, id := range ids {
		_, err := tx.Exec(ctx,
			`UPDATE memories SET status = 'DELETED', updated_at = NOW() WHERE id = $1`, id,
		)
		if err != nil {
			return 0, fmt.Errorf("deleting memory %s: %w", id, err)
		}
		deleted := domain.MemoryStatusDeleted
		_ = insertAuditLog(ctx, tx, domain.MemoryAuditEntry{
			MemoryID:  id,
			UserID:    userID,
			Action:    domain.AuditDeleted,
			NewStatus: &deleted,
			ChangedBy: "user",
			Reason:    &reason,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing bulk forget: %w", err)
	}

	return int64(len(ids)), nil
}

// UpdateScoredSalience persists a new computed salience value and stamps scored_at.
// Called by the background decay worker (Phase 9) and the scoring service.
func (r *MemoryRepository) UpdateScoredSalience(ctx context.Context, id uuid.UUID, newSalience float64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE memories SET salience = $1, scored_at = NOW(), updated_at = NOW() WHERE id = $2`,
		newSalience, id,
	)
	return err
}

// ListForDecay returns all ACTIVE memories that have not been scored since cutoff.
// Used by the background decay worker to find stale salience values.
func (r *MemoryRepository) ListForDecay(ctx context.Context, cutoff time.Time, limit int) ([]*domain.Memory, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM memories
		WHERE status = 'ACTIVE'
		  AND (scored_at IS NULL OR scored_at < $1)
		ORDER BY last_accessed ASC
		LIMIT $2
	`, memorySelectCols)

	rows, err := r.db.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("listing memories for decay: %w", err)
	}
	defer rows.Close()

	var mems []*domain.Memory
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// ListScoredByImportanceLevel returns ACTIVE memories filtered by importance tier.
func (r *MemoryRepository) ListScoredByImportanceLevel(
	ctx context.Context,
	userID uuid.UUID,
	level string,
	limit int,
) ([]*domain.Memory, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM memories
		WHERE user_id = $1
		  AND importance_level = $2
		  AND status = 'ACTIVE'
		ORDER BY importance DESC, salience DESC
		LIMIT $3
	`, memorySelectCols)

	rows, err := r.db.Query(ctx, q, userID, level, limit)
	if err != nil {
		return nil, fmt.Errorf("listing by importance level: %w", err)
	}
	defer rows.Close()

	var mems []*domain.Memory
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// FindByContentHash looks up an active memory by its content hash for exact dedup.
func (r *MemoryRepository) FindByContentHash(ctx context.Context, userID uuid.UUID, hash string) (*domain.Memory, error) {
	q := `SELECT ` + memorySelectCols + `
		FROM memories
		WHERE user_id = $1 AND content_hash = $2 AND status NOT IN ('DELETED', 'ARCHIVED')
		LIMIT 1`

	mem, err := scanMemory(r.db.QueryRow(ctx, q, userID, hash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // not found is fine for dedup check
		}
		return nil, err
	}
	return mem, nil
}

// ListActiveForUser returns ACTIVE memories for a user/namespace (for dedup checks).
func (r *MemoryRepository) ListActiveForUser(ctx context.Context, userID uuid.UUID, namespace string, limit int) ([]*domain.Memory, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM memories
		WHERE user_id = $1 AND namespace = $2 AND status = 'ACTIVE'
		ORDER BY importance DESC, created_at DESC
		LIMIT $3
	`, memorySelectCols)

	rows, err := r.db.Query(ctx, q, userID, namespace, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mems []*domain.Memory
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// ListActiveByTypeAndNamespace returns ACTIVE memories filtered by type and namespace (for conflict checks).
func (r *MemoryRepository) ListActiveByTypeAndNamespace(
	ctx context.Context,
	userID uuid.UUID,
	memType domain.MemoryType,
	namespace string,
	limit int,
) ([]*domain.Memory, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM memories
		WHERE user_id = $1 AND memory_type = $2 AND namespace = $3 AND status = 'ACTIVE'
		ORDER BY importance DESC, created_at DESC
		LIMIT $4
	`, memorySelectCols)

	rows, err := r.db.Query(ctx, q, userID, string(memType), namespace, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mems []*domain.Memory
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// SetValidUntil sets the valid_until timestamp on a memory.
func (r *MemoryRepository) SetValidUntil(ctx context.Context, id uuid.UUID, t time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE memories SET valid_until = $1, updated_at = NOW() WHERE id = $2`,
		t, id,
	)
	return err
}

// SetContentHash writes the content_hash for a memory (called after creation).
func (r *MemoryRepository) SetContentHash(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE memories SET content_hash = $1 WHERE id = $2`,
		hash, id,
	)
	return err
}

// GetByIDAdmin fetches a memory by ID without ownership check.
// Used internally (e.g., uncertainty propagation across users is not applicable,
// but needed for system-level operations that already verified ownership upstream).
func (r *MemoryRepository) GetByIDAdmin(ctx context.Context, id uuid.UUID) (*domain.Memory, error) {
	q := `SELECT ` + memorySelectCols + ` FROM memories WHERE id = $1`
	mem, err := scanMemory(r.db.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("memory", id.String())
		}
		return nil, fmt.Errorf("getting memory %s (admin): %w", id, err)
	}
	return mem, nil
}

// ListExpired returns memories whose delete_after timestamp has passed.
func (r *MemoryRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]*domain.Memory, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM memories
		WHERE delete_after IS NOT NULL
		  AND delete_after <= $1
		  AND status NOT IN ('DELETED')
		ORDER BY delete_after ASC
		LIMIT $2
	`, memorySelectCols)

	rows, err := r.db.Query(ctx, q, now, limit)
	if err != nil {
		return nil, fmt.Errorf("listing expired memories: %w", err)
	}
	defer rows.Close()

	var mems []*domain.Memory
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// ListForConsolidation returns RAW ACTIVE memories eligible for promotion.
// Criteria: importance > 0.7, created more than 7 days ago.
func (r *MemoryRepository) ListForConsolidation(ctx context.Context, limit int) ([]*domain.Memory, error) {
	cutoff := time.Now().AddDate(0, 0, -7)
	q := fmt.Sprintf(`
		SELECT %s FROM memories
		WHERE status = 'ACTIVE'
		  AND memory_level = 'RAW'
		  AND importance > 0.7
		  AND created_at < $1
		ORDER BY importance DESC, created_at ASC
		LIMIT $2
	`, memorySelectCols)

	rows, err := r.db.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("listing consolidation candidates: %w", err)
	}
	defer rows.Close()

	var mems []*domain.Memory
	for rows.Next() {
		m, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// SetMemoryLevel directly updates the memory_level field without versioning.
// Used by the consolidation worker.
func (r *MemoryRepository) SetMemoryLevel(ctx context.Context, id uuid.UUID, level domain.MemoryLevel) error {
	_, err := r.db.Exec(ctx,
		`UPDATE memories SET memory_level = $1, updated_at = NOW() WHERE id = $2`,
		string(level), id,
	)
	return err
}
