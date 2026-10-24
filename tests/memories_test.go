package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreateMemory_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	body := mustJSON(t, map[string]interface{}{
		"content":     "User prefers Python for scripting",
		"memory_type": "PREFERENCE",
		"importance":  0.9,
		"confidence":  0.95,
		"namespace":   "CODING",
	})

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Status != domain.MemoryStatusNew {
		t.Errorf("expected status NEW for non-explicit create, got %s", resp.Data.Status)
	}
	if resp.Data.MemoryType != domain.MemoryTypePreference {
		t.Errorf("expected type PREFERENCE, got %s", resp.Data.MemoryType)
	}
	if resp.Data.Namespace != "CODING" {
		t.Errorf("expected namespace CODING, got %s", resp.Data.Namespace)
	}
}

func TestCreateMemory_ValidationError(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Empty content.
	body := mustJSON(t, map[string]interface{}{
		"content":     "",
		"memory_type": "FACT",
	})

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestCreateMemory_InvalidType(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	body := mustJSON(t, map[string]interface{}{
		"content":     "test content",
		"memory_type": "INVALID_TYPE",
	})

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rr.Code)
	}
}

// ── Remember (explicit ACTIVE) ────────────────────────────────────────────────

func TestRemember_CreatesActiveMemory(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	body := mustJSON(t, map[string]interface{}{
		"content":     "I prefer dark mode in all editors",
		"memory_type": "PREFERENCE",
		"importance":  1.0,
		"namespace":   "PERSONAL",
	})

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	// "remember this" must produce ACTIVE status immediately.
	if resp.Data.Status != domain.MemoryStatusActive {
		t.Errorf("expected ACTIVE status from remember, got %s", resp.Data.Status)
	}
}

func TestRemember_DefaultsToFact(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// No memory_type supplied.
	body := mustJSON(t, map[string]interface{}{
		"content": "The sky is blue",
	})

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.MemoryType != domain.MemoryTypeFact {
		t.Errorf("expected type FACT as default, got %s", resp.Data.MemoryType)
	}
}

// ── Get ───────────────────────────────────────────────────────────────────────

func TestGetMemory_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	mem := createTestMemory(t, router, user)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/%s?user_id=%s", mem.ID, user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestGetMemory_WrongUser(t *testing.T) {
	router := buildRouter(t)
	user1 := createTestUser(t, router)
	user2 := createTestUser(t, router)
	mem := createTestMemory(t, router, user1)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/%s?user_id=%s", mem.ID, user2.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for wrong user, got %d", rr.Code)
	}
}

