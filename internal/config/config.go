package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"

	CleanupProviderDeepSeek = "deepseek"
	CleanupProviderOllama   = "ollama"

	defaultPort = "8080"

	// OTLPProtocolHTTP is the only OTLP transport the exporters support.
	OTLPProtocolHTTP = "http/protobuf"

	defaultServiceVersion = "dev"
)

// Database holds the Postgres connection settings.
type Database struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
	// TraceStatements is DB_TRACE_STATEMENTS: when true, database spans carry
	// the SQL text (with placeholders, never values) as db.statement.
	TraceStatements bool
}

// DSN returns the Postgres connection string for these settings.
func (d Database) DSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		d.Host, d.Port, d.User, d.Password, d.Name)
}

// Telemetry holds the OpenTelemetry export settings. The exporters also read
// the other standard OTEL_EXPORTER_OTLP_* variables (headers, timeout,
// per-signal endpoints) and OTEL_METRIC_EXPORT_INTERVAL directly.
type Telemetry struct {
	// OTLPEndpoint is OTEL_EXPORTER_OTLP_ENDPOINT, e.g. http://localhost:4318.
	// When empty, telemetry is not exported and no-op providers are used.
	OTLPEndpoint string
	// OTLPProtocol is OTEL_EXPORTER_OTLP_PROTOCOL; only http/protobuf is supported.
	OTLPProtocol string
	// ServiceVersion is SERVICE_VERSION, reported as service.version.
	ServiceVersion string
	// Disabled is OTEL_SDK_DISABLED=true, which turns export off even when
	// an endpoint is set.
	Disabled bool
}

// Enabled reports whether telemetry should be exported.
func (t Telemetry) Enabled() bool {
	return !t.Disabled && t.OTLPEndpoint != ""
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
	Telemetry       Telemetry

	// jwtSecretOptional is set by WithoutJWTSecret for processes that never
	// issue or verify tokens.
	jwtSecretOptional bool
}

// Option changes how Load reads the configuration.
type Option func(*Config)

// WithoutJWTSecret lets a process that never issues or verifies tokens,
// such as the background worker, start without JWT_SECRET.
func WithoutJWTSecret() Option {
	return func(c *Config) { c.jwtSecretOptional = true }
}

// IsDevelopment reports whether the app runs in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Environment == EnvDevelopment
}

// Load reads the configuration from environment variables and validates it.
// ENVIRONMENT defaults to production, so a missing setting fails closed.
// Outside development, JWT_SECRET is required; in development a random
// secret is generated when it is empty. WithoutJWTSecret drops both.
func Load(opts ...Option) (*Config, error) {
	cfg := &Config{
		Environment: strings.ToLower(envOr("ENVIRONMENT", EnvProduction)),
		Port:        envOr("PORT", defaultPort),
		Database: Database{
			Host:     env("DB_HOST"),
			Port:     env("DB_PORT"),
			Name:     env("DB_NAME"),
			User:     env("DB_USER"),
			Password: env("DB_PASSWORD"),

			TraceStatements: strings.EqualFold(env("DB_TRACE_STATEMENTS"), "true"),
		},
		OllamaBaseURL:   strings.TrimRight(env("OLLAMA_BASE_URL"), "/"),
		DeepSeekAPIKey:  env("DEEPSEEK_API_KEY"),
		CleanupProvider: strings.ToLower(envOr("CV_CLEANUP_PROVIDER", CleanupProviderDeepSeek)),
		JWTSecret:       env("JWT_SECRET"),
		Telemetry: Telemetry{
			OTLPEndpoint:   env("OTEL_EXPORTER_OTLP_ENDPOINT"),
			OTLPProtocol:   strings.ToLower(envOr("OTEL_EXPORTER_OTLP_PROTOCOL", OTLPProtocolHTTP)),
			ServiceVersion: envOr("SERVICE_VERSION", defaultServiceVersion),
			Disabled:       strings.EqualFold(env("OTEL_SDK_DISABLED"), "true"),
		},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if cfg.JWTSecret == "" && !cfg.jwtSecretOptional {
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

	if c.JWTSecret == "" && !c.IsDevelopment() && !c.jwtSecretOptional {
		errs = append(errs, fmt.Errorf("JWT_SECRET is required when ENVIRONMENT is %q", c.Environment))
	}

	switch c.CleanupProvider {
	case CleanupProviderDeepSeek, CleanupProviderOllama:
	default:
		errs = append(errs, fmt.Errorf("CV_CLEANUP_PROVIDER must be %q or %q, got %q",
			CleanupProviderDeepSeek, CleanupProviderOllama, c.CleanupProvider))
	}

	errs = append(errs, c.Telemetry.validate())

	return errors.Join(errs...)
}

func (t Telemetry) validate() error {
	var errs []error
	if t.OTLPEndpoint != "" {
		u, err := url.Parse(t.OTLPEndpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must be an http(s) URL such as http://localhost:4318, got %q", t.OTLPEndpoint))
		}
	}
	if t.OTLPProtocol != "" && t.OTLPProtocol != OTLPProtocolHTTP {
		errs = append(errs, fmt.Errorf("OTEL_EXPORTER_OTLP_PROTOCOL must be %q, got %q", OTLPProtocolHTTP, t.OTLPProtocol))
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
