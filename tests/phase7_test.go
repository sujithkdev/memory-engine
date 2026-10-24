package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ── Embedding info ────────────────────────────────────────────────────────────

func TestEmbeddingInfo_Endpoint(t *testing.T) {
	router := buildRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/embeddings/info", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Model      string `json:"model"`
			Dimensions int    `json:"dimensions"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Model == "" {
		t.Error("expected model name in response")
	}
	if resp.Data.Dimensions <= 0 {
		t.Errorf("expected positive dimensions, got %d", resp.Data.Dimensions)
	}
}

// ── On-demand embedding ───────────────────────────────────────────────────────

func TestEmbedMemory_OnDemand(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	mem := createActiveMemory(t, router, user, "I prefer Go for high-performance services")

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/embed?user_id=%s", mem.ID, user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if _, ok := resp.Data["model"]; !ok {
		t.Error("expected 'model' in embed response")
	}
	if _, ok := resp.Data["memory_id"]; !ok {
		t.Error("expected 'memory_id' in embed response")
	}
}

func TestEmbedMemory_InvalidID_Returns400(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/not-a-uuid/embed?user_id=%s", user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid UUID, got %d", rr.Code)
	}
}

// ── Vector search ─────────────────────────────────────────────────────────────

func TestVectorSearch_EmptyQuery_Returns400(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/vector-search", user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty query, got %d", rr.Code)
	}
}

func TestVectorSearch_WithQuery_ReturnsValidResponse(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("VECTOR_%s", randSuffix())

	// Create and embed memories.
	for _, content := range []string{
		"Python is great for machine learning and data science",
		"Go is excellent for building web services",
		"Rust provides memory safety without garbage collection",
	} {
		mem := createActiveMemoryNS(t, router, user, content, ns)

		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/%s/embed?user_id=%s", mem.ID, user.ID), nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("embed failed: %s", rr.Body.String())
		}
	}

	// Vector search.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/vector-search?q=machine+learning&namespace=%s", user.ID, ns),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Results []domain.ScoredMemory `json:"results"`
			Total   int                   `json:"total"`
			Model   string                `json:"model"`
			Query   string                `json:"query"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Model == "" {
		t.Error("expected model name in vector search response")
	}
	if resp.Data.Query != "machine learning" {
		t.Errorf("expected query='machine learning', got %q", resp.Data.Query)
	}
}

func TestVectorSearch_NamespaceIsolation(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns1 := fmt.Sprintf("VS1_%s", randSuffix())
	ns2 := fmt.Sprintf("VS2_%s", randSuffix())

	// Create and embed in ns1.
	mem := createActiveMemoryNS(t, router, user, "deep learning neural networks tensorflow", ns1)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/embed?user_id=%s", mem.ID, user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("embed: %s", rr.Body.String())
	}

	// Search in ns2 — should find nothing from ns1.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/vector-search?q=deep+learning&namespace=%s", user.ID, ns2),
		nil,
	)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Data struct {
			Results []domain.ScoredMemory `json:"results"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	for _, r := range resp.Data.Results {
		if r.Memory != nil && r.Memory.Namespace == ns1 {
			t.Error("namespace isolation failed — ns1 memory appeared in ns2 vector search")
		}
	}
}

func TestVectorSearch_TopK_Respected(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("TOPK_%s", randSuffix())

	// Create and embed 5 memories.
	for i := 0; i < 5; i++ {
		mem := createActiveMemoryNS(t, router, user,
			fmt.Sprintf("programming languages memory number %d golang python rust", i), ns)
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/%s/embed?user_id=%s", mem.ID, user.ID), nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("embed %d: %s", i, rr.Body.String())
		}
	}

	// Request top_k=2.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/vector-search?q=programming&namespace=%s&top_k=2", user.ID, ns),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Results []domain.ScoredMemory `json:"results"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if len(resp.Data.Results) > 2 {
		t.Errorf("top_k=2 should return at most 2 results, got %d", len(resp.Data.Results))
	}
}

// ── helper ────────────────────────────────────────────────────────────────────

// createActiveMemoryNS creates an ACTIVE memory in a specific namespace.
func createActiveMemoryNS(t *testing.T, router interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, user *domain.User, content, namespace string) *domain.Memory {
	t.Helper()
	body := mustJSON(t, map[string]interface{}{
		"content":     content,
		"memory_type": "FACT",
		"importance":  0.7,
		"namespace":   namespace,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("createActiveMemoryNS failed: %d — %s", rr.Code, rr.Body.String())
	}
	var resp struct{ Data *domain.Memory `json:"data"` }
	mustDecode(t, rr.Body, &resp)
	return resp.Data
}
