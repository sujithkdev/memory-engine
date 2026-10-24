// Package database manages the PostgreSQL connection pool and migrations.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/config"
)

// DB wraps pgxpool.Pool with helpers.
type DB struct {
	Pool   *pgxpool.Pool
	logger zerolog.Logger
}

// New creates a new connection pool using the given config.
// It validates connectivity before returning.
func New(ctx context.Context, cfg config.DatabaseConfig, logger zerolog.Logger) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parsing database DSN: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod

	// Traced connect: log every acquired connection in debug mode.
	poolCfg.ConnConfig.Tracer = &queryTracer{logger: logger}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	db := &DB{Pool: pool, logger: logger}

	// Verify connectivity immediately.
	if err := db.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	logger.Info().
		Str("host", cfg.Host).
		Int("port", cfg.Port).
		Str("database", cfg.Name).
		Int32("max_conns", cfg.MaxConns).
		Msg("database pool established")

	return db, nil
}

// Ping sends a simple query to verify the connection is alive.
func (db *DB) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := conn.Conn().Ping(ctx); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	return nil
}

// Stats returns current pool statistics — useful for metrics/health checks.
func (db *DB) Stats() *pgxpool.Stat {
	return db.Pool.Stat()
}

// Close gracefully closes the connection pool.
func (db *DB) Close() {
	db.logger.Info().Msg("closing database pool")
	db.Pool.Close()
}
