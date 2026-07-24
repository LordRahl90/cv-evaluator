package cleanup

import (
	"context"
	"cv-evaluator/internal/integrations/extractor"
)

type LLMClient interface {
	CleanupCV(ctx context.Context, content string) (string, error)
}

type Extractor interface {
	ExtractContent(ctx context.Context, fileName string) (string, error)
}

type Service struct {
	llmService LLMClient
}

func New(llmClient LLMClient) *Service {
	return &Service{
		llmService: llmClient,
	}
}

func (s *Service) CleanupCV(ctx context.Context, fileName string) (string, error) {
	// first we extract
	// then we call the llm
	// then we return the result
	extractResult, err := extractor.Extract(fileName)
	if err != nil {
		return "", err
	}

	result, err := s.llmService.CleanupCV(ctx, extractResult)
	if err != nil {
		return "", err
	}

	return result, nil
}
