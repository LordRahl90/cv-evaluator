package apikeys

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

	"cv-evaluator/internal/servers/middleware"
	apikeysvc "cv-evaluator/internal/services/apikeys"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var testJWTSecret = []byte("apikeys-handler-test-secret")

const testAPIKey = "sk_live_handlertest"

func newTestRouter(svc Service, verifier *MockVerifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	New(r, svc, testJWTSecret, middleware.WithAPIKeyVerifier(NewVerifier(verifier)))
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
		Type  string `json:"type"`
		Code  string `json:"code"`
		Param string `json:"param"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body
}

// ── POST /api-keys ──────────────────────────────────────────────────────────

func TestCreate_Success(t *testing.T) {
	svc := new(MockAPIKeyService)
	userID := ulid.Make()
	created := &apikeysvc.CreatedKey{
		Key: apikeysvc.Key{
			ID:        ulid.Make(),
			Name:      "CI",
			Prefix:    "sk_live_AbCd",
			CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		Secret: "sk_live_AbCd" + strings.Repeat("x", 36), // built at runtime so secret scanners do not flag it
	}
	svc.On("Create", mock.Anything, userID, "CI").Return(created, nil).Once()

	w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodPost, "/api-keys", bearerJWT(t, userID), `{"name":"CI"}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "api_key", resp["object"])
	assert.Equal(t, created.ID.String(), resp["id"])
	assert.Equal(t, "CI", resp["name"])
	assert.Equal(t, "sk_live_AbCd", resp["prefix"])
	assert.Equal(t, created.Secret, resp["secret"])
	assert.Equal(t, "2026-01-02T03:04:05Z", resp["created_at"])
	assert.Contains(t, resp, "last_used_at")
	assert.Nil(t, resp["last_used_at"])
	svc.AssertExpectations(t)
}

func TestCreate_InvalidJSON(t *testing.T) {
	svc := new(MockAPIKeyService)

	w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodPost, "/api-keys", bearerJWT(t, ulid.Make()), `{`)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_json", decodeError(t, w).Error.Code)
	svc.AssertNotCalled(t, "Create")
}

func TestCreate_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"name required", apikeysvc.ErrNameRequired, "parameter_missing"},
		{"name too long", apikeysvc.ErrNameTooLong, "parameter_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(MockAPIKeyService)
			svc.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(nil, tc.err).Once()

			w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodPost, "/api-keys", bearerJWT(t, ulid.Make()), `{"name":""}`)

			require.Equal(t, http.StatusBadRequest, w.Code)
			body := decodeError(t, w)
			assert.Equal(t, "invalid_request_error", body.Error.Type)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, "name", body.Error.Param)
		})
	}
}

func TestCreate_InternalError(t *testing.T) {
	svc := new(MockAPIKeyService)
	svc.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("pg down")).Once()

	w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodPost, "/api-keys", bearerJWT(t, ulid.Make()), `{"name":"x"}`)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "api_error", decodeError(t, w).Error.Type)
	assert.NotContains(t, w.Body.String(), "pg down")
}

// ── GET /api-keys ───────────────────────────────────────────────────────────

func TestList_NeverReturnsSecrets(t *testing.T) {
	svc := new(MockAPIKeyService)
	userID := ulid.Make()
	used := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	keys := []apikeysvc.Key{
		{ID: ulid.Make(), Name: "a", Prefix: "sk_live_aaaa", LastUsedAt: &used},
		{ID: ulid.Make(), Name: "b", Prefix: "sk_live_bbbb"},
	}
	svc.On("List", mock.Anything, userID).Return(keys, nil).Once()

	w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodGet, "/api-keys", bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	var resp ListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, keys[0].ID.String(), resp.Data[0].ID)
	require.NotNil(t, resp.Data[0].LastUsedAt)
	assert.True(t, resp.Data[0].LastUsedAt.Equal(used))
	assert.NotContains(t, w.Body.String(), `"secret"`)
	svc.AssertExpectations(t)
}

func TestList_EmptyIsArray(t *testing.T) {
	svc := new(MockAPIKeyService)
	userID := ulid.Make()
	svc.On("List", mock.Anything, userID).Return([]apikeysvc.Key{}, nil).Once()

	w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodGet, "/api-keys", bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"object":"list","data":[]}`, w.Body.String())
}

// ── DELETE /api-keys/:id ────────────────────────────────────────────────────

func TestDelete_Success(t *testing.T) {
	svc := new(MockAPIKeyService)
	userID, keyID := ulid.Make(), ulid.Make()
	svc.On("Delete", mock.Anything, userID, keyID).Return(nil).Once()

	w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodDelete, "/api-keys/"+keyID.String(), bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"object":"api_key","id":"`+keyID.String()+`","deleted":true}`, w.Body.String())
	svc.AssertExpectations(t)
}

