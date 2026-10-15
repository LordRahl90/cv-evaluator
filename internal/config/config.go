package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"

	CleanupProviderDeepSeek = "deepseek"
	CleanupProviderOllama   = "ollama"

	defaultPort = "8080"
)

// Database holds the Postgres connection settings.
type Database struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
}

// DSN returns the Postgres connection string for these settings.
func (d Database) DSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		d.Host, d.Port, d.User, d.Password, d.Name)
}

// Config is the application configuration, read once at startup.
type Config struct {
	Environment     string
	Port            string
	Database        Database
	OllamaBaseURL   string
	DeepSeekAPIKey  string
	CleanupProvider string
	JWTSecret       string
}

// IsDevelopment reports whether the app runs in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Environment == EnvDevelopment
}

// Load reads the configuration from environment variables and validates it.
// ENVIRONMENT defaults to production, so a missing setting fails closed.
// Outside development, JWT_SECRET is required; in development a random
// secret is generated when it is empty.
func Load() (*Config, error) {
	cfg := &Config{
		Environment: strings.ToLower(envOr("ENVIRONMENT", EnvProduction)),
		Port:        envOr("PORT", defaultPort),
		Database: Database{
			Host:     env("DB_HOST"),
			Port:     env("DB_PORT"),
			Name:     env("DB_NAME"),
			User:     env("DB_USER"),
			Password: env("DB_PASSWORD"),
		},
		OllamaBaseURL:   strings.TrimRight(env("OLLAMA_BASE_URL"), "/"),
		DeepSeekAPIKey:  env("DEEPSEEK_API_KEY"),
		CleanupProvider: strings.ToLower(envOr("CV_CLEANUP_PROVIDER", CleanupProviderDeepSeek)),
		JWTSecret:       env("JWT_SECRET"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if cfg.JWTSecret == "" {
		secret, err := randomSecret()
		if err != nil {
			return nil, err
		}
		cfg.JWTSecret = secret
		slog.Warn("JWT_SECRET not set; using an ephemeral secret for development. Tokens will not survive a restart.")
	}

	return cfg, nil
}

// Validate checks that every required setting is present and valid.
func (c *Config) Validate() error {
	var errs []error

	required := []struct{ key, value string }{
		{"DB_HOST", c.Database.Host},
		{"DB_PORT", c.Database.Port},
		{"DB_NAME", c.Database.Name},
		{"DB_USER", c.Database.User},
		{"OLLAMA_BASE_URL", c.OllamaBaseURL},
		{"DEEPSEEK_API_KEY", c.DeepSeekAPIKey},
	}
	for _, r := range required {
		if r.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", r.key))
		}
	}

	if c.JWTSecret == "" && !c.IsDevelopment() {
		errs = append(errs, fmt.Errorf("JWT_SECRET is required when ENVIRONMENT is %q", c.Environment))
	}

	switch c.CleanupProvider {
	case CleanupProviderDeepSeek, CleanupProviderOllama:
	default:
		errs = append(errs, fmt.Errorf("CV_CLEANUP_PROVIDER must be %q or %q, got %q",
			CleanupProviderDeepSeek, CleanupProviderOllama, c.CleanupProvider))
	}

	return errors.Join(errs...)
}

func env(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func envOr(key, fallback string) string {
	if v := env(key); v != "" {
		return v
	}
	return fallback
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate JWT secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
