// Package service contains business logic that orchestrates repositories.
package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/repository"
)

// UserService handles user business logic.
type UserService struct {
	users *repository.UserRepository
}

// NewUserService creates a new UserService.
func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
}

// Create validates and creates a new user.
func (s *UserService) Create(ctx context.Context, input domain.CreateUserInput) (*domain.User, error) {
	// Normalize.
	input.Username = strings.TrimSpace(strings.ToLower(input.Username))

	// Validate.
	if errs := validateCreateUser(input); len(errs) > 0 {
		return nil, domain.Validation(errs)
	}

	user, err := s.users.Create(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("user service create: %w", err)
	}
	return user, nil
}

// GetByID fetches a user by ID.
func (s *UserService) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.users.GetByID(ctx, id)
}

// List returns a paginated user list.
func (s *UserService) List(ctx context.Context, filter domain.UserFilter) (*domain.UserList, error) {
	filter = normalizeUserFilter(filter)
	return s.users.List(ctx, filter)
}

// Update partially updates a user.
func (s *UserService) Update(ctx context.Context, id uuid.UUID, input domain.UpdateUserInput) (*domain.User, error) {
	if input.Username != nil {
		normalized := strings.TrimSpace(strings.ToLower(*input.Username))
		input.Username = &normalized
	}
	if errs := validateUpdateUser(input); len(errs) > 0 {
		return nil, domain.Validation(errs)
	}
	return s.users.Update(ctx, id, input)
}

// Delete soft-deletes a user.
func (s *UserService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.users.SoftDelete(ctx, id)
}

// --- validators ---

func validateCreateUser(input domain.CreateUserInput) map[string]string {
	errs := map[string]string{}

	if input.Username == "" {
		errs["username"] = "username is required"
	} else if len(input.Username) < 3 {
		errs["username"] = "username must be at least 3 characters"
	} else if len(input.Username) > 50 {
		errs["username"] = "username must not exceed 50 characters"
	}

	if input.Email != nil && *input.Email != "" {
		if !isValidEmail(*input.Email) {
			errs["email"] = "invalid email format"
		}
	}

	return errs
}

func validateUpdateUser(input domain.UpdateUserInput) map[string]string {
	errs := map[string]string{}

	if input.Username != nil {
		if *input.Username == "" {
			errs["username"] = "username cannot be empty"
		} else if len(*input.Username) < 3 {
			errs["username"] = "username must be at least 3 characters"
		} else if len(*input.Username) > 50 {
			errs["username"] = "username must not exceed 50 characters"
		}
	}

	if input.Email != nil && *input.Email != "" {
		if !isValidEmail(*input.Email) {
			errs["email"] = "invalid email format"
		}
	}

	return errs
}

func normalizeUserFilter(f domain.UserFilter) domain.UserFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 20
	}
	return f
}

// isValidEmail does a simple format check — no regex library needed.
func isValidEmail(email string) bool {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		return false
	}
	if !strings.Contains(parts[1], ".") {
		return false
	}
	return true
}
