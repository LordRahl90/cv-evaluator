package jobsearches

import (
	"context"

	"cv-evaluator/internal/models"
	jobsearchsvc "cv-evaluator/internal/services/jobsearches"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/mock"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) result(args mock.Arguments) (*models.JobSearch, error) {
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.JobSearch), args.Error(1)
}

func (m *MockService) Create(ctx context.Context, userID ulid.ULID, p jobsearchsvc.CreateParams) (*models.JobSearch, error) {
	return m.result(m.Called(ctx, userID, p))
}

func (m *MockService) List(ctx context.Context, userID ulid.ULID) ([]models.JobSearch, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.JobSearch), args.Error(1)
}

func (m *MockService) Get(ctx context.Context, userID, id ulid.ULID) (*models.JobSearch, error) {
	return m.result(m.Called(ctx, userID, id))
}

func (m *MockService) Update(ctx context.Context, userID, id ulid.ULID, p jobsearchsvc.UpdateParams) (*models.JobSearch, error) {
	return m.result(m.Called(ctx, userID, id, p))
}

func (m *MockService) Delete(ctx context.Context, userID, id ulid.ULID) error {
	return m.Called(ctx, userID, id).Error(0)
}

func (m *MockService) RunNow(ctx context.Context, userID, id ulid.ULID) (*models.JobSearch, error) {
	return m.result(m.Called(ctx, userID, id))
}
