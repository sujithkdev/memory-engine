package domain

import (
	"time"

	"github.com/google/uuid"
)

// Conversation represents a chat session belonging to a user.
type Conversation struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	Title      *string   `json:"title,omitempty"`
	Namespace  string    `json:"namespace"`
	IsArchived bool      `json:"is_archived"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// MessageCount is a derived field — populated by JOIN, not stored.
	MessageCount int64 `json:"message_count,omitempty"`
}

// CreateConversationInput holds validated input for creating a conversation.
type CreateConversationInput struct {
	UserID    uuid.UUID `json:"user_id"`
	Title     *string   `json:"title,omitempty"`
	Namespace string    `json:"namespace"`
}

// UpdateConversationInput holds partial update fields.
type UpdateConversationInput struct {
	Title      *string `json:"title,omitempty"`
	Namespace  *string `json:"namespace,omitempty"`
	IsArchived *bool   `json:"is_archived,omitempty"`
}

// ConversationFilter controls listing queries.
type ConversationFilter struct {
	UserID     uuid.UUID
	Namespace  *string
	IsArchived *bool
	Page       int
	PageSize   int
}

// ConversationList is a paginated list of conversations.
type ConversationList struct {
	Conversations []*Conversation `json:"conversations"`
	Total         int64           `json:"total"`
	Page          int             `json:"page"`
	PageSize      int             `json:"page_size"`
	TotalPages    int             `json:"total_pages"`
}

// Known namespaces — not enforced at DB level, but validated at API level.
var KnownNamespaces = map[string]bool{
	"DEFAULT":  true,
	"TRADING":  true,
	"CODING":   true,
	"PERSONAL": true,
	"WORK":     true,
	"LEARNING": true,
	"HEALTH":   true,
	"FINANCE":  true,
}
