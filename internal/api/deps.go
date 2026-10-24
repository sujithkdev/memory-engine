package api

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/cache"
	"github.com/memory-engine/memory-engine/internal/config"
	"github.com/memory-engine/memory-engine/internal/embeddings"
	"github.com/memory-engine/memory-engine/internal/repository"
	"github.com/memory-engine/memory-engine/internal/service"
)

// Deps holds all initialized application dependencies.
type Deps struct {
	UserSvc         *service.UserService
	ConvSvc         *service.ConversationService
	MsgSvc          *service.MessageService
	MemorySvc       *service.MemoryService
	ConflictSvc     *service.ConflictService
	RelationshipSvc *service.RelationshipService
	SearchSvc       *service.SearchService
	EmbeddingSvc    *service.EmbeddingService
	VectorSearchSvc *service.VectorSearchService
	RetrievalSvc    *service.RetrievalService
	Cache           cache.Cache
}

// NewDeps wires up dependencies with safe defaults (null embedder, disabled cache).
// Used by tests.
func NewDeps(pool *pgxpool.Pool) *Deps {
	return NewDepsWithConfig(
		pool,
		config.ScoringConfig{
			Weights: config.ScoringWeights{
				Relevance: 0.40, Importance: 0.20, Confidence: 0.15,
				Salience: 0.10, Recency: 0.10, AccessFrequency: 0.05,
			},
			Decay: config.DecayConfig{Lambda: 0.023, Floor: 0.05},
			InitialSalience: map[string]float64{
				"WORKING": 0.9, "INSTRUCTION": 0.85, "GOAL": 0.75,
				"PREFERENCE": 0.7, "PROJECT": 0.65, "DECISION": 0.65,
				"FACT": 0.6, "SKILL": 0.6, "RELATIONSHIP": 0.6,
				"EXPERIENCE": 0.55, "KNOWLEDGE": 0.5,
			},
		},
		config.EmbeddingsConfig{
			Provider: "null", Dimensions: 768,
			Model: "null-embedder", Version: "0.0.0",
		},
		config.CacheConfig{Enabled: false},
		zerolog.Nop(),
	)
}

// NewDepsWithConfig wires up all repositories, services, and cache.
func NewDepsWithConfig(
	pool *pgxpool.Pool,
	scoringCfg config.ScoringConfig,
	embCfg config.EmbeddingsConfig,
	cacheCfg config.CacheConfig,
	logger zerolog.Logger,
) *Deps {
	// Repositories.
	userRepo     := repository.NewUserRepository(pool)
	convRepo     := repository.NewConversationRepository(pool)
	msgRepo      := repository.NewMessageRepository(pool)
	memoryRepo   := repository.NewMemoryRepository(pool)
	conflictRepo := repository.NewConflictRepository(pool)
	relationRepo := repository.NewRelationshipRepository(pool)
	searchRepo   := repository.NewSearchRepository(pool)
	embedRepo    := repository.NewEmbeddingRepository(pool)

	// Cache.
	var c cache.Cache
	if cacheCfg.Enabled {
		c = cache.NewMemoryCache(cacheCfg.MaxItems, cacheCfg.CleanupInterval)
		logger.Info().Int("max_items", cacheCfg.MaxItems).Msg("in-memory cache enabled")
	} else {
		c = &cache.NullCache{}
	}

	// Embedding provider.
	var embedder embeddings.Embedder
	switch embCfg.Provider {
	case "ollama":
		embedder = embeddings.NewOllamaEmbedder(embeddings.OllamaConfig{
			BaseURL:    embCfg.OllamaURL,
			Model:      embCfg.Model,
			Version:    embCfg.Version,
			Dimensions: embCfg.Dimensions,
		})
	default:
		embedder = embeddings.NewNullEmbedder(embCfg.Dimensions)
	}

	// Services.
	embedSvc     := service.NewEmbeddingService(embedder, embedRepo, memoryRepo, logger)
	vectorSvc    := service.NewVectorSearchService(embedSvc, embedRepo, memoryRepo)
	retrievalSvc := service.NewRetrievalService(searchRepo, embedRepo, memoryRepo, embedSvc, scoringCfg, logger)

	return &Deps{
		UserSvc:         service.NewUserService(userRepo),
		ConvSvc:         service.NewConversationService(convRepo, userRepo),
		MsgSvc:          service.NewMessageService(msgRepo, convRepo),
		MemorySvc:       service.NewMemoryService(memoryRepo, scoringCfg),
		ConflictSvc:     service.NewConflictService(memoryRepo, conflictRepo, logger),
		RelationshipSvc: service.NewRelationshipService(memoryRepo, relationRepo),
		SearchSvc:       service.NewSearchService(searchRepo),
		EmbeddingSvc:    embedSvc,
		VectorSearchSvc: vectorSvc,
		RetrievalSvc:    retrievalSvc,
		Cache:           c,
	}
}
