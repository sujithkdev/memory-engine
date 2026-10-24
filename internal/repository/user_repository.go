// Package repository implements data access using pgx.
// Each repository is responsible for one domain entity.
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

// UserRepository handles all user persistence.
type UserRepository struct {
	db *pgxpool.Pool
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user and returns the created record.
func (r *UserRepository) Create(ctx context.Context, input domain.CreateUserInput) (*domain.User, error) {
	const q = `
		INSERT INTO users (username, email, display_name, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, username, email, display_name, is_active, created_at, updated_at, created_by
	`

	user := &domain.User{}
	err := r.db.QueryRow(ctx, q,
		input.Username,
		input.Email,
		input.DisplayName,
		input.CreatedBy,
	).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.DisplayName,
		&user.IsActive,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.CreatedBy,
	)
	if err != nil {
		return nil, mapDBError(err, "user", "username", input.Username)
	}
	return user, nil
}

// GetByID fetches a user by primary key.
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	const q = `
		SELECT id, username, email, display_name, is_active, created_at, updated_at, created_by
		FROM users
		WHERE id = $1
	`

	user := &domain.User{}
	err := r.db.QueryRow(ctx, q, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.DisplayName,
		&user.IsActive,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.CreatedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("user", id.String())
		}
		return nil, fmt.Errorf("getting user %s: %w", id, err)
	}
	return user, nil
}

// GetByUsername fetches a user by username.
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	const q = `
		SELECT id, username, email, display_name, is_active, created_at, updated_at, created_by
		FROM users
		WHERE username = $1
	`

	user := &domain.User{}
	err := r.db.QueryRow(ctx, q, username).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.DisplayName,
		&user.IsActive,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.CreatedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("user", username)
		}
		return nil, fmt.Errorf("getting user by username %s: %w", username, err)
	}
	return user, nil
}

// List returns a paginated list of users with optional active filter.
func (r *UserRepository) List(ctx context.Context, filter domain.UserFilter) (*domain.UserList, error) {
	offset := (filter.Page - 1) * filter.PageSize

	// Count query.
	countQ := `SELECT COUNT(*) FROM users`
	args := []any{}
	where := ``

	if filter.IsActive != nil {
		where = ` WHERE is_active = $1`
		args = append(args, *filter.IsActive)
	}

	var total int64
	if err := r.db.QueryRow(ctx, countQ+where, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting users: %w", err)
	}

	// Data query.
	dataArgs := append(args, filter.PageSize, offset)
	placeholderOffset := len(args) + 1
	dataQ := fmt.Sprintf(`
		SELECT id, username, email, display_name, is_active, created_at, updated_at, created_by
		FROM users%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, placeholderOffset, placeholderOffset+1)

	rows, err := r.db.Query(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	users := make([]*domain.User, 0)
	for rows.Next() {
		u := &domain.User{}
		if err := rows.Scan(
			&u.ID, &u.Username, &u.Email, &u.DisplayName,
			&u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.CreatedBy,
		); err != nil {
			return nil, fmt.Errorf("scanning user row: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating users: %w", err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(filter.PageSize)))
	if totalPages == 0 {
		totalPages = 1
	}

	return &domain.UserList{
		Users:      users,
		Total:      total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
		TotalPages: totalPages,
	}, nil
}

// Update applies partial updates to a user.
func (r *UserRepository) Update(ctx context.Context, id uuid.UUID, input domain.UpdateUserInput) (*domain.User, error) {
	// Build dynamic SET clause — only update provided fields.
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if input.Username != nil {
		setClauses = append(setClauses, fmt.Sprintf("username = $%d", argIdx))
		args = append(args, *input.Username)
		argIdx++
	}
	if input.Email != nil {
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", argIdx))
		args = append(args, *input.Email)
		argIdx++
	}
	if input.DisplayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
		args = append(args, *input.DisplayName)
		argIdx++
	}

	if len(setClauses) == 1 {
		// Only updated_at — nothing to change, return current.
		return r.GetByID(ctx, id)
	}

	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE users SET %s
		WHERE id = $%d
		RETURNING id, username, email, display_name, is_active, created_at, updated_at, created_by
	`, joinClauses(setClauses), argIdx)

	user := &domain.User{}
	err := r.db.QueryRow(ctx, q, args...).Scan(
		&user.ID, &user.Username, &user.Email, &user.DisplayName,
		&user.IsActive, &user.CreatedAt, &user.UpdatedAt, &user.CreatedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NotFound("user", id.String())
		}
		return nil, mapDBError(err, "user", "username", "")
	}
	return user, nil
}

// SoftDelete marks a user as inactive.
func (r *UserRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE users SET is_active = FALSE, updated_at = NOW() WHERE id = $1`
	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("soft-deleting user %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("user", id.String())
	}
	return nil
}

// Exists checks if a user ID is valid and active.
func (r *UserRepository) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND is_active = TRUE)`, id,
	).Scan(&exists)
	return exists, err
}
