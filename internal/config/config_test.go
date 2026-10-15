package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("PORT", "")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_NAME", "cv_evaluator")
	t.Setenv("DB_USER", "postgres")
	t.Setenv("DB_PASSWORD", "postgres")
	t.Setenv("OLLAMA_BASE_URL", "http://localhost:11434/")
	t.Setenv("DEEPSEEK_API_KEY", "key")
	t.Setenv("CV_CLEANUP_PROVIDER", "")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "")
	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("SERVICE_VERSION", "")
}

func TestLoad_Defaults(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, EnvProduction, cfg.Environment)
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, CleanupProviderDeepSeek, cfg.CleanupProvider)
	assert.Equal(t, "http://localhost:11434", cfg.OllamaBaseURL)
	assert.Equal(t, "secret", cfg.JWTSecret)
	assert.Equal(t, "host=localhost port=5432 user=postgres password=postgres dbname=cv_evaluator sslmode=disable", cfg.Database.DSN())
}

func TestLoad_MissingJWTSecretInProduction(t *testing.T) {
	setValidEnv(t)
	t.Setenv("JWT_SECRET", "")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET is required")
}

func TestLoad_MissingJWTSecretInDevelopment(t *testing.T) {
	setValidEnv(t)
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("JWT_SECRET", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.True(t, cfg.IsDevelopment())
	assert.NotEmpty(t, cfg.JWTSecret)
}

func TestLoad_MissingRequired(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DB_HOST", "")
	t.Setenv("DEEPSEEK_API_KEY", "")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_HOST is required")
	assert.Contains(t, err.Error(), "DEEPSEEK_API_KEY is required")
}

func TestLoad_InvalidCleanupProvider(t *testing.T) {
	setValidEnv(t)
	t.Setenv("CV_CLEANUP_PROVIDER", "openai")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CV_CLEANUP_PROVIDER")
}

func TestLoad_TelemetryDefaults(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	require.NoError(t, err)

	assert.False(t, cfg.Telemetry.Enabled())
	assert.Equal(t, OTLPProtocolHTTP, cfg.Telemetry.OTLPProtocol)
	assert.Equal(t, "dev", cfg.Telemetry.ServiceVersion)
}

func TestLoad_TelemetryEnabled(t *testing.T) {
	setValidEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	t.Setenv("SERVICE_VERSION", "1.2.3")

	cfg, err := Load()
	require.NoError(t, err)

	assert.True(t, cfg.Telemetry.Enabled())
	assert.Equal(t, "http://localhost:4318", cfg.Telemetry.OTLPEndpoint)
	assert.Equal(t, "1.2.3", cfg.Telemetry.ServiceVersion)
}

func TestLoad_TelemetrySDKDisabled(t *testing.T) {
	setValidEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	t.Setenv("OTEL_SDK_DISABLED", "true")

	cfg, err := Load()
	require.NoError(t, err)
	assert.False(t, cfg.Telemetry.Enabled())
}

func TestLoad_TelemetryInvalid(t *testing.T) {
	tests := map[string]struct {
		key, value, want string
	}{
		"endpoint without scheme": {"OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318", "OTEL_EXPORTER_OTLP_ENDPOINT"},
		"grpc protocol":           {"OTEL_EXPORTER_OTLP_PROTOCOL", "grpc", "OTEL_EXPORTER_OTLP_PROTOCOL"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(tc.key, tc.value)

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
