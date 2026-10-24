package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
	"github.com/memory-engine/memory-engine/internal/service"
)

// SearchHandler handles full-text search HTTP requests.
type SearchHandler struct {
	svc *service.SearchService
}

// NewSearchHandler creates a new SearchHandler.
func NewSearchHandler(svc *service.SearchService) *SearchHandler {
	return &SearchHandler{svc: svc}
}

// Search handles GET /api/v1/users/:userID/memories/search
//
// Query params:
//
//	q         — search query (required for ranked results)
//	namespace — filter by namespace
//	type      — filter by memory_type
//	status    — filter by status (default: ACTIVE)
//	mode      — AND|OR|PHRASE|PREFIX|WEB (default: auto-detect)
//	min_rank  — minimum FTS rank (default: 0)
//	page      — page number (default: 1)
//	page_size — results per page (default: 20, max: 100)
//
// Example:
//
//	GET /api/v1/users/abc/memories/search?q=python+scripting&namespace=CODING&type=PREFERENCE
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	if userID == "" {
		httputil.BadRequest(w, "user_id is required")
		return
	}

	req := domain.SearchRequest{
		UserID:    userID,
		Query:     r.URL.Query().Get("q"),
		Namespace: r.URL.Query().Get("namespace"),
		MemoryType: r.URL.Query().Get("type"),
		Status:    r.URL.Query().Get("status"),
		QueryType: r.URL.Query().Get("mode"),
		Page:      parseIntQueryDefault(r, "page", 1),
		PageSize:  parseIntQueryDefault(r, "page_size", 20),
	}

	if minRankStr := r.URL.Query().Get("min_rank"); minRankStr != "" {
		if v, err := strconv.ParseFloat(minRankStr, 64); err == nil {
			req.MinRank = v
		}
	}

	resp, err := h.svc.Search(r.Context(), req)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, resp)
}

// SuggestNamespaces handles GET /api/v1/users/:userID/namespaces/suggest
//
// Query params:
//
//	q — namespace prefix for autocomplete
func (h *SearchHandler) SuggestNamespaces(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	prefix := r.URL.Query().Get("q")

	suggestions, err := h.svc.SuggestNamespaces(r.Context(), userID, prefix)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]interface{}{
		"suggestions": suggestions,
		"prefix":      prefix,
	})
}

func parseIntQueryDefault(r *http.Request, key string, def int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	return n
}
