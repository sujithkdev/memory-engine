package domain

import (
	"time"

	"github.com/google/uuid"
)

// Role defines who sent a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// ValidRoles is the set of accepted role values.
var ValidRoles = map[Role]bool{
	RoleUser:      true,
	RoleAssistant: true,
	RoleSystem:    true,
	RoleTool:      true,
}

// Message is a single turn in a conversation.
type Message struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	Role           Role      `json:"role"`
	Content        string    `json:"content"`
	TokenCount     *int      `json:"token_count,omitempty"`
	SequenceNumber int       `json:"sequence_number"`
	SourceModel    *string   `json:"source_model,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// CreateMessageInput holds validated input for appending a message.
type CreateMessageInput struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	Role           Role      `json:"role"`
	Content        string    `json:"content"`
	TokenCount     *int      `json:"token_count,omitempty"`
	SourceModel    *string   `json:"source_model,omitempty"`
}

// MessageFilter controls listing queries.
type MessageFilter struct {
	ConversationID uuid.UUID
	Role           *Role
	Page           int
	PageSize       int
}

// MessageList is a paginated list of messages.
type MessageList struct {
	Messages   []*Message `json:"messages"`
	Total      int64      `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	TotalPages int        `json:"total_pages"`
}
