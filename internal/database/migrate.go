package database

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/rs/zerolog"

	"github.com/memory-engine/memory-engine/internal/config"
)

// MigrateUp runs all pending UP migrations from the migrations directory.
func MigrateUp(cfg config.DatabaseConfig, migrationsCfg config.MigrationsConfig, logger zerolog.Logger) error {
	sourceURL := fmt.Sprintf("file://%s", migrationsCfg.Path)

	// Build postgres URL for migrate (different format from pgx DSN).
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&x-migrations-table=%s",
		cfg.User, cfg.Password,
		cfg.Host, cfg.Port,
		cfg.Name,
		cfg.SSLMode,
		migrationsCfg.Table,
	)

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			logger.Error().Err(srcErr).Msg("migration source close error")
		}
		if dbErr != nil {
			logger.Error().Err(dbErr).Msg("migration db close error")
		}
	}()

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("reading migration version: %w", err)
	}

	if dirty {
		return fmt.Errorf("database is in dirty migration state at version %d — manual intervention required", version)
	}

	logger.Info().
		Uint("current_version", version).
		Msg("running database migrations")

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			logger.Info().Msg("database schema is up to date")
			return nil
		}
		return fmt.Errorf("running migrations: %w", err)
	}

	newVersion, _, _ := m.Version()
	logger.Info().
		Uint("version", newVersion).
		Msg("migrations applied successfully")

	return nil
}

// MigrateDown rolls back the last migration — use with caution.
func MigrateDown(cfg config.DatabaseConfig, migrationsCfg config.MigrationsConfig, logger zerolog.Logger) error {
	sourceURL := fmt.Sprintf("file://%s", migrationsCfg.Path)
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User, cfg.Password,
		cfg.Host, cfg.Port,
		cfg.Name,
		cfg.SSLMode,
	)

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			logger.Error().Err(srcErr).Msg("migration source close error")
		}
		if dbErr != nil {
			logger.Error().Err(dbErr).Msg("migration db close error")
		}
	}()

	logger.Warn().Msg("rolling back last migration")

	if err := m.Steps(-1); err != nil {
		return fmt.Errorf("rolling back migration: %w", err)
	}

	return nil
}
