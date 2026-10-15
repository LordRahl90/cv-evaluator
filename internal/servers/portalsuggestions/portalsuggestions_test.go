package portalsuggestions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/servers/middleware"
	portalsvc "cv-evaluator/internal/services/portalsuggestions"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var testJWTSecret = []byte("portal-suggestions-handler-test-secret")

const testAPIKey = "sk_live_portalhandlertest"

type MockService struct {
	mock.Mock
}

func (m *MockService) Create(ctx context.Context, userID ulid.ULID, p portalsvc.CreateParams) (*models.PortalSuggestion, error) {
	args := m.Called(ctx, userID, p)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PortalSuggestion), args.Error(1)
}

func (m *MockService) List(ctx context.Context, userID ulid.ULID) ([]models.PortalSuggestion, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.PortalSuggestion), args.Error(1)
}

func newTestRouter(svc Service, apiKeyUser ulid.ULID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	verifier := middleware.APIKeyVerifierFunc(func(_ context.Context, rawKey string) (ulid.ULID, error) {
		if rawKey != testAPIKey {
			return ulid.ULID{}, middleware.ErrInvalidToken
		}
		return apiKeyUser, nil
	})
	New(r, svc, testJWTSecret, verifier)
	return r
}

func bearerJWT(t *testing.T, userID ulid.ULID) string {
	t.Helper()
	now := time.Now()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(testJWTSecret)
	require.NoError(t, err)
	return "Bearer " + tok
}

func doRequest(router *gin.Engine, method, path, auth, body string) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

type apiErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Param   string `json:"param"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body
}

func sample(userID ulid.ULID) *models.PortalSuggestion {
	ts := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	return &models.PortalSuggestion{
		ID: ulid.Make(), CreatedAt: ts, UpdatedAt: ts, UserID: userID,
		Name: "StepStone", URL: "https://stepstone.de", Note: "Germany", Status: models.PortalSuggestionSubmitted,
	}
}

func TestCreate_Success(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	rec := sample(userID)
	svc.On("Create", mock.Anything, userID, portalsvc.CreateParams{Name: "StepStone", URL: "stepstone.de", Note: "Germany"}).
		Return(rec, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/portal-suggestions", bearerJWT(t, userID),
		`{"name":"StepStone","url":"stepstone.de","note":"Germany"}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.JSONEq(t, `{
		"object":"portal_suggestion","id":"`+rec.ID.String()+`","name":"StepStone","url":"https://stepstone.de",
		"note":"Germany","status":"submitted","created_at":"2026-03-01T08:00:00Z","updated_at":"2026-03-01T08:00:00Z"
	}`, w.Body.String())
	svc.AssertExpectations(t)
}

func TestCreate_InvalidJSON(t *testing.T) {
	svc := new(MockService)

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/portal-suggestions", bearerJWT(t, ulid.Make()), `{`)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_json", decodeError(t, w).Error.Code)
	svc.AssertNotCalled(t, "Create")
}

func TestCreate_Errors(t *testing.T) {
	existing := sample(ulid.Make())
	cases := []struct {
		name     string
		rec      *models.PortalSuggestion
		err      error
		status   int
		code     string
		param    string
		contains string
	}{
		{"missing", nil, &portalsvc.ValidationError{Param: "url", Missing: true, Message: "m"}, 400, "parameter_missing", "url", ""},
		{"invalid", nil, &portalsvc.ValidationError{Param: "note", Message: "m"}, 400, "parameter_invalid", "note", ""},
		{"duplicate", existing, portalsvc.ErrAlreadySuggested, 409, CodePortalAlreadySuggested, "url", existing.ID.String()},
		{"duplicate without record", nil, portalsvc.ErrAlreadySuggested, 409, CodePortalAlreadySuggested, "url", ""},
		{"supported", nil, portalsvc.ErrAlreadySupported, 409, CodePortalAlreadySupported, "url", ""},
		{"timeout", nil, context.DeadlineExceeded, 504, "timeout", "", ""},
		{"internal", nil, errors.New("pg down"), 500, "internal_error", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(MockService)
			svc.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(tc.rec, tc.err).Once()

			w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/portal-suggestions", bearerJWT(t, ulid.Make()), `{"url":"x"}`)

			require.Equal(t, tc.status, w.Code)
			body := decodeError(t, w)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
			assert.Contains(t, body.Error.Message, tc.contains)
			assert.NotContains(t, w.Body.String(), "pg down")
		})
	}
}

func TestList_WithAPIKey(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	recs := []models.PortalSuggestion{*sample(userID), *sample(userID)}
	svc.On("List", mock.Anything, userID).Return(recs, nil).Once()

	w := doRequest(newTestRouter(svc, userID), http.MethodGet, "/portal-suggestions", "Bearer "+testAPIKey, "")

	require.Equal(t, http.StatusOK, w.Code)
	var resp ListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, recs[1].ID.String(), resp.Data[1].ID)
	svc.AssertExpectations(t)
}

func TestList_EmptyIsArray(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	svc.On("List", mock.Anything, userID).Return([]models.PortalSuggestion{}, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/portal-suggestions", bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"object":"list","data":[]}`, w.Body.String())
}

func TestList_Error(t *testing.T) {
	svc := new(MockService)
	svc.On("List", mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/portal-suggestions", bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRoutes_RequireAuth(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			svc := new(MockService)
			router := newTestRouter(svc, ulid.Make())

			w := doRequest(router, method, "/portal-suggestions", "", "")
			require.Equal(t, http.StatusUnauthorized, w.Code)

			w = doRequest(router, method, "/portal-suggestions", "Bearer sk_live_wrong", "")
			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, "invalid_api_key", decodeError(t, w).Error.Code)
			svc.AssertExpectations(t)
		})
	}
}

func TestHandlers_NoMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{svc: new(MockService)}
	for _, fn := range []gin.HandlerFunc{h.create, h.list} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/portal-suggestions", nil)
		fn(c)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	}
}
