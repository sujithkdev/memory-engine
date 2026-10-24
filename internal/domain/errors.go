package domain

import "fmt"

// Sentinel errors — use errors.Is() to check these.

// ErrNotFound is returned when a requested resource does not exist.
type ErrNotFound struct {
	Resource string
	ID       string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("%s with id %q not found", e.Resource, e.ID)
}

// ErrConflict is returned when a unique constraint is violated.
type ErrConflict struct {
	Resource string
	Field    string
	Value    string
}

func (e *ErrConflict) Error() string {
	return fmt.Sprintf("%s with %s %q already exists", e.Resource, e.Field, e.Value)
}

// ErrValidation is returned when input fails validation.
type ErrValidation struct {
	Fields map[string]string // field name → error message
}

func (e *ErrValidation) Error() string {
	return fmt.Sprintf("validation failed: %v", e.Fields)
}

// ErrForbidden is returned when an operation is not permitted.
type ErrForbidden struct {
	Message string
}

func (e *ErrForbidden) Error() string {
	return e.Message
}

// NotFound constructs an ErrNotFound.
func NotFound(resource, id string) *ErrNotFound {
	return &ErrNotFound{Resource: resource, ID: id}
}

// Conflict constructs an ErrConflict.
func Conflict(resource, field, value string) *ErrConflict {
	return &ErrConflict{Resource: resource, Field: field, Value: value}
}

// Validation constructs an ErrValidation.
func Validation(fields map[string]string) *ErrValidation {
	return &ErrValidation{Fields: fields}
}
