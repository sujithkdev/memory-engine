// Package httputil provides shared HTTP helpers used across handler packages.
package httputil

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// Response is the standard API envelope.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

// APIError carries a machine-readable code and human-readable message.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JSON writes a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// Success writes a 200 OK JSON response.
func Success(w http.ResponseWriter, data interface{}) {
	JSON(w, http.StatusOK, Response{Success: true, Data: data})
}

// Created writes a 201 Created JSON response.
func Created(w http.ResponseWriter, data interface{}) {
	JSON(w, http.StatusCreated, Response{Success: true, Data: data})
}

// ErrorResponse writes an error JSON response.
func ErrorResponse(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, Response{
		Success: false,
		Error:   &APIError{Code: code, Message: message},
	})
}

// BadRequest writes 400.
func BadRequest(w http.ResponseWriter, message string) {
	ErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", message)
}

// NotFound writes 404.
func NotFound(w http.ResponseWriter, message string) {
	ErrorResponse(w, http.StatusNotFound, "NOT_FOUND", message)
}

// InternalError writes 500.
func InternalError(w http.ResponseWriter, message string) {
	ErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", message)
}

// ServiceUnavailable writes 503.
func ServiceUnavailable(w http.ResponseWriter, message string) {
	ErrorResponse(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", message)
}

// HandleDomainError maps domain errors to HTTP responses.
func HandleDomainError(w http.ResponseWriter, err error) {
	var notFound *domain.ErrNotFound
	var conflict *domain.ErrConflict
	var validation *domain.ErrValidation
	var forbidden *domain.ErrForbidden

	switch {
	case errors.As(err, &notFound):
		NotFound(w, notFound.Error())
	case errors.As(err, &conflict):
		ErrorResponse(w, http.StatusConflict, "CONFLICT", conflict.Error())
	case errors.As(err, &validation):
		JSON(w, http.StatusUnprocessableEntity, Response{
			Success: false,
			Error:   &APIError{Code: "VALIDATION_ERROR", Message: "request validation failed"},
			Data:    validation.Fields,
		})
	case errors.As(err, &forbidden):
		ErrorResponse(w, http.StatusForbidden, "FORBIDDEN", forbidden.Error())
	default:
		InternalError(w, "an unexpected error occurred")
	}
}
