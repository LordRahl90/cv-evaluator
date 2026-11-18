package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"

	CleanupProviderDeepSeek = "deepseek"
	CleanupProviderOllama   = "ollama"

	EmbeddingProviderOpenAI = "openai"
	EmbeddingProviderOllama = "ollama"

	defaultPort = "8080"

	// OTLPProtocolHTTP is the only OTLP transport the exporters support.
	OTLPProtocolHTTP = "http/protobuf"

	defaultServiceVersion = "dev"

	// DefaultEvalDailyCap is WORKER_EVAL_DAILY_CAP when it is not set.
	DefaultEvalDailyCap = 30

	// minJWTSecretBytes is the shortest JWT_SECRET accepted outside
	// development: HS256 is only as strong as its key.
	minJWTSecretBytes = 32
	// exampleJWTSecret is the placeholder from .env.example and the README.
	exampleJWTSecret = "change-me-with-a-long-random-value"
)

// Database holds the Postgres connection settings.
type Database struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
	// SSLMode is DB_SSLMODE. It defaults to require, or to disable in
	// development, where Postgres runs in docker-compose without TLS.
	SSLMode string
}

// SSL modes accepted in DB_SSLMODE.
var sslModes = []string{"disable", "require", "verify-ca", "verify-full"}

// DefaultSSLMode returns the DB_SSLMODE used when none is set.
func DefaultSSLMode(environment string) string {
	if strings.EqualFold(environment, EnvDevelopment) {
		return "disable"
	}
	return "require"
}

