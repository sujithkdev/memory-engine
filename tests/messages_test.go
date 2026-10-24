package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ── Message CRUD tests ────────────────────────────────────────────────────────

func TestCreateMessage_Success(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	conv := createTestConversation(t, router, user)

	body := mustJSON(t, map[string]string{
		"role":    "user",
		"content": "Hello, this is a test message.",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/conversations/%s/messages?user_id=%s", conv.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestCreateMessage_InvalidRole(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	conv := createTestConversation(t, router, user)

	body := mustJSON(t, map[string]string{
		"role":    "invalidrole",
		"content": "Test",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/conversations/%s/messages?user_id=%s", conv.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestCreateMessage_EmptyContent(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	conv := createTestConversation(t, router, user)

	body := mustJSON(t, map[string]string{
		"role":    "user",
		"content": "",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/conversations/%s/messages?user_id=%s", conv.ID, user.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for empty content, got %d", rr.Code)
	}
}

func TestMessageSequenceOrder(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	conv := createTestConversation(t, router, user)

	// Create 3 messages.
	roles := []string{"user", "assistant", "user"}
	contents := []string{"Hello", "Hi there!", "How are you?"}
	for i, role := range roles {
		body := mustJSON(t, map[string]string{"role": role, "content": contents[i]})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/conversations/%s/messages?user_id=%s", conv.ID, user.ID),
			body,
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("creating message %d failed: %s", i, rr.Body.String())
		}
	}

	// List and verify order.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/conversations/%s/messages?user_id=%s", conv.ID, user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Messages []*domain.Message `json:"messages"`
			Total    int64             `json:"total"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)

	if resp.Data.Total < 3 {
		t.Errorf("expected at least 3 messages, got %d", resp.Data.Total)
	}

	// Verify sequence numbers are ascending.
	msgs := resp.Data.Messages
	for i := 1; i < len(msgs); i++ {
		if msgs[i].SequenceNumber <= msgs[i-1].SequenceNumber {
			t.Errorf("messages not in sequence order at index %d", i)
		}
	}
}

func TestGetMessage_WrongUser(t *testing.T) {
	router := buildRouter(t)
	user1 := createTestUser(t, router)
	user2 := createTestUser(t, router)
	conv := createTestConversation(t, router, user1)

	// Create message as user1.
	body := mustJSON(t, map[string]string{"role": "user", "content": "secret"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/conversations/%s/messages?user_id=%s", conv.ID, user1.ID),
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup failed: %s", rr.Body.String())
	}

	var createResp struct {
		Data *domain.Message `json:"data"`
	}
	mustDecode(t, rr.Body, &createResp)
	msgID := createResp.Data.ID

	// user2 tries to access user1's message.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/messages/%s?user_id=%s", msgID, user2.ID),
		nil,
	)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for wrong user, got %d", rr.Code)
	}
}

func TestListMessages_FilterByRole(t *testing.T) {
	router := buildRouter(t)
	user := createTestUser(t, router)
	conv := createTestConversation(t, router, user)

	// Create user + assistant messages.
	for _, role := range []string{"user", "assistant", "user"} {
		body := mustJSON(t, map[string]string{"role": role, "content": "test " + role})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/conversations/%s/messages?user_id=%s", conv.ID, user.ID),
			body,
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
	}

	// Filter by role=user.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/conversations/%s/messages?user_id=%s&role=user", conv.ID, user.ID),
		nil,
	)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data struct {
			Messages []*domain.Message `json:"messages"`
		} `json:"data"`
	}
	mustDecode(t, rr.Body, &resp)
	for _, m := range resp.Data.Messages {
		if m.Role != "user" {
			t.Errorf("expected role=user, got %s", m.Role)
		}
	}
}
