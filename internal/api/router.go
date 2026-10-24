package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/api/handlers"
	"github.com/memory-engine/memory-engine/internal/database"
	"github.com/memory-engine/memory-engine/internal/middleware"
)

// RouterConfig carries dependencies needed to wire up the router.
type RouterConfig struct {
	DB      *database.DB
	Logger  zerolog.Logger
	Version string
	Name    string
	Deps    *Deps
}

// NewRouter builds and returns the fully configured HTTP router.
//
// Phase 5 route additions:
//
//	GET  /api/v1/users/{userID}/conflicts
//	POST /api/v1/conflicts/{conflictID}/resolve
//	POST /api/v1/memories/{memoryID}/relationships
//	GET  /api/v1/memories/{memoryID}/relationships
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	// ── Global middleware ────────────────────────────────────────────────────
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.RequestLogger(cfg.Logger))
	r.Use(middleware.Recoverer(cfg.Logger))
	r.Use(chimiddleware.SetHeader("X-Powered-By", cfg.Name))
	r.Use(chimiddleware.CleanPath)
	r.Use(chimiddleware.StripSlashes)

	// ── Health probes ────────────────────────────────────────────────────────
	health := NewHealthHandler(cfg.DB, cfg.Name, cfg.Version)
	r.Get("/health", health.Live)
	r.Get("/ready", health.Ready)
	r.Get("/workers", health.WorkerStatus)

	// ── API v1 ───────────────────────────────────────────────────────────────
	r.Route("/api/v1", func(r chi.Router) {

		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			Success(w, map[string]string{
				"name":    cfg.Name,
				"version": cfg.Version,
				"status":  "running",
			})
		})

		if cfg.Deps == nil {
			return
		}

		// Instantiate handlers once.
		userH      := handlers.NewUserHandler(cfg.Deps.UserSvc)
		convH      := handlers.NewConversationHandler(cfg.Deps.ConvSvc)
		msgH       := handlers.NewMessageHandler(cfg.Deps.MsgSvc)
		memH       := handlers.NewMemoryHandler(cfg.Deps.MemorySvc)
		conflictH  := handlers.NewConflictHandler(cfg.Deps.ConflictSvc, cfg.Deps.RelationshipSvc)
		searchH    := handlers.NewSearchHandler(cfg.Deps.SearchSvc)
		vectorH    := handlers.NewVectorHandler(cfg.Deps.VectorSearchSvc, cfg.Deps.EmbeddingSvc)
		retrievalH := handlers.NewRetrievalHandler(cfg.Deps.RetrievalSvc)

		// ── Users ──────────────────────────────────────────────────────────
		r.Route("/users", func(r chi.Router) {
			r.Post("/", userH.Create)
			r.Get("/", userH.List)

			r.Route("/{userID}", func(r chi.Router) {
				r.Get("/", userH.Get)
				r.Patch("/", userH.Update)
				r.Delete("/", userH.Delete)

				r.Post("/conversations", convH.Create)
				r.Get("/conversations", convH.List)

				r.Get("/memories", memH.List)
				r.Get("/memories/search", searchH.Search)          // Phase 6: FTS
				r.Get("/memories/vector-search", vectorH.VectorSearch) // Phase 7: vector
				r.Get("/memories/scored", memH.ScoreList)
				r.Get("/memories/suggest-namespaces", searchH.SuggestNamespaces)

				// Phase 5: conflicts per user.
				r.Get("/conflicts", conflictH.ListConflicts)
			})
		})

		// ── Conversations ──────────────────────────────────────────────────
		r.Route("/conversations/{convID}", func(r chi.Router) {
			r.Get("/", convH.Get)
			r.Patch("/", convH.Update)
			r.Delete("/", convH.Delete)
			r.Post("/messages", msgH.Create)
			r.Get("/messages", msgH.List)
		})

		// ── Messages ───────────────────────────────────────────────────────
		r.Get("/messages/{msgID}", msgH.Get)

		// ── Memories ───────────────────────────────────────────────────────
		r.Route("/memories", func(r chi.Router) {
			// Named static routes — must precede /{memoryID}.
			r.Get("/types", memH.Types)
			r.Get("/scoring-weights", memH.ScoringWeights)
			r.Post("/remember", memH.Remember)
			r.Post("/forget-namespace", memH.ForgetNamespace)
			r.Post("/", memH.Create)

			r.Route("/{memoryID}", func(r chi.Router) {
				// CRUD.
				r.Get("/", memH.Get)
				r.Patch("/", memH.Update)
				// Lifecycle.
				r.Post("/forget", memH.Forget)
				r.Post("/archive", memH.Archive)
				r.Post("/restore", memH.Restore)
				r.Post("/activate", memH.Activate)
				// Phase 4: scoring + audit.
				r.Get("/score", memH.ScoreOne)
				r.Get("/audit", memH.GetAuditLog)
				// Phase 5: relationships.
				r.Post("/relationships", conflictH.LinkMemories)
				r.Get("/relationships", conflictH.GetRelationships)
				// Phase 7: on-demand embedding.
				r.Post("/embed", vectorH.EmbedMemory)
			})
		})

		// ── Embeddings info ────────────────────────────────────────────────
		r.Get("/embeddings/info", vectorH.EmbeddingInfo)

		// ── Conflicts (standalone resolve) ─────────────────────────────────
		r.Route("/conflicts", func(r chi.Router) {
			r.Post("/{conflictID}/resolve", conflictH.ResolveConflict)
		})

		// ── Phase 8: Hybrid Retrieval ──────────────────────────────────────
		// Primary LLM context endpoint — POST with JSON body.
		r.Post("/retrieve", retrievalH.Retrieve)
		// GET convenience endpoint per user.
		r.Get("/users/{userID}/retrieve", retrievalH.RetrieveForUser)
	})

	// ── Fallbacks ────────────────────────────────────────────────────────────
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		NotFound(w, "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		ErrorResponse(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
	})

	return r
}
