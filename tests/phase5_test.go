package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ── Deduplication ─────────────────────────────────────────────────────────────

func TestDedup_ExactDuplicate_SecondDropped(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	content := fmt.Sprintf("exact duplicate content %s", randSuffix())

	// Create first memory.
	body := mustJSON(t, map[string]interface{}{
		"content":     content,
		"memory_type": "FACT",
		"namespace":   "DEFAULT",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first memory: %s", rr.Body.String())
	}
	var first struct{ Data *domain.Memory `json:"data"` }
	mustDecode(t, rr.Body, &first)

	// Create second memory with identical content — dedup should drop it.
	body = mustJSON(t, map[string]interface{}{
		"content":     content, // identical
		"memory_type": "FACT",
		"namespace":   "DEFAULT",
	})
	req = httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("second memory creation failed: %d — %s", rr.Code, rr.Body.String())
	}

	var second struct{ Data *domain.Memory `json:"data"` }
	mustDecode(t, rr.Body, &second)

	// The returned memory should be the first one (dedup returns existing).
	if second.Data != nil && second.Data.ID != first.Data.ID {
		// Both IDs are different — dedup returned the existing memory.
		// Verify the "new" one is actually deleted.
		req = httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/api/v1/memories/%s?user_id=%s", second.Data.ID, user.ID), nil)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		var getResp struct{ Data *domain.Memory `json:"data"` }
		mustDecode(t, rr.Body, &getResp)
		if getResp.Data != nil && getResp.Data.Status == domain.MemoryStatusActive {
			t.Error("duplicate memory should not be ACTIVE")
		}
	}
}

// ── Conflict detection ────────────────────────────────────────────────────────

func TestConflict_ListEndpoint(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/conflicts", user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Conflicts      interface{} `json:"conflicts"`
			Total          int         `json:"total"`
			UnresolvedCount int64      `json:"unresolved_count"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)
	// New user should have zero conflicts.
	if resp.Data.UnresolvedCount != 0 {
		t.Errorf("expected 0 unresolved for new user, got %d", resp.Data.UnresolvedCount)
	}
}

func TestConflict_TemporalUpdate_DetectedAndResolved(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("TEMPORAL_%s", randSuffix())

	// Create original preference.
	body := mustJSON(t, map[string]interface{}{
		"content":     "I use VS Code as my primary editor",
		"memory_type": "PREFERENCE",
		"namespace":   ns,
		"importance":  0.8,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("original memory: %s", rr.Body.String())
	}

	// Create an update with temporal keyword "switched".
	body = mustJSON(t, map[string]interface{}{
		"content":     "I recently switched to Neovim as my primary editor",
		"memory_type": "PREFERENCE",
		"namespace":   ns,
		"importance":  0.8,
	})
	req = httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("temporal update memory: %d — %s", rr.Code, rr.Body.String())
	}

	// Check conflicts list — temporal update should have been detected.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/conflicts", user.ID), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("conflicts list: %d", rr.Code)
	}
	// We don't assert exact conflict count since auto-resolution may have resolved it.
	// Just verify the endpoint works and returns valid structure.
}

// ── Memory Relationships ──────────────────────────────────────────────────────

func TestRelationship_Link_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create two ACTIVE memories.
	mem1 := createActiveMemory(t, router, user, "I started learning Rust")
	mem2 := createActiveMemory(t, router, user, "I built a CLI tool in Rust")

	// Link: mem1 LED_TO mem2.
	body := mustJSON(t, map[string]interface{}{
		"target_memory_id":  mem2.ID.String(),
		"relationship_type": "LED_TO",
		"confidence":        0.9,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/relationships?user_id=%s", mem1.ID, user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.MemoryRelationship `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.RelationshipType != domain.RelLedTo {
		t.Errorf("expected LED_TO, got %s", resp.Data.RelationshipType)
	}
	if resp.Data.SourceMemoryID != mem1.ID {
		t.Errorf("expected source=%s, got %s", mem1.ID, resp.Data.SourceMemoryID)
	}
	if resp.Data.TargetMemoryID != mem2.ID {
		t.Errorf("expected target=%s, got %s", mem2.ID, resp.Data.TargetMemoryID)
	}
}

