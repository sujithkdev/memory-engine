package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
	"github.com/memory-engine/memory-engine/internal/service"
)

// ConflictHandler handles conflict and relationship HTTP requests.
type ConflictHandler struct {
	conflictSvc     *service.ConflictService
	relationshipSvc *service.RelationshipService
}

// NewConflictHandler creates a new ConflictHandler.
func NewConflictHandler(c *service.ConflictService, r *service.RelationshipService) *ConflictHandler {
	return &ConflictHandler{conflictSvc: c, relationshipSvc: r}
}

// ── Conflicts ─────────────────────────────────────────────────────────────────

// ListConflicts handles GET /api/v1/users/:userID/conflicts
// Optional ?status=DETECTED|RESOLVED|IGNORED filter.
func (h *ConflictHandler) ListConflicts(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	var statusFilter *domain.ConflictStatus
	if s := r.URL.Query().Get("status"); s != "" {
		cs := domain.ConflictStatus(s)
		statusFilter = &cs
	}

	entries, err := h.conflictSvc.ListConflicts(r.Context(), userID, statusFilter)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	unresolved, _ := h.conflictSvc.GetUnresolvedCount(r.Context(), userID)

	httputil.Success(w, map[string]interface{}{
		"conflicts":        entries,
		"total":            len(entries),
		"unresolved_count": unresolved,
	})
}

// ResolveConflict handles POST /api/v1/conflicts/:conflictID/resolve
//
// Body:
//
//	{
//	  "winner_memory_id": "<uuid>",   // optional
//	  "strategy":         "KEEP_NEWER",
//	  "reason":           "manually resolved"
//	}
func (h *ConflictHandler) ResolveConflict(w http.ResponseWriter, r *http.Request) {
	conflictID, err := parseUUID(chi.URLParam(r, "conflictID"))
	if err != nil {
		httputil.BadRequest(w, "invalid conflict id")
		return
	}

	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	var body struct {
		WinnerMemoryID string `json:"winner_memory_id"`
		Strategy       string `json:"strategy"`
		Reason         string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}
	if body.Strategy == "" {
		httputil.BadRequest(w, "strategy is required (KEEP_NEWER, KEEP_HIGHER_CONF, MERGE, KEEP_BOTH, MANUAL)")
		return
	}

	var winnerID *uuid.UUID
	if body.WinnerMemoryID != "" {
		parsed, err := uuid.Parse(body.WinnerMemoryID)
		if err != nil {
			httputil.BadRequest(w, "invalid winner_memory_id")
			return
		}
		winnerID = &parsed
	}

	reason := body.Reason
	if reason == "" {
		reason = "manually resolved by user"
	}

	if err := h.conflictSvc.ResolveConflict(
		r.Context(), conflictID, winnerID,
		domain.ResolutionStrategy(body.Strategy),
		reason, userID.String(),
	); err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]string{"message": "conflict resolved"})
}

// ── Relationships ─────────────────────────────────────────────────────────────

// LinkMemories handles POST /api/v1/memories/:memoryID/relationships
//
// Body:
//
//	{
//	  "target_memory_id":  "<uuid>",
//	  "relationship_type": "CAUSED_BY",
//	  "confidence":        0.9
//	}
func (h *ConflictHandler) LinkMemories(w http.ResponseWriter, r *http.Request) {
	sourceID, err := parseUUID(chi.URLParam(r, "memoryID"))
	if err != nil {
		httputil.BadRequest(w, "invalid memory id")
		return
	}

	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	var body struct {
		TargetMemoryID string  `json:"target_memory_id"`
		RelType        string  `json:"relationship_type"`
		Confidence     float64 `json:"confidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}
	if body.TargetMemoryID == "" {
		httputil.BadRequest(w, "target_memory_id is required")
		return
	}
	if body.RelType == "" {
		httputil.BadRequest(w, "relationship_type is required (CAUSED_BY, LED_TO, SUPPORTS, CONTRADICTS, RELATED_TO, SUPERSEDES, DERIVED_FROM)")
		return
	}

	targetID, err := uuid.Parse(body.TargetMemoryID)
	if err != nil {
		httputil.BadRequest(w, "invalid target_memory_id")
		return
	}

	if body.Confidence <= 0 {
		body.Confidence = 1.0
	}

	rel, err := h.relationshipSvc.Link(
		r.Context(), userID, sourceID, targetID,
		domain.RelationshipType(body.RelType), body.Confidence,
	)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Created(w, rel)
}

// GetRelationships handles GET /api/v1/memories/:memoryID/relationships
func (h *ConflictHandler) GetRelationships(w http.ResponseWriter, r *http.Request) {
	memoryID, err := parseUUID(chi.URLParam(r, "memoryID"))
	if err != nil {
		httputil.BadRequest(w, "invalid memory id")
		return
	}

	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	rels, err := h.relationshipSvc.GetRelationships(r.Context(), memoryID, userID)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]interface{}{
		"memory_id":     memoryID,
		"relationships": rels,
		"total":         len(rels),
	})
}
