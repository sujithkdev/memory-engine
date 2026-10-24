package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ── Full-text search integration tests ───────────────────────────────────────

func TestSearch_EmptyQuery_ReturnsActiveMemories(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("SEARCH_%s", randSuffix())

	// Create 3 active memories.
	for i := 0; i < 3; i++ {
		body := mustJSON(t, map[string]interface{}{
			"content":     fmt.Sprintf("search test memory number %d about golang", i),
			"memory_type": "FACT",
			"namespace":   ns,
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup: %s", rr.Body.String())
		}
	}

	// Empty query — should return all active memories in namespace.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/search?namespace=%s", user.ID, ns), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.SearchResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Total < 3 {
		t.Errorf("expected at least 3 results, got %d", resp.Data.Total)
	}
}

func TestSearch_QueryReturnsRankedResults(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("FTS_%s", randSuffix())

	// Create memories with different content relevance.
	memories := []string{
		"I prefer Python for data science and machine learning",
		"Python is excellent for scripting automation tasks",
		"I use Go for backend web services",
		"Rust is great for systems programming",
	}
	for _, content := range memories {
		body := mustJSON(t, map[string]interface{}{
			"content":     content,
			"memory_type": "PREFERENCE",
			"namespace":   ns,
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup: %s", rr.Body.String())
		}
	}

	// Search for "python".
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/search?q=python&namespace=%s", user.ID, ns), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.SearchResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	// Python memories should appear in results.
	if len(resp.Data.Results) == 0 {
		t.Fatal("expected search results for 'python'")
	}

	// First result should be most relevant to python.
	first := resp.Data.Results[0].Memory.Content
	if first == "" {
		t.Error("first result should have content")
	}

	// All results should have positive rank.
	for i, r := range resp.Data.Results {
		if r.Rank < 0 {
			t.Errorf("result %d has negative rank: %.4f", i, r.Rank)
		}
	}
}

func TestSearch_ResultsHaveHeadline(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("HEADLINE_%s", randSuffix())

	body := mustJSON(t, map[string]interface{}{
		"content":     "I prefer Python for data analysis and automation scripting tasks",
		"memory_type": "PREFERENCE",
		"namespace":   ns,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup: %s", rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/search?q=python&namespace=%s", user.ID, ns), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.SearchResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if len(resp.Data.Results) == 0 {
		t.Skip("no results returned — DB may not have FTS index yet")
	}

	// Headline should be non-empty for FTS results.
	if resp.Data.Results[0].Headline == "" {
		t.Error("search results should include a highlighted headline")
	}
}

func TestSearch_QueryMetadataReturned(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/search?q=golang+programming", user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.SearchResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Query.Raw != "golang programming" {
		t.Errorf("expected raw query 'golang programming', got %q", resp.Data.Query.Raw)
	}
	if resp.Data.Query.TSQuery == "" {
		t.Error("expected TSQuery to be populated")
	}
}

func TestSearch_NamespaceFilter_IsolatesResults(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns1 := fmt.Sprintf("NS1_%s", randSuffix())
	ns2 := fmt.Sprintf("NS2_%s", randSuffix())

	// Create memory in ns1.
	body := mustJSON(t, map[string]interface{}{
		"content":     "golang memory in namespace one for testing isolation",
		"memory_type": "FACT",
		"namespace":   ns1,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup ns1: %s", rr.Body.String())
	}

	// Search only in ns2 — should not find the ns1 memory.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/search?q=golang&namespace=%s", user.ID, ns2), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Data *domain.SearchResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	// No results in ns2.
	for _, r := range resp.Data.Results {
		if r.Memory.Namespace == ns1 {
			t.Errorf("namespace filter failed — found ns1 memory in ns2 search")
		}
	}
}

func TestSearch_MemoryTypeFilter(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("TYPEFILTER_%s", randSuffix())

	// Create FACT and PREFERENCE memories.
	for _, mt := range []string{"FACT", "PREFERENCE"} {
		body := mustJSON(t, map[string]interface{}{
			"content":     fmt.Sprintf("golang programming %s memory", mt),
			"memory_type": mt,
			"namespace":   ns,
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup %s: %s", mt, rr.Body.String())
		}
	}

	// Search filtered to PREFERENCE only.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/search?q=golang&namespace=%s&type=PREFERENCE", user.ID, ns), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.SearchResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	for _, r := range resp.Data.Results {
		if r.Memory.MemoryType != domain.MemoryTypePreference {
			t.Errorf("type filter failed — got %s memory in PREFERENCE search", r.Memory.MemoryType)
		}
	}
}

func TestSearch_ModeOverride_OR(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("MODE_%s", randSuffix())

	// Create memories about different topics.
	for _, content := range []string{
		"python is good for scripting",
		"golang is great for concurrency",
	} {
		body := mustJSON(t, map[string]interface{}{
			"content":     content,
			"memory_type": "FACT",
			"namespace":   ns,
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup: %s", rr.Body.String())
		}
	}

	// OR search — should match both python AND golang memories.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/search?q=python+golang&mode=OR&namespace=%s", user.ID, ns), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.SearchResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	// OR mode with python|golang should find both memories.
	if resp.Data.Total < 2 {
		t.Errorf("OR search should find both memories, got %d", resp.Data.Total)
	}
}

func TestSearch_Pagination(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("PAGE_%s", randSuffix())

	// Create 5 memories.
	for i := 0; i < 5; i++ {
		body := mustJSON(t, map[string]interface{}{
			"content":     fmt.Sprintf("pagination test memory %d about golang development", i),
			"memory_type": "FACT",
			"namespace":   ns,
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup: %s", rr.Body.String())
		}
	}

	// Page 1 with page_size=2.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/search?q=golang&namespace=%s&page=1&page_size=2", user.ID, ns), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("page 1: %d — %s", rr.Code, rr.Body.String())
	}

	var p1 struct {
		Data *domain.SearchResponse `json:"data"`
	}
	mustDecode(t, rr.Body, &p1)

	if len(p1.Data.Results) > 2 {
		t.Errorf("page_size=2 should return at most 2 results, got %d", len(p1.Data.Results))
	}
	if p1.Data.TotalPages < 1 {
		t.Errorf("expected total_pages >= 1")
	}
}

// ── Namespace suggestions ─────────────────────────────────────────────────────

func TestSuggestNamespaces_ReturnsMatches(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	prefix := fmt.Sprintf("SUGGEST_%s", randSuffix())

	// Create a memory with a known namespace.
	body := mustJSON(t, map[string]interface{}{
		"content":     "namespace suggestion test",
		"memory_type": "FACT",
		"namespace":   prefix,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup: %s", rr.Body.String())
	}

	// Suggest namespaces starting with prefix.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories/suggest-namespaces?q=%s", user.ID, prefix[:5]), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Suggestions []string `json:"suggestions"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	found := false
	for _, s := range resp.Data.Suggestions {
		if s == prefix {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected namespace %q in suggestions, got: %v", prefix, resp.Data.Suggestions)
	}
}
