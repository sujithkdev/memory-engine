// Package tests contains integration tests for the memory-engine API.
// These tests require a running PostgreSQL instance.
// Run with: go test ./tests/... -v
package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/api"
	"github.com/memory-engine/memory-engine/internal/config"
	"github.com/memory-engine/memory-engine/internal/database"
)

// testDB is a package-level DB used across tests.
// Initialized once via TestMain.
var testDB *database.DB

func TestMain(m *testing.M) {
	// Allow overriding DB config via environment for CI.
	cfg := config.DatabaseConfig{
		Host:              getEnv("TEST_DB_HOST", "localhost"),
		Port:              5432,
		Name:              getEnv("TEST_DB_NAME", "memory_engine_test"),
		User:              getEnv("TEST_DB_USER", "postgres"),
		Password:          getEnv("TEST_DB_PASSWORD", "postgres"),
		SSLMode:           "disable",
		MaxConns:          5,
		MinConns:          1,
		MaxConnLifetime:   1 * time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: 1 * time.Minute,
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	testDB, err = database.New(ctx, cfg, logger)
	if err != nil {
		// Skip tests if DB is unavailable — don't fail the build.
		// In CI, ensure DB is available.
		logger.Warn().Err(err).Msg("database unavailable — skipping integration tests")
		os.Exit(0)
	}
	defer testDB.Close()

	os.Exit(m.Run())
}

// ── /health tests ─────────────────────────────────────────────────────────────

func TestLivenessEndpoint(t *testing.T) {
	handler := api.NewHealthHandler(testDB, "memory-engine", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handler.Live(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp api.HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", resp.Status)
	}
	if resp.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", resp.Version)
	}
	if resp.Name != "memory-engine" {
		t.Errorf("expected name 'memory-engine', got %q", resp.Name)
	}
}

func TestReadinessEndpoint_DatabaseHealthy(t *testing.T) {
	handler := api.NewHealthHandler(testDB, "memory-engine", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()

	handler.Ready(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	var resp api.HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", resp.Status)
	}
	if resp.Checks["database"].Status != "ok" {
		t.Errorf("expected database check 'ok', got %q", resp.Checks["database"].Status)
	}
}

// ── /api/v1 tests ─────────────────────────────────────────────────────────────

func TestAPIRootEndpoint(t *testing.T) {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

	router := api.NewRouter(api.RouterConfig{
		DB:      testDB,
		Logger:  logger,
		Version: "1.0.0",
		Name:    "memory-engine",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp api.Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if !resp.Success {
		t.Error("expected success=true")
	}
}

func TestNotFoundReturnsJSON(t *testing.T) {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

	router := api.NewRouter(api.RouterConfig{
		DB:      testDB,
		Logger:  logger,
		Version: "1.0.0",
		Name:    "memory-engine",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType == "" {
		t.Error("expected Content-Type header")
	}

	var resp api.Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding 404 response: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false for 404")
	}
}

func TestContentTypeHeader(t *testing.T) {
	handler := api.NewHealthHandler(testDB, "memory-engine", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handler.Live(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header to be set")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
