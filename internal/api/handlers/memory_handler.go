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

// MemoryHandler handles all memory HTTP requests.
type MemoryHandler struct {
	svc *service.MemoryService
}

// NewMemoryHandler creates a new MemoryHandler.
func NewMemoryHandler(svc *service.MemoryService) *MemoryHandler {
	return &MemoryHandler{svc: svc}
}

// ── Create ─────────────────────────────────────────────────────────────────────

// Create handles POST /api/v1/memories
// Creates a memory — initial status depends on source_type and explicit flag.
func (h *MemoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	var input domain.CreateMemoryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}
	input.UserID = userID

	mem, err := h.svc.Create(r.Context(), input)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Created(w, mem)
}

// ── Remember ───────────────────────────────────────────────────────────────────

// Remember handles POST /api/v1/memories/remember
// Explicit "remember this" — creates an ACTIVE memory immediately.
//
// Example request:
//
//	POST /api/v1/memories/remember?user_id=<uuid>
//	{
//	  "content": "I prefer Python for data scripts",
//	  "memory_type": "PREFERENCE",
//	  "importance": 0.9,
//	  "namespace": "CODING"
//	}
func (h *MemoryHandler) Remember(w http.ResponseWriter, r *http.Request) {
	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	var input domain.RememberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}
	input.UserID = userID

	// Default memory_type to FACT if not provided.
	if input.MemoryType == "" {
		input.MemoryType = domain.MemoryTypeFact
	}
	// Default importance.
	if input.Importance == 0 {
		input.Importance = 0.7
	}

	mem, err := h.svc.Remember(r.Context(), input)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Created(w, mem)
}

// ── Forget ─────────────────────────────────────────────────────────────────────

// Forget handles POST /api/v1/memories/:memoryID/forget
// Explicit "forget this" — transitions memory to DELETED.
//
// Example request:
//
//	POST /api/v1/memories/<id>/forget?user_id=<uuid>
//	{ "reason": "user requested deletion" }
func (h *MemoryHandler) Forget(w http.ResponseWriter, r *http.Request) {
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

	var input domain.ForgetInput
	// Body is optional — reason is optional too.
	_ = json.NewDecoder(r.Body).Decode(&input)
	input.UserID = userID

	if err := h.svc.Forget(r.Context(), memoryID, input); err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]string{"message": "memory forgotten"})
}

