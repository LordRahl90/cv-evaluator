package llm

import (
	"context"
	"fmt"

	"cv-evaluator/internal/config"
)

// Embedder turns text into a 768-dimension vector.
type Embedder interface {
	CreateEmbedding(ctx context.Context, content string) ([]float32, error)
}

// NewEmbedder returns the embedder for the configured provider.
func NewEmbedder(provider string, openai, ollama Embedder) (Embedder, error) {
	switch provider {
	case config.EmbeddingProviderOpenAI:
		return openai, nil
	case config.EmbeddingProviderOllama:
		return ollama, nil
	default:
		return nil, fmt.Errorf("unknown embedding provider %q", provider)
	}
}