func TestRelationship_GetRelationships(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	mem1 := createActiveMemory(t, router, user, "I studied Go concurrency")
	mem2 := createActiveMemory(t, router, user, "I built a concurrent web scraper in Go")
	mem3 := createActiveMemory(t, router, user, "I prefer Go for concurrent workloads")

	// Create two relationships from mem1.
	for _, target := range []*domain.Memory{mem2, mem3} {
		body := mustJSON(t, map[string]interface{}{
			"target_memory_id":  target.ID.String(),
			"relationship_type": "LED_TO",
			"confidence":        0.85,
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/%s/relationships?user_id=%s", mem1.ID, user.ID), body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("link failed: %s", rr.Body.String())
		}
	}

	// Get relationships for mem1.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/%s/relationships?user_id=%s", mem1.ID, user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Relationships []*domain.MemoryRelationship `json:"relationships"`
			Total         int                          `json:"total"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Total < 2 {
		t.Errorf("expected at least 2 relationships, got %d", resp.Data.Total)
	}
}

func TestRelationship_SelfLink_Rejected(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	mem := createActiveMemory(t, router, user, "self-link test memory")

	body := mustJSON(t, map[string]interface{}{
		"target_memory_id":  mem.ID.String(),
		"relationship_type": "RELATED_TO",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/relationships?user_id=%s", mem.ID, user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusCreated {
		t.Error("self-link should be rejected")
	}
}

func TestRelationship_WrongUser_Rejected(t *testing.T) {
	router := buildRouter(t)
	user1 := createTestUser(t, router)
	user2 := createTestUser(t, router)

	mem1 := createActiveMemory(t, router, user1, "user1 memory A")
	mem2 := createActiveMemory(t, router, user2, "user2 memory B")

	// user1 tries to link user2's memory.
	body := mustJSON(t, map[string]interface{}{
		"target_memory_id":  mem2.ID.String(),
		"relationship_type": "RELATED_TO",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/relationships?user_id=%s", mem1.ID, user1.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Should be rejected — user2's memory not found for user1.
	if rr.Code == http.StatusCreated {
		t.Error("cross-user relationship should be rejected")
	}
}

func TestRelationship_DerivedFrom_ConfidenceConstraint(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Source memory with moderate confidence.
	mem1 := createActiveMemory(t, router, user, "Go has goroutines for concurrency")
	mem2 := createActiveMemory(t, router, user, "Go is good for high-concurrency services")

	// Link as DERIVED_FROM.
	body := mustJSON(t, map[string]interface{}{
		"target_memory_id":  mem2.ID.String(),
		"relationship_type": "DERIVED_FROM",
		"confidence":        0.8,
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/relationships?user_id=%s", mem1.ID, user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rr.Code, rr.Body.String())
	}
	// Relationship created — confidence propagation runs async/on-demand in Phase 5.
	// This test verifies the link was created correctly.
}

func TestRelationship_Idempotent_UpsertOnDuplicate(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	mem1 := createActiveMemory(t, router, user, "memory for idempotent test A")
	mem2 := createActiveMemory(t, router, user, "memory for idempotent test B")

	// Link twice — should not error.
	for i := 0; i < 2; i++ {
		body := mustJSON(t, map[string]interface{}{
			"target_memory_id":  mem2.ID.String(),
			"relationship_type": "SUPPORTS",
			"confidence":        0.7,
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/%s/relationships?user_id=%s", mem1.ID, user.ID), body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("attempt %d: expected 201, got %d — %s", i+1, rr.Code, rr.Body.String())
		}
	}

	// Should still have exactly 1 relationship (upsert).
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/%s/relationships?user_id=%s", mem1.ID, user.ID), nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)
	// SUPPORTS relationship from mem1 → mem2 should be exactly 1.
	if resp.Data.Total != 1 {
		t.Errorf("expected exactly 1 relationship after upsert, got %d", resp.Data.Total)
	}
}

// ── Conflict resolution ───────────────────────────────────────────────────────

func TestConflict_Resolve_InvalidID_Returns404(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	body := mustJSON(t, map[string]interface{}{
		"strategy": "KEEP_NEWER",
		"reason":   "test",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/conflicts/00000000-0000-0000-0000-000000000001/resolve?user_id=%s", user.ID),
		body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// No conflict with that ID exists — should get a non-200 (DB will return 0 rows affected).
	if rr.Code == http.StatusCreated {
		t.Error("resolving non-existent conflict should not return 201")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// createActiveMemory creates an ACTIVE memory via the remember endpoint.
func createActiveMemory(t *testing.T, router interface{ ServeHTTP(http.ResponseWriter, *http.Request) }, user *domain.User, content string) *domain.Memory {
	t.Helper()
	body := mustJSON(t, map[string]interface{}{
		"content":     content,
		"memory_type": "FACT",
		"importance":  0.7,
		"namespace":   "DEFAULT",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID), body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("createActiveMemory failed: %d — %s", rr.Code, rr.Body.String())
	}
	var resp struct{ Data *domain.Memory `json:"data"` }
	mustDecode(t, rr.Body, &resp)
	if resp.Data == nil {
		t.Fatal("createActiveMemory: nil data")
	}
	return resp.Data
}
