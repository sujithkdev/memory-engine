package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ── POST /api/v1/retrieve ─────────────────────────────────────────────────────

func TestRetrieve_POST_BasicQuery(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("HYBRID_%s", randSuffix())

	// Create and embed memories.
	for _, content := range []string{
		"I prefer Python for machine learning tasks and data analysis",
		"Go is my primary language for backend web services",
		"I use PostgreSQL for all production databases",
		"My trading strategy uses XAUUSD with technical analysis",
	} {
		mem := createActiveMemoryNS(t, router, user, content, ns)
		embedMemory(t, router, user, mem)
	}

	body := mustJSON(t, map[string]interface{}{
		"user_id":                 user.ID.String(),
		"query":                   "python machine learning",
		"namespace":               ns,
		"top_k":                   3,
		"max_tokens":              2000,
		"include_score_breakdown": true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.RetrievalResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data == nil {
		t.Fatal("nil retrieval response")
	}
	if resp.Data.Query != "python machine learning" {
		t.Errorf("expected query='python machine learning', got %q", resp.Data.Query)
	}
	if resp.Data.TokenBudget != 2000 {
		t.Errorf("expected token_budget=2000, got %d", resp.Data.TokenBudget)
	}
	if resp.Data.TokensUsed < 0 {
		t.Error("tokens_used should be non-negative")
	}
}

func TestRetrieve_POST_EmptyQuery_Returns400(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	body := mustJSON(t, map[string]interface{}{
		"user_id": user.ID.String(),
		"query":   "",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty query, got %d", rr.Code)
	}
}

func TestRetrieve_POST_MissingUserID_Returns400(t *testing.T) {
	router := buildRouter(t)

	body := mustJSON(t, map[string]interface{}{
		"query": "python",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing user_id, got %d", rr.Code)
	}
}

// ── GET /api/v1/users/:userID/retrieve ───────────────────────────────────────

func TestRetrieve_GET_ConvenienceEndpoint(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("GETRET_%s", randSuffix())

	mem := createActiveMemoryNS(t, router, user, "golang concurrency goroutines channels", ns)
	embedMemory(t, router, user, mem)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/retrieve?q=golang+concurrency&namespace=%s", user.ID, ns),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.RetrievalResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data == nil {
		t.Fatal("nil response")
	}
	if resp.Data.Query != "golang concurrency" {
		t.Errorf("expected query='golang concurrency', got %q", resp.Data.Query)
	}
}

func TestRetrieve_GET_EmptyQuery_Returns400(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/retrieve", user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// ── Response structure ────────────────────────────────────────────────────────

func TestRetrieve_ResponseHasTrace(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	body := mustJSON(t, map[string]interface{}{
		"user_id": user.ID.String(),
		"query":   "golang programming",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.RetrievalResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Trace == nil {
		t.Error("expected trace in retrieval response")
	}
	if resp.Data.Trace.QueryType == "" {
		t.Error("expected query_type in trace")
	}
}

func TestRetrieve_TopKRespected(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("TOPKRET_%s", randSuffix())

	// Create 6 memories.
	for i := 0; i < 6; i++ {
		mem := createActiveMemoryNS(t, router, user,
			fmt.Sprintf("golang programming memory number %d for testing top k retrieval", i), ns)
		embedMemory(t, router, user, mem)
	}

	body := mustJSON(t, map[string]interface{}{
		"user_id":   user.ID.String(),
		"query":     "golang programming",
		"namespace": ns,
		"top_k":     3,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.RetrievalResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if len(resp.Data.Results) > 3 {
		t.Errorf("top_k=3 should return at most 3 results, got %d", len(resp.Data.Results))
	}
}

func TestRetrieve_TokenBudgetEnforced(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("BUDGET_%s", randSuffix())

	// Create several memories with long content.
	for i := 0; i < 5; i++ {
		content := fmt.Sprintf("golang memory %d: "+
			"this is a fairly long piece of content about golang programming that contains "+
			"many words about goroutines channels concurrency patterns interfaces structs "+
			"methods receivers pointers slices maps error handling context packages modules", i)
		mem := createActiveMemoryNS(t, router, user, content, ns)
		embedMemory(t, router, user, mem)
	}

	// Very tight token budget.
	body := mustJSON(t, map[string]interface{}{
		"user_id":    user.ID.String(),
		"query":      "golang goroutines",
		"namespace":  ns,
		"top_k":      10,
		"max_tokens": 50, // very tight
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.RetrievalResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.TokensUsed > 50 {
		t.Errorf("tokens_used %d exceeds budget of 50", resp.Data.TokensUsed)
	}
}

func TestRetrieve_ScoreBreakdownIncluded(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("BREAKDOWN_%s", randSuffix())

	mem := createActiveMemoryNS(t, router, user, "python machine learning tensorflow", ns)
	embedMemory(t, router, user, mem)

	body := mustJSON(t, map[string]interface{}{
		"user_id":                 user.ID.String(),
		"query":                   "python machine learning",
		"namespace":               ns,
		"include_score_breakdown": true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.RetrievalResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	for i, r := range resp.Data.Results {
		if r.ScoreBreakdown == nil {
			t.Errorf("result %d missing score_breakdown when include_score_breakdown=true", i)
		}
	}
}

func TestRetrieve_ResultsHaveSourceTags(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("SOURCES_%s", randSuffix())

	mem := createActiveMemoryNS(t, router, user, "rust memory safety ownership borrowing", ns)
	embedMemory(t, router, user, mem)

	body := mustJSON(t, map[string]interface{}{
		"user_id":   user.ID.String(),
		"query":     "rust memory safety",
		"namespace": ns,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retrieve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.RetrievalResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	for i, r := range resp.Data.Results {
		if len(r.Sources) == 0 {
			t.Errorf("result %d has no source tags", i)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// embedMemory triggers on-demand embedding for a memory.
func embedMemory(t *testing.T, router interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, user *domain.User, mem *domain.Memory) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/embed?user_id=%s", mem.ID, user.ID),
		bytes.NewReader([]byte{}),
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("embedMemory failed: %d — %s", rr.Code, rr.Body.String())
	}
}

// encodeJSON is a helper for encoding body (used for POST /retrieve).
func encodeJSON(v interface{}) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}
