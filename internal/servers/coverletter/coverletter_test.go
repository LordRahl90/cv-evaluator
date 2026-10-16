package coverletter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/match"
	"cv-evaluator/internal/servers/ratelimit"
	coverlettersvc "cv-evaluator/internal/services/coverletter"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/services/users"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var testJWTSecret = []byte("cover-letter-handler-test-secret")

type mockWriter struct {
	mock.Mock
}

func (m *mockWriter) Generate(ctx context.Context, userID ulid.ULID, req coverlettersvc.Request) (*coverlettersvc.Letter, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*coverlettersvc.Letter), args.Error(1)
}

func makeToken(userID ulid.ULID) string {
	now := time.Now()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(testJWTSecret)
	if err != nil {
		panic(err)
	}
	return tok
}

func newRouter(w Writer, limiter *ratelimit.Limiter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if limiter == nil {
		New(router, w, testJWTSecret)
	} else {
		NewWithLimiter(router, w, limiter, testJWTSecret)
	}
	return router
}

func post(router *gin.Engine, userID ulid.ULID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/match/cover-letter", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != (ulid.ULID{}) {
		req.Header.Set("Authorization", "Bearer "+makeToken(userID))
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) apierror.Error {
	t.Helper()
	var body apierror.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body.Error
}

func TestCreate_ReturnsTheLetter(t *testing.T) {
	svc := new(mockWriter)
	userID := ulid.Make()
	want := coverlettersvc.Request{
		JobDescription: "Senior Go engineer",
		Evaluation: &matcher.Response{
			OverallScore: 86,
			Requirements: []matcher.Requirement{{Requirement: "Go", Importance: "must", Met: "yes", Evidence: "6 years"}},
			Strengths:    []string{"6 years of Go"},
			Summary:      "Strong fit.",
		},
	}
	svc.On("Generate", mock.Anything, userID, want).
		Return(&coverlettersvc.Letter{Subject: "Application for Senior Go Engineer", Letter: "Dear Hiring Manager,\n\n...", WordCount: 280}, nil)

	w := post(newRouter(svc, nil), userID, `{
		"job_description": "  Senior Go engineer  ",
		"evaluation": {"overall_score": 86, "requirements": [{"requirement":"Go","importance":"must","met":"yes","evidence":"6 years"}],
			"strengths": ["6 years of Go"], "summary": "Strong fit."}
	}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"subject":"Application for Senior Go Engineer","letter":"Dear Hiring Manager,\n\n...","word_count":280}`, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	svc.AssertExpectations(t)
}

func TestCreate_EvaluationIsOptional(t *testing.T) {
	svc := new(mockWriter)
	userID := ulid.Make()
	svc.On("Generate", mock.Anything, userID, coverlettersvc.Request{JobDescription: "Go"}).
		Return(&coverlettersvc.Letter{Subject: "s", Letter: "l", WordCount: 1}, nil)

	w := post(newRouter(svc, nil), userID, `{"job_description":"Go"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	svc.AssertExpectations(t)
}

func TestCreate_InvalidRequests(t *testing.T) {
	tests := []struct {
		name, body, code, param string
	}{
		{"not json", `{`, apierror.CodeInvalidJSON, ""},
		{"wrong evaluation type", `{"job_description":"Go","evaluation":{"overall_score":"high"}}`, apierror.CodeInvalidJSON, ""},
		{"missing job", `{}`, apierror.CodeParameterMissing, "job_description"},
		{"blank job", `{"job_description":"   "}`, apierror.CodeParameterMissing, "job_description"},
		{"long job", fmt.Sprintf(`{"job_description":%q}`, strings.Repeat("a", coverlettersvc.MaxJobDescriptionChars+1)), apierror.CodeParameterInvalid, "job_description"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := new(mockWriter)
			w := post(newRouter(svc, nil), ulid.Make(), tt.body)
			require.Equal(t, http.StatusBadRequest, w.Code)
			got := decodeError(t, w)
			assert.Equal(t, apierror.TypeInvalidRequest, got.Type)
			assert.Equal(t, tt.code, got.Code)
			assert.Equal(t, tt.param, got.Param)
			svc.AssertNotCalled(t, "Generate")
		})
	}
}

func TestCreate_RequiresAuthentication(t *testing.T) {
	svc := new(mockWriter)
	w := post(newRouter(svc, nil), ulid.ULID{}, `{"job_description":"Go"}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "Generate")
}

func TestCreate_ServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		typ    string
		code   string
	}{
		{"no cv", fmt.Errorf("load: %w", coverlettersvc.ErrNoCV), http.StatusUnprocessableEntity, apierror.TypeInvalidRequest, CodeCVMissing},
		{"checks failed", fmt.Errorf("%w: failed checks cliche", coverlettersvc.ErrGenerationFailed), http.StatusBadGateway, apierror.TypeAPI, CodeCoverLetterFailed},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, apierror.TypeAPI, apierror.CodeTimeout},
		{"account gone", users.ErrUserNotFound, http.StatusNotFound, apierror.TypeInvalidRequest, apierror.CodeResourceMissing},
		{"unexpected", errors.New("db down"), http.StatusInternalServerError, apierror.TypeAPI, apierror.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := new(mockWriter)
			svc.On("Generate", mock.Anything, mock.Anything, mock.Anything).Return(nil, tt.err)

			w := post(newRouter(svc, nil), ulid.Make(), `{"job_description":"Go"}`)
			require.Equal(t, tt.status, w.Code)
			got := decodeError(t, w)
			assert.Equal(t, tt.typ, got.Type)
			assert.Equal(t, tt.code, got.Code)
			assert.NotContains(t, got.Message, "cliche", "internal details stay in the logs")
			assert.NotContains(t, got.Message, "db down")
		})
	}
}

