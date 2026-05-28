package matcher

import (
	"context"

	"gorm.io/gorm"
)

type LLMService interface {
	CreateEmbedding(ctx context.Context, content string) ([]float64, error)
}

type Service struct {
	db  *gorm.DB
	llm LLMService
}

func New(db *gorm.DB, llm LLMService) *Service {
	return &Service{
		db:  db,
		llm: llm,
	}
}

func (s *Service) Match(ctx context.Context, userID int, jobDescription string) (float64, error) {
	// get the user's cv with section embeddings
	
	return 0, nil
}
