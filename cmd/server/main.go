// Command server is the entry point for the memory-engine API server.
// It loads configuration, connects to the database, runs migrations,
// starts the HTTP server, and handles graceful shutdown.
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/memory-engine/memory-engine/internal/api"
	"github.com/memory-engine/memory-engine/internal/config"
	"github.com/memory-engine/memory-engine/internal/database"
	"github.com/memory-engine/memory-engine/internal/repository"
	"github.com/memory-engine/memory-engine/internal/scorer"
	"github.com/memory-engine/memory-engine/internal/worker"
)

func main() {
	// --- Flags ---
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	// --- Bootstrap logger (pretty for dev, JSON for prod) ---
	// We use a temporary info-level logger until config is loaded.
	tmpLogger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().Timestamp().Logger()

	// --- Load configuration ---
	cfg, err := config.Load(*configPath)
	if err != nil {
		tmpLogger.Fatal().Err(err).Str("path", *configPath).Msg("failed to load configuration")
	}

	// --- Configure structured logger ---
	logger := buildLogger(cfg.Log)
	logger.Info().
		Str("app", cfg.App.Name).
		Str("version", cfg.App.Version).
		Str("env", cfg.App.Environment).
		Msg("starting memory engine")

	// --- Connect to database ---
	ctx := context.Background()
	db, err := database.New(ctx, cfg.Database, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Close()

	// --- Run migrations ---
	logger.Info().Msg("running database migrations")
	if err := database.MigrateUp(cfg.Database, cfg.Migrations, logger); err != nil {
		logger.Fatal().Err(err).Msg("database migration failed")
	}

	// --- Wire application dependencies ---
	deps := api.NewDepsWithConfig(db.Pool, cfg.Scoring, cfg.Embeddings, cfg.Cache, logger)

	// --- Start background workers ---
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	if cfg.Workers.Enabled {
		memRepo   := repository.NewMemoryRepository(db.Pool)
		embedRepo := repository.NewEmbeddingRepository(db.Pool)
		_ = embedRepo // used by embed worker via service

		s := scorer.New(cfg.Scoring)
		bs := cfg.Workers.BatchSize

		pool := worker.NewPool(logger,
			worker.NewDecayWorker(memRepo, s, cfg.Workers.DecayInterval, bs, logger),
			worker.NewReembedWorker(deps.EmbeddingSvc, cfg.Workers.ReembedInterval, bs/4, logger),
			worker.NewExpiryWorker(memRepo, cfg.Workers.ExpiryInterval, bs, logger),
			worker.NewConsolidationWorker(memRepo, cfg.Workers.ConsolidateInterval, bs/2, logger),
		)
		pool.Start(workerCtx)
		defer pool.Wait()
		logger.Info().Msg("background workers started")
	}

	// --- Build HTTP router ---
	router := api.NewRouter(api.RouterConfig{
		DB:      db,
		Logger:  logger,
		Version: cfg.App.Version,
		Name:    cfg.App.Name,
		Deps:    deps,
	})

	// --- Configure HTTP server ---
	srv := &http.Server{
		Addr:         cfg.Server.Addr(),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// --- Start server in background goroutine ---
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info().
			Str("addr", srv.Addr).
			Msg("HTTP server listening")
		serverErrors <- srv.ListenAndServe()
	}()

	// --- Wait for shutdown signal ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		logger.Fatal().Err(err).Msg("server error")

	case sig := <-quit:
		logger.Info().
			Str("signal", sig.String()).
			Msg("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(ctx, cfg.Server.ShutdownTimeout)
		defer cancel()

		logger.Info().
			Dur("timeout", cfg.Server.ShutdownTimeout).
			Msg("gracefully shutting down HTTP server")

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error().Err(err).Msg("forced server shutdown")
			if closeErr := srv.Close(); closeErr != nil {
				logger.Error().Err(closeErr).Msg("server close error")
			}
		}

		logger.Info().Msg("server stopped cleanly")
	}
}

// buildLogger constructs the application logger based on config.
func buildLogger(cfg config.LogConfig) zerolog.Logger {
	// Parse level.
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if cfg.Format == "pretty" {
		return zerolog.New(
			zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339},
		).With().Timestamp().Caller().Logger()
	}

	// JSON format for staging/production.
	return zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()
}

// Keep log package happy if used anywhere.
var _ = log.Logger
