package cv

import (
	"context"
	"mime/multipart"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/mock"
)

type MockCVService struct {
	mock.Mock
}

func (m *MockCVService) UploadCV(ctx context.Context, userID ulid.ULID, cv *multipart.FileHeader) error {
	args := m.Called(ctx, userID, cv)
	return args.Error(0)
}

func (m *MockCVService) ListUserCVs(ctx context.Context, userID ulid.ULID) ([]models.CV, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.CV), args.Error(1)
}

func (m *MockCVService) GetCV(ctx context.Context, userID, cvID ulid.ULID) (*models.CV, error) {
	args := m.Called(ctx, userID, cvID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CV), args.Error(1)
}

func (m *MockCVService) DeleteCV(ctx context.Context, userID, cvID ulid.ULID) error {
	args := m.Called(ctx, userID, cvID)
	return args.Error(0)
}
