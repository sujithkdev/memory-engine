package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
)

// ── Conversation CRUD tests ────────────────────────────────────────────────────

func TestCreateConversation_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	body := mustJSON(t, map[string]string{
		"title":     "Test Conversation",
		"namespace": "CODING",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/users/%s/conversations", user.ID),
		body,
	)
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

func TestCreateConversation_UserNotFound(t *testing.T) {
	router := buildRouter(t)

	body := mustJSON(t, map[string]string{"title": "Test"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/users/00000000-0000-0000-0000-000000000001/conversations",
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestListConversations(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)

	// Create 3 conversations.
	for i := 0; i < 3; i++ {
		createTestConversation(t, router, user)
	}

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/users/%s/conversations?page=1&page_size=10", user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestGetConversation_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	conv := createTestConversation(t, router, user)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/conversations/%s?user_id=%s", conv.ID, user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestGetConversation_WrongUser(t *testing.T) {
	router := buildRouter(t)
	user1 := createTestUser(t, router)
	user2 := createTestUser(t, router)
	conv := createTestConversation(t, router, user1)

	// user2 tries to access user1's conversation.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/conversations/%s?user_id=%s", conv.ID, user2.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for wrong user, got %d", rr.Code)
	}
}

func TestUpdateConversation(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	conv := createTestConversation(t, router, user)

	body := mustJSON(t, map[string]interface{}{
		"title":       "Updated Title",
		"is_archived": true,
	})
	req := httptest.NewRequest(http.MethodPatch,
		fmt.Sprintf("/api/v1/conversations/%s?user_id=%s", conv.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteConversation(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	conv := createTestConversation(t, router, user)

	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/conversations/%s?user_id=%s", conv.ID, user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// createTestConversation creates a conversation via the API.
func createTestConversation(t *testing.T, router http.Handler, user *domain.User) *domain.Conversation {
	t.Helper()
	body := mustJSON(t, map[string]string{
		"title":     fmt.Sprintf("conv_%s", randSuffix()),
		"namespace": "DEFAULT",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/users/%s/conversations", user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("createTestConversation: expected 201, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Success bool                 `json:"success"`
		Data    *domain.Conversation `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)
	if resp.Data == nil {
		t.Fatal("createTestConversation: data is nil")
	}
	return resp.Data
}
