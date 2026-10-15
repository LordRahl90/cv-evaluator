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

func TestLoad_WithoutJWTSecret(t *testing.T) {
	setValidEnv(t)
	t.Setenv("JWT_SECRET", "")

	cfg, err := Load(WithoutJWTSecret())
	require.NoError(t, err)
	assert.Empty(t, cfg.JWTSecret)
}
