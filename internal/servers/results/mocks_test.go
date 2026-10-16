package results

import (
	"context"

	resultsvc "cv-evaluator/internal/services/results"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/mock"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) detail(args mock.Arguments) (*resultsvc.Detail, error) {
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*resultsvc.Detail), args.Error(1)
}

func (m *MockService) List(ctx context.Context, userID ulid.ULID, p resultsvc.ListParams) (*resultsvc.Page, error) {
	args := m.Called(ctx, userID, p)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*resultsvc.Page), args.Error(1)
}

func (m *MockService) Get(ctx context.Context, userID, id ulid.ULID) (*resultsvc.Detail, error) {
	return m.detail(m.Called(ctx, userID, id))
}

func (m *MockService) UpdateStatus(ctx context.Context, userID, id ulid.ULID, status string) (*resultsvc.Detail, error) {
	return m.detail(m.Called(ctx, userID, id, status))
}

func (m *MockService) ReEvaluate(ctx context.Context, userID, id ulid.ULID) (*resultsvc.Detail, error) {
	return m.detail(m.Called(ctx, userID, id))
}

func (m *MockService) Summary(ctx context.Context, userID ulid.ULID) (*resultsvc.Summary, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*resultsvc.Summary), args.Error(1)
}
