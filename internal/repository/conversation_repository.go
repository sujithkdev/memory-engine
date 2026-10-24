package repository

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ConversationRepository handles all conversation persistence.
type ConversationRepository struct {
	db *pgxpool.Pool
}

// NewConversationRepository creates a new ConversationRepository.
func NewConversationRepository(db *pgxpool.Pool) *ConversationRepository {
	return &ConversationRepository{db: db}
}

// Create inserts a new conversation.
func (r *ConversationRepository) Create(ctx context.Context, input domain.CreateConversationInput) (*domain.Conversation, error) {
	const q = `
		INSERT INTO conversations (user_id, title, namespace)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, title, namespace, is_archived, created_at, updated_at
	`

	conv := &domain.Conversation{}
	err := r.db.QueryRow(ctx, q, input.UserID, input.Title, input.Namespace).Scan(
		&conv.ID, &conv.UserID, &conv.Title, &conv.Namespace,
		&conv.IsArchived, &conv.CreatedAt, &conv.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating conversation: %w", err)
	}
	return conv, nil
}

// GetByID fetches a conversation by primary key, with message count.
func (r *ConversationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Conversation, error) {
	const q = `
		SELECT c.id, c.user_id, c.title, c.namespace, c.is_archived,
		       c.created_at, c.updated_at,
		       COUNT(m.id) AS message_count
		FROM conversations c
		LEFT JOIN messages m ON m.conversation_id = c.id
		WHERE c.id = $1
		GROUP BY c.id
	`

	conv := &domain.Conversation{}
	err := r.db.QueryRow(ctx, q, id).Scan(
		&conv.ID, &conv.UserID, &conv.Title, &conv.Namespace, &conv.IsArchived,
		&conv.CreatedAt, &conv.UpdatedAt, &conv.MessageCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("conversation", id.String())
		}
		return nil, fmt.Errorf("getting conversation %s: %w", id, err)
	}
	return conv, nil
}

// List returns paginated conversations for a user.
func (r *ConversationRepository) List(ctx context.Context, filter domain.ConversationFilter) (*domain.ConversationList, error) {
	args := []any{filter.UserID}
	argIdx := 2
	whereClauses := []string{"c.user_id = $1"}

	if filter.Namespace != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("c.namespace = $%d", argIdx))
		args = append(args, *filter.Namespace)
		argIdx++
	}
	if filter.IsArchived != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("c.is_archived = $%d", argIdx))
		args = append(args, *filter.IsArchived)
		argIdx++
	} else {
		// Default: exclude archived.
		whereClauses = append(whereClauses, "c.is_archived = FALSE")
	}

	whereSQL := "WHERE " + joinClauses(whereClauses)

	// Count.
	countQ := fmt.Sprintf(`SELECT COUNT(*) FROM conversations c %s`, whereSQL)
	var total int64
	if err := r.db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting conversations: %w", err)
	}

	// Data.
	offset := (filter.Page - 1) * filter.PageSize
	dataArgs := append(args, filter.PageSize, offset)
	dataQ := fmt.Sprintf(`
		SELECT c.id, c.user_id, c.title, c.namespace, c.is_archived,
		       c.created_at, c.updated_at,
		       COUNT(m.id) AS message_count
		FROM conversations c
		LEFT JOIN messages m ON m.conversation_id = c.id
		%s
		GROUP BY c.id
		ORDER BY c.updated_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	rows, err := r.db.Query(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("listing conversations: %w", err)
	}
	defer rows.Close()

	convs := make([]*domain.Conversation, 0)
	for rows.Next() {
		c := &domain.Conversation{}
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.Title, &c.Namespace, &c.IsArchived,
			&c.CreatedAt, &c.UpdatedAt, &c.MessageCount,
		); err != nil {
			return nil, fmt.Errorf("scanning conversation: %w", err)
		}
		convs = append(convs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating conversations: %w", err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(filter.PageSize)))
	if totalPages == 0 {
		totalPages = 1
	}

	return &domain.ConversationList{
		Conversations: convs,
		Total:         total,
		Page:          filter.Page,
		PageSize:      filter.PageSize,
		TotalPages:    totalPages,
	}, nil
}

// Update applies partial updates to a conversation.
func (r *ConversationRepository) Update(ctx context.Context, id uuid.UUID, input domain.UpdateConversationInput) (*domain.Conversation, error) {
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if input.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", argIdx))
		args = append(args, *input.Title)
		argIdx++
	}
	if input.Namespace != nil {
		setClauses = append(setClauses, fmt.Sprintf("namespace = $%d", argIdx))
		args = append(args, *input.Namespace)
		argIdx++
	}
	if input.IsArchived != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_archived = $%d", argIdx))
		args = append(args, *input.IsArchived)
		argIdx++
	}

	if len(setClauses) == 1 {
		return r.GetByID(ctx, id)
	}

	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE conversations SET %s
		WHERE id = $%d
		RETURNING id, user_id, title, namespace, is_archived, created_at, updated_at
	`, joinClauses(setClauses), argIdx)

	conv := &domain.Conversation{}
	err := r.db.QueryRow(ctx, q, args...).Scan(
		&conv.ID, &conv.UserID, &conv.Title, &conv.Namespace,
		&conv.IsArchived, &conv.CreatedAt, &conv.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("conversation", id.String())
		}
		return nil, fmt.Errorf("updating conversation %s: %w", id, err)
	}
	return conv, nil
}

// Delete permanently deletes a conversation and cascades to messages.
func (r *ConversationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting conversation %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("conversation", id.String())
	}
	return nil
}

// BelongsToUser checks ownership — prevents cross-user access.
func (r *ConversationRepository) BelongsToUser(ctx context.Context, convID, userID uuid.UUID) (bool, error) {
	var belongs bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversations WHERE id = $1 AND user_id = $2)`,
		convID, userID,
	).Scan(&belongs)
	return belongs, err
}
