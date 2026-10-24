package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

// MessageService handles message business logic.
type MessageService struct {
	msgs  *repository.MessageRepository
	convs *repository.ConversationRepository
}

// NewMessageService creates a new MessageService.
func NewMessageService(
	msgs *repository.MessageRepository,
	convs *repository.ConversationRepository,
) *MessageService {
	return &MessageService{msgs: msgs, convs: convs}
}

// Create appends a message to a conversation.
func (s *MessageService) Create(ctx context.Context, userID uuid.UUID, input domain.CreateMessageInput) (*domain.Message, error) {
	// Validate input.
	if errs := validateCreateMessage(input); len(errs) > 0 {
		return nil, domain.Validation(errs)
	}

	// Verify conversation ownership.
	belongs, err := s.convs.BelongsToUser(ctx, input.ConversationID, userID)
	if err != nil {
		return nil, fmt.Errorf("checking conversation ownership: %w", err)
	}
	if !belongs {
		return nil, domain.NotFound("conversation", input.ConversationID.String())
	}

	return s.msgs.Create(ctx, input)
}

// GetByID fetches a single message, verifying user access via conversation.
func (s *MessageService) GetByID(ctx context.Context, id, userID uuid.UUID) (*domain.Message, error) {
	msg, err := s.msgs.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Verify the user owns the conversation this message belongs to.
	belongs, err := s.convs.BelongsToUser(ctx, msg.ConversationID, userID)
	if err != nil {
		return nil, fmt.Errorf("checking ownership: %w", err)
	}
	if !belongs {
		return nil, domain.NotFound("message", id.String())
	}

	return msg, nil
}

// List returns paginated messages for a conversation.
func (s *MessageService) List(ctx context.Context, userID uuid.UUID, filter domain.MessageFilter) (*domain.MessageList, error) {
	// Verify user owns the conversation.
	belongs, err := s.convs.BelongsToUser(ctx, filter.ConversationID, userID)
	if err != nil {
		return nil, fmt.Errorf("checking ownership: %w", err)
	}
	if !belongs {
		return nil, domain.NotFound("conversation", filter.ConversationID.String())
	}

	filter = normalizeMsgFilter(filter)
	return s.msgs.List(ctx, filter)
}

func validateCreateMessage(input domain.CreateMessageInput) map[string]string {
	errs := map[string]string{}

	if input.ConversationID == uuid.Nil {
		errs["conversation_id"] = "conversation_id is required"
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		errs["content"] = "content is required"
	} else if len(content) > 100_000 {
		errs["content"] = "content must not exceed 100,000 characters"
	}

	if !domain.ValidRoles[input.Role] {
		errs["role"] = "role must be one of: user, assistant, system, tool"
	}

	if input.TokenCount != nil && *input.TokenCount < 0 {
		errs["token_count"] = "token_count must be non-negative"
	}

	return errs
}

func normalizeMsgFilter(f domain.MessageFilter) domain.MessageFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 50
	}
	return f
}
