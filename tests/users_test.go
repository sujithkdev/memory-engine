package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/api"
	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
)

// counter generates unique suffixes for test names within a test run.
var counter int64

func randSuffix() string {
	n := atomic.AddInt64(&counter, 1)
	return fmt.Sprintf("%d", n)
}

// buildRouter creates a test router with the live test DB.
func buildRouter(t *testing.T) http.Handler {
	t.Helper()
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	deps := api.NewDeps(testDB.Pool)
	return api.NewRouter(api.RouterConfig{
		DB:      testDB,
		Logger:  logger,
		Version: "test",
		Name:    "memory-engine-test",
		Deps:    deps,
	})
}

// ── User CRUD tests ────────────────────────────────────────────────────────────

func TestCreateUser_Success(t *testing.T) {
	router := buildRouter(t)

	body := mustJSON(t, map[string]string{
		"username": fmt.Sprintf("testuser_%s", randSuffix()),
		"email":    "test@example.com",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp httputil.Response
	mustDecode(t, rr.Body, &resp)
	if !resp.Success {
		t.Error("expected success=true")
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	router := buildRouter(t)
	username := fmt.Sprintf("dupuser_%s", randSuffix())

	// First create.
	body := mustJSON(t, map[string]string{"username": username})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first create failed: %s", rr.Body.String())
	}

	// Duplicate.
	body = mustJSON(t, map[string]string{"username": username})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestCreateUser_ValidationError(t *testing.T) {
	router := buildRouter(t)

	// Username too short (< 3 chars).
	body := mustJSON(t, map[string]string{"username": "ab"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestGetUser_NotFound(t *testing.T) {
	router := buildRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/00000000-0000-0000-0000-000000000001", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestGetUser_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+user.ID.String(), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateUser_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	body := mustJSON(t, map[string]string{"display_name": "Updated Display Name"})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+user.ID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteUser_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+user.ID.String(), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestListUsers(t *testing.T) {
	router := buildRouter(t)
	createTestUser(t, router)
	createTestUser(t, router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?page=1&page_size=10", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// createTestUser creates a user via the API and returns the domain object.
func createTestUser(t *testing.T, router http.Handler) *domain.User {
	t.Helper()
	body := mustJSON(t, map[string]string{
		"username": fmt.Sprintf("user_%s", randSuffix()),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("createTestUser: expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Success bool         `json:"success"`
		Data    *domain.User `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)
	if resp.Data == nil {
		t.Fatal("createTestUser: data is nil")
	}
	return resp.Data
}

func mustJSON(t *testing.T, v interface{}) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return bytes.NewBuffer(b)
}

func mustDecode(t *testing.T, body *bytes.Buffer, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(v); err != nil {
		t.Fatalf("mustDecode: %v", err)
	}
}