func TestDelete_NotFound(t *testing.T) {
	svc := new(MockAPIKeyService)
	svc.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(apikeysvc.ErrNotFound).Once()

	w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodDelete, "/api-keys/"+ulid.Make().String(), bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusNotFound, w.Code)
	body := decodeError(t, w)
	assert.Equal(t, "resource_missing", body.Error.Code)
	assert.Equal(t, "id", body.Error.Param)
}

func TestDelete_InvalidID(t *testing.T) {
	svc := new(MockAPIKeyService)

	w := doRequest(newTestRouter(svc, new(MockVerifier)), http.MethodDelete, "/api-keys/not-a-ulid", bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "parameter_invalid", decodeError(t, w).Error.Code)
	svc.AssertNotCalled(t, "Delete")
}

// ── authentication ──────────────────────────────────────────────────────────

func TestRoutes_RequireAuth(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api-keys"},
		{http.MethodGet, "/api-keys"},
		{http.MethodDelete, "/api-keys/" + ulid.Make().String()},
	}
	for _, rt := range routes {
		t.Run(rt.method, func(t *testing.T) {
			svc := new(MockAPIKeyService)
			w := doRequest(newTestRouter(svc, new(MockVerifier)), rt.method, rt.path, "", `{"name":"x"}`)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, "authentication_error", decodeError(t, w).Error.Type)
			assert.Empty(t, svc.Calls)
		})
	}
}

func TestRoutes_AcceptAPIKey(t *testing.T) {
	svc := new(MockAPIKeyService)
	verifier := new(MockVerifier)
	owner := ulid.Make()
	verifier.On("Verify", mock.Anything, testAPIKey).Return(owner, nil).Once()
	svc.On("List", mock.Anything, owner).Return([]apikeysvc.Key{}, nil).Once()

	w := doRequest(newTestRouter(svc, verifier), http.MethodGet, "/api-keys", "Bearer "+testAPIKey, "")

	require.Equal(t, http.StatusOK, w.Code)
	svc.AssertExpectations(t)
	verifier.AssertExpectations(t)
}

func TestRoutes_RevokedAPIKey(t *testing.T) {
	svc := new(MockAPIKeyService)
	verifier := new(MockVerifier)
	verifier.On("Verify", mock.Anything, testAPIKey).Return(ulid.ULID{}, apikeysvc.ErrInvalidKey).Once()

	w := doRequest(newTestRouter(svc, verifier), http.MethodGet, "/api-keys", "Bearer "+testAPIKey, "")

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid_api_key", decodeError(t, w).Error.Code)
	assert.Empty(t, svc.Calls)
}

// ── NewVerifier ─────────────────────────────────────────────────────────────

func TestNewVerifier_MapsErrors(t *testing.T) {
	owner := ulid.Make()
	verifier := new(MockVerifier)
	verifier.On("Verify", mock.Anything, "sk_live_ok").Return(owner, nil)
	verifier.On("Verify", mock.Anything, "sk_live_bad").Return(ulid.ULID{}, apikeysvc.ErrInvalidKey)
	dbErr := errors.New("db down")
	verifier.On("Verify", mock.Anything, "sk_live_db").Return(ulid.ULID{}, dbErr)

	v := NewVerifier(verifier)

	got, err := v.VerifyAPIKey(context.Background(), "sk_live_ok")
	require.NoError(t, err)
	assert.Equal(t, owner, got)

	_, err = v.VerifyAPIKey(context.Background(), "sk_live_bad")
	assert.ErrorIs(t, err, middleware.ErrInvalidToken)
	assert.ErrorIs(t, err, apikeysvc.ErrInvalidKey)

	_, err = v.VerifyAPIKey(context.Background(), "sk_live_db")
	assert.ErrorIs(t, err, dbErr)
	assert.NotErrorIs(t, err, middleware.ErrInvalidToken)
}

func TestServiceSatisfiesInterfaces(t *testing.T) {
	var _ Service = (*apikeysvc.Service)(nil)
	var _ KeyVerifier = (*apikeysvc.Service)(nil)
}
