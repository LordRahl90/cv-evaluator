package match

import (
	"context"

	"cv-evaluator/internal/services/matcher"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/mock"
)

type MockJobMatcher struct {
	mock.Mock
}

func (m *MockJobMatcher) MatchByJobID(ctx context.Context, userID ulid.ULID, jobID string) (*matcher.Response, error) {
	args := m.Called(ctx, userID, jobID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*matcher.Response), args.Error(1)
}

func (m *MockJobMatcher) MatchByJobDescription(ctx context.Context, userID ulid.ULID, jobDescription string) (*matcher.Response, error) {
	args := m.Called(ctx, userID, jobDescription)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*matcher.Response), args.Error(1)
}
