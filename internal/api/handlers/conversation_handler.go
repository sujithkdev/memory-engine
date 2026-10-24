package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
	"github.com/memory-engine/memory-engine/internal/service"
)

// ConversationHandler handles conversation HTTP requests.
type ConversationHandler struct {
	svc *service.ConversationService
}

// NewConversationHandler creates a new ConversationHandler.
func NewConversationHandler(svc *service.ConversationService) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// Create handles POST /api/v1/users/:userID/conversations
func (h *ConversationHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	var input domain.CreateConversationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}
	input.UserID = userID

	conv, err := h.svc.Create(r.Context(), input)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Created(w, conv)
}

// Get handles GET /api/v1/conversations/:convID
func (h *ConversationHandler) Get(w http.ResponseWriter, r *http.Request) {
	convID, err := parseUUID(chi.URLParam(r, "convID"))
	if err != nil {
		httputil.BadRequest(w, "invalid conversation id")
		return
	}

	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	conv, err := h.svc.GetByID(r.Context(), convID, userID)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, conv)
}

// List handles GET /api/v1/users/:userID/conversations
func (h *ConversationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	filter := domain.ConversationFilter{
		UserID:   userID,
		Page:     parseIntQuery(r, "page", 1),
		PageSize: parseIntQuery(r, "page_size", 20),
	}

	if ns := r.URL.Query().Get("namespace"); ns != "" {
		filter.Namespace = &ns
	}

	if archivedStr := r.URL.Query().Get("archived"); archivedStr != "" {
		archived := archivedStr == "true" || archivedStr == "1"
		filter.IsArchived = &archived
	}

	list, err := h.svc.List(r.Context(), filter)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, list)
}

// Update handles PATCH /api/v1/conversations/:convID
func (h *ConversationHandler) Update(w http.ResponseWriter, r *http.Request) {
	convID, err := parseUUID(chi.URLParam(r, "convID"))
	if err != nil {
		httputil.BadRequest(w, "invalid conversation id")
		return
	}

	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	var input domain.UpdateConversationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}

	conv, err := h.svc.Update(r.Context(), convID, userID, input)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, conv)
}

// Delete handles DELETE /api/v1/conversations/:convID
func (h *ConversationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	convID, err := parseUUID(chi.URLParam(r, "convID"))
	if err != nil {
		httputil.BadRequest(w, "invalid conversation id")
		return
	}

	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	if err := h.svc.Delete(r.Context(), convID, userID); err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]string{"message": "conversation deleted"})
}

// requireUserIDFromQuery extracts ?user_id= from the query string.
// Note: replaced by JWT middleware in Phase 3.
func requireUserIDFromQuery(r *http.Request) (uuid.UUID, error) {
	raw := r.URL.Query().Get("user_id")
	if raw == "" {
		return uuid.Nil, fmt.Errorf("user_id query param is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid user_id format")
	}
	return id, nil
}
