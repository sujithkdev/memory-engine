package repository

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// joinClauses joins SQL clauses with ", ".
func joinClauses(clauses []string) string {
	return strings.Join(clauses, ", ")
}

// mapDBError translates pgx/postgres errors to domain errors.
func mapDBError(err error, resource, field, value string) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			// Extract the constraint name for a more specific message.
			constraintField := field
			if pgErr.ConstraintName != "" {
				constraintField = pgErr.ConstraintName
			}
			return domain.Conflict(resource, constraintField, value)
		case "23503": // foreign_key_violation
			return fmt.Errorf("referenced %s not found: %w", resource, err)
		case "23502": // not_null_violation
			return fmt.Errorf("required field missing: %s", pgErr.ColumnName)
		}
	}

	return err
}
