package auth

import (
	"context"

	"cv-evaluator/internal/services/users"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/mock"
)

type MockAuthService struct {
	mock.Mock
}

func (m *MockAuthService) Signup(ctx context.Context, req users.SignupRequest) (*users.AuthResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*users.AuthResponse), args.Error(1)
}

func (m *MockAuthService) Signin(ctx context.Context, req users.SigninRequest) (*users.AuthResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*users.AuthResponse), args.Error(1)
}

func (m *MockAuthService) GetProfile(ctx context.Context, userID ulid.ULID) (*users.Profile, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*users.Profile), args.Error(1)
}
