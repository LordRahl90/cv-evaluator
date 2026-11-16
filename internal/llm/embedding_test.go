package llm

import (
	"context"
	"testing"

	"cv-evaluator/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type namedEmbedder string

func (n namedEmbedder) CreateEmbedding(context.Context, string) ([]float32, error) {
	return nil, nil
}

func TestNewEmbedder(t *testing.T) {
	openai, ollama := namedEmbedder("openai"), namedEmbedder("ollama")

	got, err := NewEmbedder(config.EmbeddingProviderOpenAI, openai, ollama)
	require.NoError(t, err)
	assert.Equal(t, openai, got)

	got, err = NewEmbedder(config.EmbeddingProviderOllama, openai, ollama)
	require.NoError(t, err)
	assert.Equal(t, ollama, got)

	_, err = NewEmbedder("cohere", openai, ollama)
	require.Error(t, err)
}
