package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

// ConversationService handles conversation business logic.
type ConversationService struct {
	convs *repository.ConversationRepository
	users *repository.UserRepository
}

// NewConversationService creates a new ConversationService.
func NewConversationService(
	convs *repository.ConversationRepository,
	users *repository.UserRepository,
) *ConversationService {
	return &ConversationService{convs: convs, users: users}
}

// Create validates and creates a new conversation.
func (s *ConversationService) Create(ctx context.Context, input domain.CreateConversationInput) (*domain.Conversation, error) {
	// Normalize namespace.
	input.Namespace = strings.ToUpper(strings.TrimSpace(input.Namespace))
	if input.Namespace == "" {
		input.Namespace = "DEFAULT"
	}

	// Validate.
	if errs := validateCreateConversation(input); len(errs) > 0 {
		return nil, domain.Validation(errs)
	}

	// Verify user exists.
	exists, err := s.users.Exists(ctx, input.UserID)
	if err != nil {
		return nil, fmt.Errorf("checking user existence: %w", err)
	}
	if !exists {
		return nil, domain.NotFound("user", input.UserID.String())
	}

	return s.convs.Create(ctx, input)
}

// GetByID fetches a conversation, verifying user ownership.
func (s *ConversationService) GetByID(ctx context.Context, id, userID uuid.UUID) (*domain.Conversation, error) {
	conv, err := s.convs.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Ownership check.
	if conv.UserID != userID {
		return nil, domain.NotFound("conversation", id.String())
	}

	return conv, nil
}

// List returns paginated conversations for a user.
func (s *ConversationService) List(ctx context.Context, filter domain.ConversationFilter) (*domain.ConversationList, error) {
	filter = normalizeConvFilter(filter)
	return s.convs.List(ctx, filter)
}

// Update partially updates a conversation.
func (s *ConversationService) Update(ctx context.Context, id, userID uuid.UUID, input domain.UpdateConversationInput) (*domain.Conversation, error) {
	// Verify ownership.
	belongs, err := s.convs.BelongsToUser(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("checking ownership: %w", err)
	}
	if !belongs {
		return nil, domain.NotFound("conversation", id.String())
	}

	if input.Namespace != nil {
		ns := strings.ToUpper(strings.TrimSpace(*input.Namespace))
		input.Namespace = &ns
	}

	return s.convs.Update(ctx, id, input)
}

// Delete removes a conversation (and cascades to messages).
func (s *ConversationService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	belongs, err := s.convs.BelongsToUser(ctx, id, userID)
	if err != nil {
		return fmt.Errorf("checking ownership: %w", err)
	}
	if !belongs {
		return domain.NotFound("conversation", id.String())
	}
	return s.convs.Delete(ctx, id)
}

func validateCreateConversation(input domain.CreateConversationInput) map[string]string {
	errs := map[string]string{}

	if input.UserID == uuid.Nil {
		errs["user_id"] = "user_id is required"
	}

	if input.Title != nil && len(*input.Title) > 200 {
		errs["title"] = "title must not exceed 200 characters"
	}

	return errs
}

func normalizeConvFilter(f domain.ConversationFilter) domain.ConversationFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 20
	}
	return f
}
