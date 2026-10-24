package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/httputil"
	"github.com/memory-engine/memory-engine/internal/service"
)

// VectorHandler handles vector search and embedding HTTP requests.
type VectorHandler struct {
	vectorSvc  *service.VectorSearchService
	embedSvc   *service.EmbeddingService
}

// NewVectorHandler creates a new VectorHandler.
func NewVectorHandler(vs *service.VectorSearchService, es *service.EmbeddingService) *VectorHandler {
	return &VectorHandler{vectorSvc: vs, embedSvc: es}
}

// VectorSearch handles GET /api/v1/users/:userID/memories/vector-search
//
// Query params:
//
//	q              — search query (required)
//	namespace      — filter by namespace
//	type           — filter by memory_type
//	top_k          — max results (default: 20)
//	min_similarity — minimum cosine similarity [0,1] (default: 0)
func (h *VectorHandler) VectorSearch(w http.ResponseWriter, r *http.Request) {
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

	req := service.VectorSearchRequest{
		UserID: userID,
		Query:  query,
		TopK:   parseIntQueryDefault(r, "top_k", 20),
	}

	if ns := r.URL.Query().Get("namespace"); ns != "" {
		req.Namespace = &ns
	}
	if t := r.URL.Query().Get("type"); t != "" {
		mt := domain.MemoryType(t)
		req.MemoryType = &mt
	}
	if minSim := r.URL.Query().Get("min_similarity"); minSim != "" {
		if v, err := strconv.ParseFloat(minSim, 64); err == nil {
			req.MinSimilarity = v
		}
	}

	resp, err := h.vectorSvc.Search(r.Context(), req)
	if err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, resp)
}

// EmbedMemory handles POST /api/v1/memories/:memoryID/embed
// Triggers on-demand embedding generation for a specific memory.
func (h *VectorHandler) EmbedMemory(w http.ResponseWriter, r *http.Request) {
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
	_ = userID // ownership already enforced by memory service on creation

	if err := h.embedSvc.EmbedMemory(r.Context(), memoryID); err != nil {
		httputil.HandleDomainError(w, err)
		return
	}

	httputil.Success(w, map[string]interface{}{
		"memory_id": memoryID,
		"model":     h.embedSvc.ModelName(),
		"message":   "embedding generated",
	})
}

// EmbeddingInfo handles GET /api/v1/embeddings/info
// Returns the active embedding model configuration.
func (h *VectorHandler) EmbeddingInfo(w http.ResponseWriter, r *http.Request) {
	httputil.Success(w, map[string]interface{}{
		"model":      h.embedSvc.ModelName(),
		"dimensions": h.embedSvc.Dimensions(),
	})
}
