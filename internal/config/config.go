// Package config handles all application configuration.
// Supports loading from YAML file and environment variable overrides.
// Environment variables take precedence over file values.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration structure.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Database   DatabaseConfig   `yaml:"database"`
	Migrations MigrationsConfig `yaml:"migrations"`
	Log        LogConfig        `yaml:"log"`
	App        AppConfig        `yaml:"app"`
	Scoring    ScoringConfig    `yaml:"scoring"`
	Embeddings EmbeddingsConfig `yaml:"embeddings"`
	Workers    WorkersConfig    `yaml:"workers"`
	Cache      CacheConfig      `yaml:"cache"`
}

// CacheConfig controls the in-memory cache.
type CacheConfig struct {
	Enabled         bool          `yaml:"enabled"`
	MaxItems        int           `yaml:"max_items"`        // default 5000
	CleanupInterval time.Duration `yaml:"cleanup_interval"` // default 5m
	// TTLs per data type.
	UserTTL      time.Duration `yaml:"user_ttl"`       // default 5m
	MemoryTTL    time.Duration `yaml:"memory_ttl"`     // default 2m
	SearchTTL    time.Duration `yaml:"search_ttl"`     // default 1m
	EmbeddingTTL time.Duration `yaml:"embedding_ttl"`  // default 1h
	RetrievalTTL time.Duration `yaml:"retrieval_ttl"`  // default 30s
}

// WorkersConfig controls background worker schedules.
type WorkersConfig struct {
	Enabled         bool          `yaml:"enabled"`
	DecayInterval   time.Duration `yaml:"decay_interval"`    // default 24h
	ReembedInterval time.Duration `yaml:"reembed_interval"`  // default 1h
	ExpiryInterval  time.Duration `yaml:"expiry_interval"`   // default 1h
	ConsolidateInterval time.Duration `yaml:"consolidate_interval"` // default 6h
	BatchSize       int           `yaml:"batch_size"`        // default 200
}

// EmbeddingsConfig holds embedding provider settings.
type EmbeddingsConfig struct {
	Provider   string `yaml:"provider"`   // "ollama" | "null"
	OllamaURL  string `yaml:"ollama_url"` // default: http://localhost:11434
	Model      string `yaml:"model"`      // e.g. nomic-embed-text
	Version    string `yaml:"version"`
	Dimensions int    `yaml:"dimensions"` // vector dimensions
}

// ScoringConfig holds memory scoring and decay parameters.
// All weights must sum to 1.0 — validated at load time.
type ScoringConfig struct {
	Weights         ScoringWeights            `yaml:"weights"`
	Decay           DecayConfig               `yaml:"decay"`
	InitialSalience map[string]float64        `yaml:"initial_salience"`
}

// ScoringWeights controls the relative importance of each scoring signal.
type ScoringWeights struct {
	Relevance       float64 `yaml:"relevance"`
	Importance      float64 `yaml:"importance"`
	Confidence      float64 `yaml:"confidence"`
	Salience        float64 `yaml:"salience"`
	Recency         float64 `yaml:"recency"`
	AccessFrequency float64 `yaml:"access_frequency"`
}

// Sum returns the total of all weights (should be ~1.0).
func (w ScoringWeights) Sum() float64 {
	return w.Relevance + w.Importance + w.Confidence +
		w.Salience + w.Recency + w.AccessFrequency
}

// DecayConfig controls the salience decay model.
type DecayConfig struct {
	// Lambda is the exponential decay rate: s = s0 * e^(-λ * days_since_access)
	Lambda   float64       `yaml:"lambda"`
	Floor    float64       `yaml:"floor"`
	Interval time.Duration `yaml:"interval"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// Addr returns host:port string.
func (s ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	Host               string        `yaml:"host"`
	Port               int           `yaml:"port"`
	Name               string        `yaml:"name"`
	User               string        `yaml:"user"`
	Password           string        `yaml:"password"`
	SSLMode            string        `yaml:"ssl_mode"`
	MaxConns           int32         `yaml:"max_conns"`
	MinConns           int32         `yaml:"min_conns"`
	MaxConnLifetime    time.Duration `yaml:"max_conn_lifetime"`
	MaxConnIdleTime    time.Duration `yaml:"max_conn_idle_time"`
	HealthCheckPeriod  time.Duration `yaml:"health_check_period"`
}

// DSN returns the PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		d.Host, d.Port, d.Name, d.User, d.Password, d.SSLMode,
	)
}

// MigrationsConfig holds database migration settings.
type MigrationsConfig struct {
	Path  string `yaml:"path"`
	Table string `yaml:"table"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // pretty, json
}

// AppConfig holds application metadata.
type AppConfig struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Environment string `yaml:"environment"`
}

// IsDevelopment returns true when running in development mode.
func (a AppConfig) IsDevelopment() bool {
	return a.Environment == "development"
}

