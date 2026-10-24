package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/memory-engine/memory-engine/internal/database"
)

// HealthHandler handles liveness and readiness checks.
type HealthHandler struct {
	db      *database.DB
	version string
	name    string
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db *database.DB, name, version string) *HealthHandler {
	return &HealthHandler{db: db, version: version, name: name}
}

// HealthResponse is the payload returned by the health endpoints.
type HealthResponse struct {
	Status    string           `json:"status"`
	Name      string           `json:"name"`
	Version   string           `json:"version"`
	Timestamp time.Time        `json:"timestamp"`
	Checks    map[string]Check `json:"checks,omitempty"`
}

// Check represents one component's health status.
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Live handles GET /health — liveness probe.
// Returns 200 if the process is running, regardless of dependencies.
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, HealthResponse{
		Status:    "ok",
		Name:      h.name,
		Version:   h.version,
		Timestamp: time.Now().UTC(),
	})
}

// Ready handles GET /ready — readiness probe.
// Returns 200 only when all dependencies (DB, etc.) are healthy.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	checks := make(map[string]Check)
	allOK := true

	// Database ping check.
	dbCheck := h.checkDatabase(r.Context())
	checks["database"] = dbCheck
	if dbCheck.Status != "ok" {
		allOK = false
	}

	// Connection pool stats.
	stats := h.db.Stats()
	checks["pool"] = Check{
		Status: "ok",
		Message: fmt.Sprintf(
			"total=%d idle=%d acquired=%d",
			stats.TotalConns(),
			stats.IdleConns(),
			stats.AcquiredConns(),
		),
	}

	if allOK {
		JSON(w, http.StatusOK, HealthResponse{
			Status:    "ok",
			Name:      h.name,
			Version:   h.version,
			Timestamp: time.Now().UTC(),
			Checks:    checks,
		})
		return
	}

	JSON(w, http.StatusServiceUnavailable, HealthResponse{
		Status:    "degraded",
		Name:      h.name,
		Version:   h.version,
		Timestamp: time.Now().UTC(),
		Checks:    checks,
	})
}

func (h *HealthHandler) checkDatabase(ctx context.Context) Check {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		return Check{Status: "error", Message: err.Error()}
	}
	return Check{Status: "ok"}
}

// WorkerStatus returns a simple status endpoint for background workers.
func (h *HealthHandler) WorkerStatus(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"workers": []string{"salience-decay", "re-embedding", "expiry-cleanup", "consolidation"},
		"note":    "workers run on schedule; check logs for execution details",
	})
}
