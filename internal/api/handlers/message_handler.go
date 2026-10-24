package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
	"github.com/memory-engine/memory-engine/internal/service"
)

// MessageHandler handles message HTTP requests.
type MessageHandler struct {
	svc *service.MessageService
}

// NewMessageHandler creates a new MessageHandler.
func NewMessageHandler(svc *service.MessageService) *MessageHandler {
	return &MessageHandler{svc: svc}
}

// Create handles POST /api/v1/conversations/:convID/messages
func (h *MessageHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var input domain.CreateMessageInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}
	input.ConversationID = convID

	msg, err := h.svc.Create(r.Context(), userID, input)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Created(w, msg)
}

// Get handles GET /api/v1/messages/:msgID
func (h *MessageHandler) Get(w http.ResponseWriter, r *http.Request) {
	msgID, err := parseUUID(chi.URLParam(r, "msgID"))
	if err != nil {
		httputil.BadRequest(w, "invalid message id")
		return
	}

	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	msg, err := h.svc.GetByID(r.Context(), msgID, userID)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, msg)
}

// List handles GET /api/v1/conversations/:convID/messages
func (h *MessageHandler) List(w http.ResponseWriter, r *http.Request) {
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

	filter := domain.MessageFilter{
		ConversationID: convID,
		Page:           parseIntQuery(r, "page", 1),
		PageSize:       parseIntQuery(r, "page_size", 50),
	}

	if roleStr := r.URL.Query().Get("role"); roleStr != "" {
		role := domain.Role(roleStr)
		filter.Role = &role
	}

	list, err := h.svc.List(r.Context(), userID, filter)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, list)
}