func TestCreate_RateLimitedPerUser(t *testing.T) {
	svc := new(mockWriter)
	svc.On("Generate", mock.Anything, mock.Anything, mock.Anything).Return(&coverlettersvc.Letter{Subject: "s", Letter: "l", WordCount: 1}, nil)
	router := newRouter(svc, ratelimit.New(2, time.Minute))
	alice, bob := ulid.Make(), ulid.Make()

	assert.Equal(t, http.StatusBadRequest, post(router, alice, `{}`).Code, "an invalid request")
	assert.Equal(t, http.StatusOK, post(router, alice, `{"job_description":"Go"}`).Code)
	assert.Equal(t, http.StatusOK, post(router, alice, `{"job_description":"Go"}`).Code, "the invalid request did not count")

	w := post(router, alice, `{"job_description":"Go"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	got := decodeError(t, w)
	assert.Equal(t, apierror.TypeRateLimit, got.Type)
	assert.Equal(t, apierror.CodeRateLimited, got.Code)
	assert.Contains(t, got.Message, "at most 2 per minute")
	assert.NotEmpty(t, w.Header().Get("Retry-After"))

	assert.Equal(t, http.StatusOK, post(router, bob, `{"job_description":"Go"}`).Code, "other users are not limited")
	svc.AssertNumberOfCalls(t, "Generate", 3)
}

func TestNew_LivesNextToTheMatchRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	match.New(router, nil, testJWTSecret)
	svc := new(mockWriter)
	svc.On("Generate", mock.Anything, mock.Anything, mock.Anything).Return(&coverlettersvc.Letter{}, nil)
	New(router, svc, testJWTSecret)

	assert.Equal(t, http.StatusOK, post(router, ulid.Make(), `{"job_description":"Go"}`).Code)
}

func TestNew_UsesTheDefaultLimit(t *testing.T) {
	svc := new(mockWriter)
	svc.On("Generate", mock.Anything, mock.Anything, mock.Anything).Return(&coverlettersvc.Letter{}, nil)
	router := newRouter(svc, nil)
	userID := ulid.Make()
	for range DefaultLimit {
		require.Equal(t, http.StatusOK, post(router, userID, `{"job_description":"Go"}`).Code)
	}
	assert.Equal(t, http.StatusTooManyRequests, post(router, userID, `{"job_description":"Go"}`).Code)
}
