// Package domain contains pure business models with no infrastructure dependencies.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// User represents a system user who owns conversations and memories.
type User struct {
	ID          uuid.UUID  `json:"id"`
	Username    string     `json:"username"`
	Email       *string    `json:"email,omitempty"`
	DisplayName *string    `json:"display_name,omitempty"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
}

// CreateUserInput holds validated input for creating a user.
type CreateUserInput struct {
	Username    string  `json:"username"`
	Email       *string `json:"email,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
}

// UpdateUserInput holds validated input for updating a user.
// All fields are optional — only non-nil fields are applied.
type UpdateUserInput struct {
	Username    *string `json:"username,omitempty"`
	Email       *string `json:"email,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
}

// UserFilter controls listing queries.
type UserFilter struct {
	IsActive *bool
	Page     int
	PageSize int
}

// UserList is a paginated list of users.
type UserList struct {
	Users      []*User `json:"users"`
	Total      int64   `json:"total"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
	TotalPages int     `json:"total_pages"`
}
