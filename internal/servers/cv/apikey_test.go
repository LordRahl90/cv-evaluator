package cv

import (
	"context"
	"net/http"
	"testing"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/servers/middleware"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const testAPIKey = "sk_live_cvhandlertest"

func newAPIKeyRouter(svc Service, userID ulid.ULID) *gin.Engine {
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

func TestListCVs_APIKey(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()
	svc.On("ListUserCVs", mock.Anything, userID).Return([]models.CV{}, nil).Once()

	w := doRequest(newAPIKeyRouter(svc, userID), http.MethodGet, "/cv/", "Bearer "+testAPIKey, "")

	assert.Equal(t, http.StatusOK, w.Code)
	svc.AssertExpectations(t)
}

func TestListCVs_InvalidAPIKey(t *testing.T) {
	svc := new(MockCVService)

	w := doRequest(newAPIKeyRouter(svc, ulid.Make()), http.MethodGet, "/cv/", "Bearer sk_live_wrong", "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "ListUserCVs")
}

func TestListCVs_APIKeyRejectedWithoutVerifier(t *testing.T) {
	svc := new(MockCVService)

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/", "Bearer "+testAPIKey, "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "ListUserCVs")
}
