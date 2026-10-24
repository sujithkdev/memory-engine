package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
	"github.com/memory-engine/memory-engine/internal/service"
)

// RetrievalHandler handles hybrid retrieval HTTP requests.
type RetrievalHandler struct {
	svc *service.RetrievalService
}

// NewRetrievalHandler creates a new RetrievalHandler.
func NewRetrievalHandler(svc *service.RetrievalService) *RetrievalHandler {
	return &RetrievalHandler{svc: svc}
}

// Retrieve handles POST /api/v1/retrieve
//
// This is the primary endpoint for LLM context retrieval.
// Accepts JSON body OR query params.
//
// Example body:
//
//	{
//	  "user_id":   "<uuid>",
//	  "query":     "what is my trading strategy for XAUUSD",
//	  "namespace": "TRADING",
//	  "top_k":     5,
//	  "max_tokens": 2000,
//	  "include_score_breakdown": true
//	}
func (h *RetrievalHandler) Retrieve(w http.ResponseWriter, r *http.Request) {
	var req domain.RetrievalRequest

	// Accept both POST body and GET params.
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.BadRequest(w, "invalid JSON body")
			return
		}
	} else {
		// GET — build from query params.
		userIDStr := r.URL.Query().Get("user_id")
		if userIDStr == "" {
			httputil.BadRequest(w, "user_id is required")
			return
		}
		uid, err := uuid.Parse(userIDStr)
		if err != nil {
			httputil.BadRequest(w, "invalid user_id")
			return
		}
		req.UserID = uid
		req.Query = r.URL.Query().Get("q")
		req.Namespace = r.URL.Query().Get("namespace")
		req.TopK = parseIntQueryDefault(r, "top_k", 5)
		req.MaxTokens = parseIntQueryDefault(r, "max_tokens", 2000)
		req.CandidatePool = parseIntQueryDefault(r, "candidate_pool", 20)
		req.IncludeScoreBreakdown = r.URL.Query().Get("breakdown") == "true"

		if minScore := r.URL.Query().Get("min_score"); minScore != "" {
			if v, err := strconv.ParseFloat(minScore, 64); err == nil {
				req.MinScore = v
			}
		}
		if types := r.URL.Query().Get("types"); types != "" {
			for _, t := range strings.Split(types, ",") {
				req.MemoryTypes = append(req.MemoryTypes, domain.MemoryType(strings.TrimSpace(t)))
			}
		}
	}

	if req.Query == "" {
		httputil.BadRequest(w, "query is required")
		return
	}
	if req.UserID == uuid.Nil {
		httputil.BadRequest(w, "user_id is required")
		return
	}

	resp, err := h.svc.Retrieve(r.Context(), req)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, resp)
}

// RetrieveForUser handles GET /api/v1/users/:userID/retrieve
// Convenience endpoint — user_id from path, query from params.
func (h *RetrievalHandler) RetrieveForUser(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		httputil.BadRequest(w, "query parameter 'q' is required")
		return
	}

	req := domain.RetrievalRequest{
		UserID:                userID,
		Query:                 query,
		Namespace:             r.URL.Query().Get("namespace"),
		TopK:                  parseIntQueryDefault(r, "top_k", 5),
		MaxTokens:             parseIntQueryDefault(r, "max_tokens", 2000),
		CandidatePool:         parseIntQueryDefault(r, "candidate_pool", 20),
		IncludeScoreBreakdown: r.URL.Query().Get("breakdown") == "true",
	}

	if minScore := r.URL.Query().Get("min_score"); minScore != "" {
		if v, err := strconv.ParseFloat(minScore, 64); err == nil {
			req.MinScore = v
		}
	}
	if types := r.URL.Query().Get("types"); types != "" {
		for _, t := range strings.Split(types, ",") {
			req.MemoryTypes = append(req.MemoryTypes, domain.MemoryType(strings.TrimSpace(t)))
		}
	}

	resp, err := h.svc.Retrieve(r.Context(), req)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, resp)
}
