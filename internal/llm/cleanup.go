package llm

import (
	"context"
	"fmt"

	"cv-evaluator/internal/config"
)

// CVCleaner splits raw CV text into structured JSON sections.
type CVCleaner interface {
	CleanupCV(ctx context.Context, content string) (string, error)
}

// NewCVCleaner returns the cleaner for the configured provider.
func NewCVCleaner(provider string, deepseek, ollama CVCleaner) (CVCleaner, error) {
	switch provider {
	case config.CleanupProviderDeepSeek:
		return deepseek, nil
	case config.CleanupProviderOllama:
		return ollama, nil
	default:
		return nil, fmt.Errorf("unknown CV cleanup provider %q", provider)
	}
}
