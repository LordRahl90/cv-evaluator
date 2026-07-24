package jobs

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type MockEmbeddingLLMService struct {
	mock.Mock
}

func (m *MockEmbeddingLLMService) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	args := m.Called(ctx, text)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]float32), args.Error(1)
}
