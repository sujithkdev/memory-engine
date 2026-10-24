// Package middleware provides reusable HTTP middleware.
package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const requestIDHeader = "X-Request-ID"

// RequestLogger returns a middleware that logs each HTTP request with
// method, path, status, duration, and a unique request ID.
func RequestLogger(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Assign or propagate request ID.
			reqID := r.Header.Get(requestIDHeader)
			if reqID == "" {
				reqID = uuid.New().String()
			}

			// Inject request ID into response headers.
			w.Header().Set(requestIDHeader, reqID)

			// Embed request ID into logger for this request's context.
			reqLogger := logger.With().
				Str("request_id", reqID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Logger()

			// Store logger in context so handlers can use it.
			ctx := reqLogger.WithContext(r.Context())

			// Use chi's ResponseWriter wrapper for status capture.
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			start := time.Now()
			next.ServeHTTP(ww, r.WithContext(ctx))
			duration := time.Since(start)

			// Determine log level based on status code.
			status := ww.Status()
			evt := reqLogger.Info()
			if status >= 500 {
				evt = reqLogger.Error()
			} else if status >= 400 {
				evt = reqLogger.Warn()
			}

			evt.
				Int("status", status).
				Int("bytes", ww.BytesWritten()).
				Dur("duration_ms", duration).
				Msg("request completed")
		})
	}
}

// Recoverer catches panics, logs the stack trace, and returns HTTP 500.
func Recoverer(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					reqLogger := log.Ctx(r.Context())
					reqLogger.Error().
						Interface("panic", rec).
						Msg("panic recovered")

					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
