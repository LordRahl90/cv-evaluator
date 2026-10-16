package account

import (
	"context"

	accountsvc "cv-evaluator/internal/services/account"
	"cv-evaluator/internal/services/users"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/mock"
)

type MockProfiles struct {
	mock.Mock
}

func (m *MockProfiles) UpdateProfile(ctx context.Context, userID ulid.ULID, req users.UpdateProfileRequest) (*users.Profile, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*users.Profile), args.Error(1)
}

func (m *MockProfiles) ChangePassword(ctx context.Context, userID ulid.ULID, current, next string) (*users.AuthResponse, error) {
	args := m.Called(ctx, userID, current, next)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*users.AuthResponse), args.Error(1)
}

func (m *MockProfiles) VerifyPassword(ctx context.Context, userID ulid.ULID, password string) error {
	return m.Called(ctx, userID, password).Error(0)
}

type MockData struct {
	mock.Mock
}

func (m *MockData) Delete(ctx context.Context, userID ulid.ULID) ([]accountsvc.Deleted, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]accountsvc.Deleted), args.Error(1)
}

func (m *MockData) Export(ctx context.Context, userID ulid.ULID) (*accountsvc.Export, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*accountsvc.Export), args.Error(1)
}
