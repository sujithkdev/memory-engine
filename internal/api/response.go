// Package api contains HTTP handlers, routing, and response helpers.
// Response helpers are thin wrappers around httputil for use within this package.
package api

import (
	"net/http"

	"github.com/memory-engine/memory-engine/internal/httputil"
)

// Re-export types so health.go and router.go can use them without importing httputil.
type Response = httputil.Response
type APIError = httputil.APIError

// JSON writes a JSON response.
func JSON(w http.ResponseWriter, status int, payload interface{}) {
	httputil.JSON(w, status, payload)
}

// Success writes a 200 OK JSON response.
func Success(w http.ResponseWriter, data interface{}) {
	httputil.Success(w, data)
}

// Created writes a 201 Created JSON response.
func Created(w http.ResponseWriter, data interface{}) {
	httputil.Created(w, data)
}

// ErrorResponse writes an error JSON response.
func ErrorResponse(w http.ResponseWriter, status int, code, message string) {
	httputil.ErrorResponse(w, status, code, message)
}

// BadRequest writes 400.
func BadRequest(w http.ResponseWriter, message string) {
	httputil.BadRequest(w, message)
}

// NotFound writes 404.
func NotFound(w http.ResponseWriter, message string) {
	httputil.NotFound(w, message)
}

// InternalError writes 500.
func InternalError(w http.ResponseWriter, message string) {
	httputil.InternalError(w, message)
}

// ServiceUnavailable writes 503.
func ServiceUnavailable(w http.ResponseWriter, message string) {
	httputil.ServiceUnavailable(w, message)
}
