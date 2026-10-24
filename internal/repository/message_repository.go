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

// MessageRepository handles all message persistence.
type MessageRepository struct {
	db *pgxpool.Pool
}

// NewMessageRepository creates a new MessageRepository.
func NewMessageRepository(db *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{db: db}
}

// Create appends a message to a conversation.
// Sequence number is auto-assigned as (max + 1) within the conversation.
func (r *MessageRepository) Create(ctx context.Context, input domain.CreateMessageInput) (*domain.Message, error) {
	const q = `
		INSERT INTO messages (conversation_id, role, content, token_count, sequence_number, source_model)
		VALUES (
			$1, $2, $3, $4,
			COALESCE((SELECT MAX(sequence_number) FROM messages WHERE conversation_id = $1), 0) + 1,
			$5
		)
		RETURNING id, conversation_id, role, content, token_count, sequence_number, source_model, created_at
	`

	msg := &domain.Message{}
	err := r.db.QueryRow(ctx, q,
		input.ConversationID,
		input.Role,
		input.Content,
		input.TokenCount,
		input.SourceModel,
	).Scan(
		&msg.ID,
		&msg.ConversationID,
		&msg.Role,
		&msg.Content,
		&msg.TokenCount,
		&msg.SequenceNumber,
		&msg.SourceModel,
		&msg.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating message: %w", err)
	}

	// Touch the conversation's updated_at so it surfaces at the top of lists.
	_, _ = r.db.Exec(ctx,
		`UPDATE conversations SET updated_at = NOW() WHERE id = $1`,
		input.ConversationID,
	)

	return msg, nil
}

// GetByID fetches a single message.
func (r *MessageRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Message, error) {
	const q = `
		SELECT id, conversation_id, role, content, token_count, sequence_number, source_model, created_at
		FROM messages
		WHERE id = $1
	`

	msg := &domain.Message{}
	err := r.db.QueryRow(ctx, q, id).Scan(
		&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content,
		&msg.TokenCount, &msg.SequenceNumber, &msg.SourceModel, &msg.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("message", id.String())
		}
		return nil, fmt.Errorf("getting message %s: %w", id, err)
	}
	return msg, nil
}

// List returns paginated messages for a conversation, ordered by sequence.
func (r *MessageRepository) List(ctx context.Context, filter domain.MessageFilter) (*domain.MessageList, error) {
	args := []any{filter.ConversationID}
	argIdx := 2
	whereClauses := []string{"conversation_id = $1"}

	if filter.Role != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, string(*filter.Role))
		argIdx++
	}

	whereSQL := "WHERE " + joinClauses(whereClauses)

	// Count.
	var total int64
	if err := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM messages %s", whereSQL), args...,
	).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting messages: %w", err)
	}

	offset := (filter.Page - 1) * filter.PageSize
	dataArgs := append(args, filter.PageSize, offset)
	dataQ := fmt.Sprintf(`
		SELECT id, conversation_id, role, content, token_count, sequence_number, source_model, created_at
		FROM messages
		%s
		ORDER BY sequence_number ASC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	rows, err := r.db.Query(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	defer rows.Close()

	msgs := make([]*domain.Message, 0)
	for rows.Next() {
		m := &domain.Message{}
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&m.TokenCount, &m.SequenceNumber, &m.SourceModel, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating messages: %w", err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(filter.PageSize)))
	if totalPages == 0 {
		totalPages = 1
	}

	return &domain.MessageList{
		Messages:   msgs,
		Total:      total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
		TotalPages: totalPages,
	}, nil
}

// DeleteByConversation removes all messages in a conversation (used in tests).
func (r *MessageRepository) DeleteByConversation(ctx context.Context, convID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM messages WHERE conversation_id = $1`, convID)
	return err
}