// ForgetNamespace handles POST /api/v1/memories/forget-namespace
// "Forget everything about X" — bulk delete by namespace.
//
// Example request:
//
//	POST /api/v1/memories/forget-namespace?user_id=<uuid>
//	{ "namespace": "TRADING", "reason": "starting fresh" }
func (h *MemoryHandler) ForgetNamespace(w http.ResponseWriter, r *http.Request) {
	userID, err := requireUserIDFromQuery(r)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	var body struct {
		Namespace string `json:"namespace"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}

	count, err := h.svc.ForgetByNamespace(r.Context(), userID, body.Namespace, body.Reason)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]interface{}{
		"message":        "memories forgotten",
		"namespace":      body.Namespace,
		"memories_deleted": count,
	})
}

// ── Get ────────────────────────────────────────────────────────────────────────

// Get handles GET /api/v1/memories/:memoryID
func (h *MemoryHandler) Get(w http.ResponseWriter, r *http.Request) {
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

	mem, err := h.svc.GetByID(r.Context(), memoryID, userID)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, mem)
}

// ── List ───────────────────────────────────────────────────────────────────────

// List handles GET /api/v1/users/:userID/memories
// Supports filtering by status, type, namespace, memory_level.
func (h *MemoryHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	filter := domain.MemoryFilter{
		UserID:   userID,
		Page:     parseIntQuery(r, "page", 1),
		PageSize: parseIntQuery(r, "page_size", 20),
	}

	if s := r.URL.Query().Get("status"); s != "" {
		status := domain.MemoryStatus(s)
		filter.Status = &status
	}
	if t := r.URL.Query().Get("type"); t != "" {
		mt := domain.MemoryType(t)
		filter.MemoryType = &mt
	}
	if ns := r.URL.Query().Get("namespace"); ns != "" {
		filter.Namespace = &ns
	}
	if lvl := r.URL.Query().Get("level"); lvl != "" {
		ml := domain.MemoryLevel(lvl)
		filter.MemoryLevel = &ml
	}

	list, err := h.svc.List(r.Context(), filter)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, list)
}

// ── Update ─────────────────────────────────────────────────────────────────────

// Update handles PATCH /api/v1/memories/:memoryID
// Content changes create a new version. Metadata changes are in-place.
func (h *MemoryHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	var input domain.UpdateMemoryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}

	mem, err := h.svc.Update(r.Context(), memoryID, userID, input)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, mem)
}

// ── Lifecycle transitions ─────────────────────────────────────────────────────

// Archive handles POST /api/v1/memories/:memoryID/archive
func (h *MemoryHandler) Archive(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	mem, err := h.svc.Archive(r.Context(), memoryID, userID, body.Reason)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, mem)
}

// Restore handles POST /api/v1/memories/:memoryID/restore
func (h *MemoryHandler) Restore(w http.ResponseWriter, r *http.Request) {
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

	mem, err := h.svc.Restore(r.Context(), memoryID, userID)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, mem)
}

// Activate handles POST /api/v1/memories/:memoryID/activate
func (h *MemoryHandler) Activate(w http.ResponseWriter, r *http.Request) {
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

	mem, err := h.svc.Activate(r.Context(), memoryID, userID)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, mem)
}

// ── Search ─────────────────────────────────────────────────────────────────────

// Search handles GET /api/v1/users/:userID/memories/search
// Phase 3: basic filter-based search. Upgraded to FTS in Phase 6, hybrid in Phase 8.
func (h *MemoryHandler) Search(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	query := r.URL.Query().Get("q")

	filter := domain.MemoryFilter{
		UserID:   userID,
		Page:     parseIntQuery(r, "page", 1),
		PageSize: parseIntQuery(r, "page_size", 20),
	}
	if ns := r.URL.Query().Get("namespace"); ns != "" {
		filter.Namespace = &ns
	}
	if t := r.URL.Query().Get("type"); t != "" {
		mt := domain.MemoryType(t)
		filter.MemoryType = &mt
	}

	list, err := h.svc.Search(r.Context(), userID, query, filter)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, list)
}

// ── Audit Log ─────────────────────────────────────────────────────────────────

// GetAuditLog handles GET /api/v1/memories/:memoryID/audit
func (h *MemoryHandler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
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

	entries, err := h.svc.GetAuditLog(r.Context(), memoryID, userID)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]interface{}{
		"memory_id": memoryID,
		"entries":   entries,
		"total":     len(entries),
	})
}

// ── Type hint endpoint ────────────────────────────────────────────────────────

// Types handles GET /api/v1/memories/types
// Returns all valid memory types and statuses — useful for clients/LLMs.
func (h *MemoryHandler) Types(w http.ResponseWriter, r *http.Request) {
	types := make([]string, 0, len(domain.ValidMemoryTypes))
	for t := range domain.ValidMemoryTypes {
		types = append(types, string(t))
	}

	statuses := []string{
		string(domain.MemoryStatusNew),
		string(domain.MemoryStatusCandidate),
		string(domain.MemoryStatusValidated),
		string(domain.MemoryStatusActive),
		string(domain.MemoryStatusUpdated),
		string(domain.MemoryStatusStale),
		string(domain.MemoryStatusArchived),
		string(domain.MemoryStatusDeleted),
	}

	httputil.Success(w, map[string]interface{}{
		"memory_types": types,
		"statuses":     statuses,
		"namespaces":   keysOf(domain.KnownNamespaces),
		"lifecycle": "NEW → CANDIDATE → VALIDATED → ACTIVE → UPDATED → STALE → ARCHIVED → DELETED",
	})
}

func keysOf(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ensure uuid import used
var _ = uuid.Nil

// ── Phase 4: Scoring endpoints ────────────────────────────────────────────────

// ScoreList handles GET /api/v1/users/:userID/memories/scored
// Returns memories ranked by the full scoring engine with breakdown visible.
func (h *MemoryHandler) ScoreList(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUID(chi.URLParam(r, "userID"))
	if err != nil {
		httputil.BadRequest(w, "invalid user id")
		return
	}

	filter := domain.MemoryFilter{
		UserID:   userID,
		Page:     parseIntQuery(r, "page", 1),
		PageSize: parseIntQuery(r, "page_size", 20),
	}
	if ns := r.URL.Query().Get("namespace"); ns != "" {
		filter.Namespace = &ns
	}
	if t := r.URL.Query().Get("type"); t != "" {
		mt := domain.MemoryType(t)
		filter.MemoryType = &mt
	}

	result, err := h.svc.ScoreAndList(r.Context(), filter)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, result)
}

// ScoreOne handles GET /api/v1/memories/:memoryID/score
// Returns the score breakdown for a single memory.
func (h *MemoryHandler) ScoreOne(w http.ResponseWriter, r *http.Request) {
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

	mem, err := h.svc.GetByID(r.Context(), memoryID, userID)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, h.svc.ClassifyMemory(mem))
}

// ScoringWeights handles GET /api/v1/memories/scoring-weights
// Returns the currently active scoring weight configuration.
func (h *MemoryHandler) ScoringWeights(w http.ResponseWriter, r *http.Request) {
	httputil.Success(w, h.svc.GetScoringWeights())
}
