package apikeys

import (
	"context"

	apikeysvc "cv-evaluator/internal/services/apikeys"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/mock"
)

type MockAPIKeyService struct {
	mock.Mock
}

func (m *MockAPIKeyService) Create(ctx context.Context, userID ulid.ULID, name string) (*apikeysvc.CreatedKey, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*apikeysvc.CreatedKey), args.Error(1)
}

func (m *MockAPIKeyService) List(ctx context.Context, userID ulid.ULID) ([]apikeysvc.Key, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]apikeysvc.Key), args.Error(1)
}

func (m *MockAPIKeyService) Delete(ctx context.Context, userID, keyID ulid.ULID) error {
	args := m.Called(ctx, userID, keyID)
	return args.Error(0)
}

type MockVerifier struct {
	mock.Mock
}

func (m *MockVerifier) Verify(ctx context.Context, rawKey string) (ulid.ULID, error) {
	args := m.Called(ctx, rawKey)
	return args.Get(0).(ulid.ULID), args.Error(1)
}