// IsProduction returns true when running in production mode.
func (a AppConfig) IsProduction() bool {
	return a.Environment == "production"
}

// Load reads configuration from the given YAML file path,
// then overlays any environment variable overrides.
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	// Apply environment variable overrides.
	applyEnvOverrides(cfg)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// defaults returns a Config pre-filled with safe defaults.
func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Host:            "0.0.0.0",
			Port:            8080,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
		Database: DatabaseConfig{
			Host:              "localhost",
			Port:              5432,
			Name:              "memory_engine",
			User:              "postgres",
			Password:          "postgres",
			SSLMode:           "disable",
			MaxConns:          25,
			MinConns:          5,
			MaxConnLifetime:   1 * time.Hour,
			MaxConnIdleTime:   30 * time.Minute,
			HealthCheckPeriod: 1 * time.Minute,
		},
		Migrations: MigrationsConfig{
			Path:  "migrations",
			Table: "schema_migrations",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "pretty",
		},
		App: AppConfig{
			Name:        "memory-engine",
			Version:     "1.0.0",
			Environment: "development",
		},
		Scoring: ScoringConfig{
			Weights: ScoringWeights{
				Relevance:       0.40,
				Importance:      0.20,
				Confidence:      0.15,
				Salience:        0.10,
				Recency:         0.10,
				AccessFrequency: 0.05,
			},
			Decay: DecayConfig{
				Lambda:   0.023,
				Floor:    0.05,
				Interval: 24 * time.Hour,
			},
			InitialSalience: map[string]float64{
				"WORKING":      0.9,
				"FACT":         0.6,
				"PREFERENCE":   0.7,
				"PROJECT":      0.65,
				"GOAL":         0.75,
				"INSTRUCTION":  0.85,
				"SKILL":        0.6,
				"EXPERIENCE":   0.55,
				"DECISION":     0.65,
				"KNOWLEDGE":    0.5,
				"RELATIONSHIP": 0.6,
			},
		},
		Embeddings: EmbeddingsConfig{
			Provider:   "null",
			OllamaURL:  "http://localhost:11434",
			Model:      "nomic-embed-text",
			Version:    "v1.5",
			Dimensions: 768,
		},
		Workers: WorkersConfig{
			Enabled:             true,
			DecayInterval:       24 * time.Hour,
			ReembedInterval:     1 * time.Hour,
			ExpiryInterval:      1 * time.Hour,
			ConsolidateInterval: 6 * time.Hour,
			BatchSize:           200,
		},
		Cache: CacheConfig{
			Enabled:         true,
			MaxItems:        5000,
			CleanupInterval: 5 * time.Minute,
			UserTTL:         5 * time.Minute,
			MemoryTTL:       2 * time.Minute,
			SearchTTL:       1 * time.Minute,
			EmbeddingTTL:    1 * time.Hour,
			RetrievalTTL:    30 * time.Second,
		},
	}
}

// applyEnvOverrides overlays environment variables onto the config.
// Supported env vars:
//   APP_ENV, LOG_LEVEL, LOG_FORMAT,
//   SERVER_HOST, SERVER_PORT,
//   DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD, DB_SSL_MODE
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("APP_ENV"); v != "" {
		cfg.App.Environment = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	if v := os.Getenv("SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = p
		}
	}
	if v := os.Getenv("DB_HOST"); v != "" {
		cfg.Database.Host = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Database.Port = p
		}
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.Database.Name = v
	}
	if v := os.Getenv("DB_USER"); v != "" {
		cfg.Database.User = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		cfg.Database.Password = v
	}
	if v := os.Getenv("DB_SSL_MODE"); v != "" {
		cfg.Database.SSLMode = v
	}
}

// validate checks required fields are present.
func validate(cfg *Config) error {
	var errs []string

	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		errs = append(errs, "server.port must be between 1 and 65535")
	}
	if cfg.Database.Host == "" {
		errs = append(errs, "database.host is required")
	}
	if cfg.Database.Name == "" {
		errs = append(errs, "database.name is required")
	}
	if cfg.Database.User == "" {
		errs = append(errs, "database.user is required")
	}

	// Validate scoring weights sum to ~1.0 (allow ±0.01 floating point tolerance).
	weightSum := cfg.Scoring.Weights.Sum()
	if weightSum < 0.99 || weightSum > 1.01 {
		errs = append(errs, fmt.Sprintf(
			"scoring.weights must sum to 1.0, got %.4f", weightSum,
		))
	}

	if cfg.Scoring.Decay.Lambda <= 0 {
		errs = append(errs, "scoring.decay.lambda must be positive")
	}
	if cfg.Scoring.Decay.Floor < 0 || cfg.Scoring.Decay.Floor > 1 {
		errs = append(errs, "scoring.decay.floor must be between 0 and 1")
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
