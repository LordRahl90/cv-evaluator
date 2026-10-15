package llm

import (
	"context"
	"testing"

	"cv-evaluator/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type namedCleaner string

func (n namedCleaner) CleanupCV(context.Context, string) (string, error) {
	return string(n), nil
}

func TestNewCVCleaner(t *testing.T) {
	deepseek, ollama := namedCleaner("deepseek"), namedCleaner("ollama")

	got, err := NewCVCleaner(config.CleanupProviderDeepSeek, deepseek, ollama)
	require.NoError(t, err)
	assert.Equal(t, deepseek, got)

	got, err = NewCVCleaner(config.CleanupProviderOllama, deepseek, ollama)
	require.NoError(t, err)
	assert.Equal(t, ollama, got)

	_, err = NewCVCleaner("openai", deepseek, ollama)
	require.Error(t, err)
}