// DSN returns the Postgres connection URL for these settings. Every part is
// escaped, so passwords may contain any character.
func (d Database) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password),
		Host:     net.JoinHostPort(d.Host, d.Port),
		Path:     "/" + d.Name,
		RawQuery: url.Values{"sslmode": {d.SSLMode}}.Encode(),
	}
	return u.String()
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
	Environment string
	Port        string
	Database    Database
	// OllamaBaseURL is OLLAMA_BASE_URL, required only when Ollama embeds
	// or cleans up CVs.
	OllamaBaseURL   string
	DeepSeekAPIKey  string
	CleanupProvider string
	// EmbeddingProvider is EMBEDDING_PROVIDER: openai (default) or ollama.
	// Vectors from different models are not comparable, so switching means
	// recomputing every stored embedding.
	EmbeddingProvider string
	// OpenAIAPIKey is OPENAI_API_KEY, required when EmbeddingProvider is openai.
	OpenAIAPIKey string
	JWTSecret    string
	Telemetry    Telemetry
	// MigrateOnStart is MIGRATE_ON_START (default true): whether the API and
	// worker migrate the database when they start. Deployments with several
	// replicas set it to false and run `cv-api migrate` once per release.
	MigrateOnStart bool
	// RedisURL is REDIS_URL, e.g. redis://default:pw@valkey:6379/0. When
	// set, rate limits are shared by every API replica; otherwise each
	// process keeps its own.
	RedisURL string
	// WebDir is WEB_DIR: a built web app for the API to serve. When set,
	// the API moves under /api. Empty serves only the API, at the root.
	WebDir string
	// TrustedProxies is TRUSTED_PROXIES: comma-separated proxy IPs or CIDRs
	// whose X-Forwarded-For is believed. Empty trusts none.
	TrustedProxies []string
	// EvalDailyCap is WORKER_EVAL_DAILY_CAP (default DefaultEvalDailyCap):
	// how many results per user the worker sends to the LLM in any 24
	// hours, 0 for no cap. The worker applies it; the API reads it to tell
	// users why a result is still waiting, so set it the same on both.
	EvalDailyCap int

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
	environment := strings.ToLower(envOr("ENVIRONMENT", EnvProduction))
	cfg := &Config{
		Environment: environment,
		Port:        envOr("PORT", defaultPort),
		Database: Database{
			Host:     env("DB_HOST"),
			Port:     env("DB_PORT"),
			Name:     env("DB_NAME"),
			User:     env("DB_USER"),
			Password: env("DB_PASSWORD"),
			SSLMode:  strings.ToLower(envOr("DB_SSLMODE", DefaultSSLMode(environment))),
		},
		OllamaBaseURL:     strings.TrimRight(env("OLLAMA_BASE_URL"), "/"),
		DeepSeekAPIKey:    env("DEEPSEEK_API_KEY"),
		CleanupProvider:   strings.ToLower(envOr("CV_CLEANUP_PROVIDER", CleanupProviderDeepSeek)),
		EmbeddingProvider: strings.ToLower(envOr("EMBEDDING_PROVIDER", EmbeddingProviderOpenAI)),
		OpenAIAPIKey:      env("OPENAI_API_KEY"),
		JWTSecret:         env("JWT_SECRET"),
		TrustedProxies:    list(env("TRUSTED_PROXIES")),
		RedisURL:          env("REDIS_URL"),
		WebDir:            env("WEB_DIR"),
		MigrateOnStart:    !strings.EqualFold(env("MIGRATE_ON_START"), "false"),
		EvalDailyCap:      nonNegativeInt(envOr("WORKER_EVAL_DAILY_CAP", strconv.Itoa(DefaultEvalDailyCap))),
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
		{"DEEPSEEK_API_KEY", c.DeepSeekAPIKey},
	}
	if c.EmbeddingProvider == EmbeddingProviderOpenAI {
		required = append(required, struct{ key, value string }{"OPENAI_API_KEY", c.OpenAIAPIKey})
	}
	if c.usesOllama() {
		required = append(required, struct{ key, value string }{"OLLAMA_BASE_URL", c.OllamaBaseURL})
	}
	for _, r := range required {
		if r.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", r.key))
		}
	}

	if !c.IsDevelopment() && !c.jwtSecretOptional {
		switch {
		case c.JWTSecret == "":
			errs = append(errs, fmt.Errorf("JWT_SECRET is required when ENVIRONMENT is %q", c.Environment))
		case c.JWTSecret == exampleJWTSecret:
			errs = append(errs, errors.New("JWT_SECRET is the example value; generate one with: openssl rand -hex 32"))
		case len(c.JWTSecret) < minJWTSecretBytes:
			errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d bytes when ENVIRONMENT is %q", minJWTSecretBytes, c.Environment))
		}
	}

	if !slices.Contains(sslModes, c.Database.SSLMode) {
		errs = append(errs, fmt.Errorf("DB_SSLMODE must be one of %s, got %q", strings.Join(sslModes, ", "), c.Database.SSLMode))
	}

	switch c.CleanupProvider {
	case CleanupProviderDeepSeek, CleanupProviderOllama:
	default:
		errs = append(errs, fmt.Errorf("CV_CLEANUP_PROVIDER must be %q or %q, got %q",
			CleanupProviderDeepSeek, CleanupProviderOllama, c.CleanupProvider))
	}

	switch c.EmbeddingProvider {
	case EmbeddingProviderOpenAI, EmbeddingProviderOllama:
	default:
		errs = append(errs, fmt.Errorf("EMBEDDING_PROVIDER must be %q or %q, got %q",
			EmbeddingProviderOpenAI, EmbeddingProviderOllama, c.EmbeddingProvider))
	}

	if c.EvalDailyCap < 0 {
		errs = append(errs, fmt.Errorf("WORKER_EVAL_DAILY_CAP must be 0 or a positive integer, got %q", env("WORKER_EVAL_DAILY_CAP")))
	}

	errs = append(errs, c.Telemetry.validate())

	return errors.Join(errs...)
}

// usesOllama reports whether Ollama embeds or cleans up CVs.
func (c *Config) usesOllama() bool {
	return c.EmbeddingProvider == EmbeddingProviderOllama || c.CleanupProvider == CleanupProviderOllama
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

// list splits a comma-separated value, dropping empty items.
func list(v string) []string {
	var out []string
	for item := range strings.SplitSeq(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// nonNegativeInt parses v, returning -1 when it is not a non-negative
// integer so that Validate reports it.
func nonNegativeInt(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return -1
	}
	return n
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
