package match

import (
	"context"
	"net/http"
	"testing"

	"cv-evaluator/internal/servers/middleware"
	"cv-evaluator/internal/services/matcher"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const testAPIKey = "sk_live_matchhandlertest"

func newAPIKeyRouter(svc JobMatcher, userID ulid.ULID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	verifier := middleware.APIKeyVerifierFunc(func(_ context.Context, rawKey string) (ulid.ULID, error) {
		if rawKey != testAPIKey {
			return ulid.ULID{}, middleware.ErrInvalidToken
		}
		return userID, nil
	})
	New(r, svc, testJWTSecret, middleware.WithAPIKeyVerifier(verifier))
	return r
}

func TestMatchByJobDescription_APIKey(t *testing.T) {
	svc := new(MockJobMatcher)
	userID := ulid.Make()
	svc.On("MatchByJobDescription", mock.Anything, userID, "Go engineer").
		Return(&matcher.Response{OverallScore: 70}, nil).Once()

	w := performRequestWithAuth(newAPIKeyRouter(svc, userID), http.MethodPost, "/match/job-description",
		`{"job_description":"Go engineer"}`, "Bearer "+testAPIKey)

	assert.Equal(t, http.StatusOK, w.Code)
	svc.AssertExpectations(t)
}

func TestMatchByJobDescription_InvalidAPIKey(t *testing.T) {
	svc := new(MockJobMatcher)

	w := performRequestWithAuth(newAPIKeyRouter(svc, ulid.Make()), http.MethodPost, "/match/job-description",
		`{"job_description":"Go engineer"}`, "Bearer sk_live_wrong")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "MatchByJobDescription")
}