func TestGetMemory_NotFound(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/00000000-0000-0000-0000-000000000001?user_id=%s", user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestListMemories(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create 3 memories.
	for i := 0; i < 3; i++ {
		createTestMemory(t, router, user)
	}

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories?page=1&page_size=10", user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Total int64            `json:"total"`
			Memories []*domain.Memory `json:"memories"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Total < 3 {
		t.Errorf("expected at least 3 memories, got %d", resp.Data.Total)
	}
}

func TestListMemories_FilterByType(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create a PREFERENCE memory via remember.
	body := mustJSON(t, map[string]interface{}{
		"content":     "I prefer Go over Java",
		"memory_type": "PREFERENCE",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup failed: %s", rr.Body.String())
	}

	// Filter by type=PREFERENCE.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/memories?type=PREFERENCE", user.ID),
		nil,
	)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Data struct {
			Memories []*domain.Memory `json:"memories"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	for _, m := range resp.Data.Memories {
		if m.MemoryType != domain.MemoryTypePreference {
			t.Errorf("expected PREFERENCE, got %s", m.MemoryType)
		}
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func TestUpdateMemory_MetadataOnly(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	mem := createTestMemory(t, router, user)

	newImportance := 0.95
	body := mustJSON(t, map[string]interface{}{
		"importance": newImportance,
	})

	req := httptest.NewRequest(http.MethodPatch,
		fmt.Sprintf("/api/v1/memories/%s?user_id=%s", mem.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Importance != newImportance {
		t.Errorf("expected importance %.2f, got %.2f", newImportance, resp.Data.Importance)
	}
	// Version should NOT change for metadata-only update.
	if resp.Data.Version != mem.Version {
		t.Errorf("expected same version %d, got %d", mem.Version, resp.Data.Version)
	}
}

func TestUpdateMemory_ContentCreatesNewVersion(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create ACTIVE memory via remember.
	body := mustJSON(t, map[string]interface{}{
		"content":     "I use VS Code",
		"memory_type": "PREFERENCE",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup failed: %s", rr.Body.String())
	}

	var createResp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &createResp)
	original := createResp.Data

	// Update content — should create new version.
	body = mustJSON(t, map[string]interface{}{
		"content": "I use Neovim now",
	})
	req = httptest.NewRequest(http.MethodPatch,
		fmt.Sprintf("/api/v1/memories/%s?user_id=%s", original.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var updateResp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &updateResp)
	newMem := updateResp.Data

	// New version should have incremented version number.
	if newMem.Version != original.Version+1 {
		t.Errorf("expected version %d, got %d", original.Version+1, newMem.Version)
	}
	// New version should point to the original as parent.
	if newMem.ParentID == nil || *newMem.ParentID != original.ID {
		t.Errorf("expected parent_id = %s, got %v", original.ID, newMem.ParentID)
	}
	// New version should be ACTIVE.
	if newMem.Status != domain.MemoryStatusActive {
		t.Errorf("expected new version to be ACTIVE, got %s", newMem.Status)
	}
	// Content should be updated.
	if newMem.Content != "I use Neovim now" {
		t.Errorf("expected updated content, got %q", newMem.Content)
	}
}

// ── Forget (single) ───────────────────────────────────────────────────────────

func TestForget_TransitionsToDeleted(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	mem := createTestMemory(t, router, user)

	body := mustJSON(t, map[string]string{"reason": "user requested"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/forget?user_id=%s", mem.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	// Verify memory is now DELETED (GetByID should still return it — DELETED is not removed).
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/%s?user_id=%s", mem.ID, user.ID),
		nil,
	)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var getResp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &getResp)

	if getResp.Data != nil && getResp.Data.Status != domain.MemoryStatusDeleted {
		t.Errorf("expected DELETED status, got %s", getResp.Data.Status)
	}
}

// ── Forget Namespace (bulk) ───────────────────────────────────────────────────

func TestForgetNamespace_BulkDelete(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	ns := fmt.Sprintf("TESTNS_%s", randSuffix())

	// Create 3 memories in the namespace.
	for i := 0; i < 3; i++ {
		body := mustJSON(t, map[string]interface{}{
			"content":     fmt.Sprintf("memory %d in namespace", i),
			"memory_type": "FACT",
			"namespace":   ns,
		})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
			body,
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("setup memory %d: %s", i, rr.Body.String())
		}
	}

	// Forget the entire namespace.
	body := mustJSON(t, map[string]interface{}{
		"namespace": ns,
		"reason":    "test cleanup",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/forget-namespace?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	deleted, ok := resp.Data["memories_deleted"]
	if !ok {
		t.Fatal("expected memories_deleted in response")
	}
	// JSON numbers decode as float64.
	if deleted.(float64) < 3 {
		t.Errorf("expected at least 3 deleted, got %v", deleted)
	}
}

// ── Lifecycle state machine ───────────────────────────────────────────────────

func TestLifecycle_ArchiveAndRestore(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create ACTIVE memory.
	body := mustJSON(t, map[string]interface{}{
		"content":     "lifecycle test memory",
		"memory_type": "FACT",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup: %s", rr.Body.String())
	}
	var createResp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &createResp)
	mem := createResp.Data

	// Archive it.
	body = mustJSON(t, map[string]string{"reason": "testing archive"})
	req = httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/archive?user_id=%s", mem.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("archive failed: %d — %s", rr.Code, rr.Body.String())
	}

	var archiveResp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &archiveResp)
	if archiveResp.Data.Status != domain.MemoryStatusArchived {
		t.Errorf("expected ARCHIVED, got %s", archiveResp.Data.Status)
	}

	// Restore it.
	req = httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/restore?user_id=%s", mem.ID, user.ID),
		nil,
	)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("restore failed: %d — %s", rr.Code, rr.Body.String())
	}

	var restoreResp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &restoreResp)
	if restoreResp.Data.Status != domain.MemoryStatusActive {
		t.Errorf("expected ACTIVE after restore, got %s", restoreResp.Data.Status)
	}
}

func TestLifecycle_InvalidTransition(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	mem := createTestMemory(t, router, user)

	// NEW memory cannot be restored directly (ARCHIVED → ACTIVE is valid; NEW → ACTIVE is valid but NEW → ARCHIVED is not).
	// Try to archive a NEW memory — this should fail (NEW → ARCHIVED is not in valid transitions).
	body := mustJSON(t, map[string]string{"reason": "invalid transition test"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/archive?user_id=%s", mem.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// NEW → ARCHIVED is invalid.
	if rr.Code == http.StatusOK {
		t.Error("expected non-200 for invalid transition NEW → ARCHIVED")
	}
}

func TestLifecycle_ActivateFromNew(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	mem := createTestMemory(t, router, user) // creates in NEW status

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/activate?user_id=%s", mem.ID, user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("activate failed: %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)
	if resp.Data.Status != domain.MemoryStatusActive {
		t.Errorf("expected ACTIVE, got %s", resp.Data.Status)
	}
}

// ── Audit Log ─────────────────────────────────────────────────────────────────

func TestAuditLog_RecordsCreation(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	mem := createTestMemory(t, router, user)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/%s/audit?user_id=%s", mem.ID, user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Entries []*domain.MemoryAuditEntry `json:"entries"`
			Total   int                        `json:"total"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Total < 1 {
		t.Error("expected at least 1 audit entry (CREATED)")
	}
	if resp.Data.Entries[0].Action != domain.AuditCreated {
		t.Errorf("expected first audit entry to be CREATED, got %s", resp.Data.Entries[0].Action)
	}
}

func TestAuditLog_RecordsStatusTransition(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create ACTIVE memory.
	body := mustJSON(t, map[string]interface{}{
		"content": "audit trail test",
		"memory_type": "FACT",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup: %s", rr.Body.String())
	}
	var cr struct{ Data *domain.Memory `json:"data"` }
	mustDecode(t, rr.Body, &cr)

	// Archive it.
	body = mustJSON(t, map[string]string{"reason": "audit test"})
	req = httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/%s/archive?user_id=%s", cr.Data.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("archive failed: %s", rr.Body.String())
	}

	// Check audit log has 2 entries: CREATED + STATUS_CHANGED.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/memories/%s/audit?user_id=%s", cr.Data.ID, user.ID),
		nil,
	)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var auditResp struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &auditResp)

	if auditResp.Data.Total < 2 {
		t.Errorf("expected at least 2 audit entries, got %d", auditResp.Data.Total)
	}
}

// ── Types endpoint ────────────────────────────────────────────────────────────

func TestMemoryTypesEndpoint(t *testing.T) {
	router := buildRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/memories/types", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if _, ok := resp.Data["memory_types"]; !ok {
		t.Error("expected memory_types in response")
	}
	if _, ok := resp.Data["statuses"]; !ok {
		t.Error("expected statuses in response")
	}
	if _, ok := resp.Data["lifecycle"]; !ok {
		t.Error("expected lifecycle in response")
	}
}

// ── Temporal validity ─────────────────────────────────────────────────────────

func TestCreateMemory_WithExpiry(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	expiry := time.Now().Add(24 * time.Hour)
	body := mustJSON(t, map[string]interface{}{
		"content":      "This memory expires tomorrow",
		"memory_type":  "WORKING",
		"valid_until":  expiry.Format(time.RFC3339),
		"retention_days": 1,
	})

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories/remember?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.ValidUntil == nil {
		t.Error("expected valid_until to be set")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// createTestMemory creates a NEW memory via the standard create endpoint.
func createTestMemory(t *testing.T, router http.Handler, user *domain.User) *domain.Memory {
	t.Helper()
	body := mustJSON(t, map[string]interface{}{
		"content":     fmt.Sprintf("test memory content %s", randSuffix()),
		"memory_type": "FACT",
		"importance":  0.5,
		"namespace":   "DEFAULT",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/memories?user_id=%s", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("createTestMemory: expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data *domain.Memory `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)
	if resp.Data == nil {
		t.Fatal("createTestMemory: data is nil")
	}
	return resp.Data
}
