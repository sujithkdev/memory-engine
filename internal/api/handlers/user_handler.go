// Package handlers contains HTTP handler implementations.
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
	"github.com/memory-engine/memory-engine/internal/service"
)

// UserHandler handles user HTTP requests.
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// Create handles POST /api/v1/users
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input domain.CreateUserInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}

	user, err := h.svc.Create(r.Context(), input)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Created(w, user)
}

// Get handles GET /api/v1/users/:userID
func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	user, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, user)
}

// List handles GET /api/v1/users
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := domain.UserFilter{
		Page:     parseIntQuery(r, "page", 1),
		PageSize: parseIntQuery(r, "page_size", 20),
	}

	if activeStr := r.URL.Query().Get("active"); activeStr != "" {
		active := activeStr == "true" || activeStr == "1"
		filter.IsActive = &active
	}

	list, err := h.svc.List(r.Context(), filter)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, list)
}

// Update handles PATCH /api/v1/users/:userID
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	var input domain.UpdateUserInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}

	user, err := h.svc.Update(r.Context(), id, input)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, user)
}

// Delete handles DELETE /api/v1/users/:userID
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	if err := h.svc.Delete(r.Context(), id); err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]string{"message": "user deactivated"})
}

// --- shared helpers used across this package ---

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func parseIntQuery(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return defaultVal
	}
	return n
}
