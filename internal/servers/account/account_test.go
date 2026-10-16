package account

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

	"cv-evaluator/internal/servers/auth"
	"cv-evaluator/internal/servers/middleware"
	accountsvc "cv-evaluator/internal/services/account"
	"cv-evaluator/internal/services/users"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var testJWTSecret = []byte("account-handler-test-secret")

const testAPIKey = "sk_test_accountkey"

type fixture struct {
	router   *gin.Engine
	profiles *MockProfiles
	data     *MockData
	userID   ulid.ULID
	token    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &fixture{profiles: new(MockProfiles), data: new(MockData), userID: ulid.Make()}
	verifier := middleware.APIKeyVerifierFunc(func(_ context.Context, raw string) (ulid.ULID, error) {
		if raw == testAPIKey {
			return f.userID, nil
		}
		return ulid.ULID{}, middleware.ErrInvalidToken
	})
	f.router = gin.New()
	New(f.router, f.profiles, f.data, testJWTSecret, middleware.WithAPIKeyVerifier(verifier))

	now := time.Now()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   f.userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(testJWTSecret)
	require.NoError(t, err)
	f.token = tok
	t.Cleanup(func() {
		f.profiles.AssertExpectations(t)
		f.data.AssertExpectations(t)
	})
	return f
}

func (f *fixture) do(method, path, body string) *httptest.ResponseRecorder {
	return f.doAs("Bearer "+f.token, method, path, body)
}

func (f *fixture) doAs(authz, method, path, body string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
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

func ptr(s string) *string { return &s }

func profile(id ulid.ULID) *users.Profile {
	return &users.Profile{ID: id, FirstName: "Ada", LastName: "King", Email: "ada@example.com", Phone: "+45"}
}

func TestRoutes_RequireAuthentication(t *testing.T) {
	f := newFixture(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodPatch, "/me"},
		{http.MethodPost, "/me/password"},
		{http.MethodDelete, "/me"},
		{http.MethodGet, "/me/export"},
	} {
		w := f.doAs("", route.method, route.path, `{}`)
		assert.Equal(t, http.StatusUnauthorized, w.Code, route.path)
		assert.Equal(t, "missing_credentials", decodeError(t, w).Error.Code)
	}
}

// ── PATCH /me ───────────────────────────────────────────────────────────────

func TestUpdate_Success(t *testing.T) {
	f := newFixture(t)
	f.profiles.On("UpdateProfile", mock.Anything, f.userID, users.UpdateProfileRequest{
		LastName: ptr("King"),
		Email:    ptr("Ada@Example.com"),
	}).Return(profile(f.userID), nil).Once()

	w := f.do(http.MethodPatch, "/me", `{"last_name":"King","email":"  Ada@Example.com "}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp auth.UserResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "user", resp.Object)
	assert.Equal(t, f.userID.String(), resp.ID)
	assert.Equal(t, "King", resp.LastName)
	assert.Equal(t, "ada@example.com", resp.Email)
}

func TestUpdate_WithAPIKey(t *testing.T) {
	f := newFixture(t)
	f.profiles.On("UpdateProfile", mock.Anything, f.userID, users.UpdateProfileRequest{FirstName: ptr("Ada")}).
		Return(profile(f.userID), nil).Once()

	w := f.doAs("Bearer "+testAPIKey, http.MethodPatch, "/me", `{"first_name":"Ada"}`)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestUpdate_Validation(t *testing.T) {
	cases := []struct {
		name, body, code, param string
	}{
		{"malformed json", `{"email":`, "invalid_json", ""},
		{"empty body", ``, "invalid_json", ""},
		{"wrong type", `{"first_name":42}`, "invalid_json", ""},
		{"empty email", `{"email":"  "}`, "parameter_invalid", "email"},
		{"invalid email", `{"email":"not-an-email"}`, "parameter_invalid", "email"},
		{"email with display name", `{"email":"Ada <ada@example.com>"}`, "parameter_invalid", "email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			w := f.do(http.MethodPatch, "/me", tc.body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			body := decodeError(t, w)
			assert.Equal(t, "invalid_request_error", body.Error.Type)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
		})
	}
}

func TestUpdate_ServiceErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		param  string
	}{
		{"email taken", users.ErrEmailAlreadyUsed, http.StatusConflict, "email_already_used", "email"},
		{"phone taken", users.ErrPhoneAlreadyUsed, http.StatusConflict, "phone_already_used", "phone"},
		{"user gone", users.ErrUserNotFound, http.StatusNotFound, "resource_missing", ""},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, "timeout", ""},
		{"unexpected", errors.New("db down"), http.StatusInternalServerError, "internal_error", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.profiles.On("UpdateProfile", mock.Anything, f.userID, mock.Anything).Return(nil, tc.err).Once()
			w := f.do(http.MethodPatch, "/me", `{"email":"x@example.com","phone":"1"}`)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			body := decodeError(t, w)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
			assert.NotContains(t, w.Body.String(), "db down")
		})
	}
}

// ── POST /me/password ───────────────────────────────────────────────────────

func TestChangePassword_Success(t *testing.T) {
	f := newFixture(t)
	f.profiles.On("ChangePassword", mock.Anything, f.userID, "old-password", "new-password").
		Return(&users.AuthResponse{Token: "fresh-token", User: *profile(f.userID)}, nil).Once()

	w := f.do(http.MethodPost, "/me/password", `{"current_password":"old-password","new_password":"new-password"}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp auth.SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "session", resp.Object)
	assert.Equal(t, "fresh-token", resp.Token)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, f.userID.String(), resp.User.ID)
	assert.NotContains(t, w.Body.String(), "password")
}

func TestChangePassword_Validation(t *testing.T) {
	cases := []struct {
		name, body, code, param string
	}{
		{"malformed json", `{`, "invalid_json", ""},
		{"missing current", `{"new_password":"new-password"}`, "parameter_missing", "current_password"},
		{"missing new", `{"current_password":"old-password"}`, "parameter_missing", "new_password"},
		{"short new", `{"current_password":"old-password","new_password":"short"}`, "parameter_invalid", "new_password"},
		{"long new", `{"current_password":"old-password","new_password":"` + strings.Repeat("a", 73) + `"}`, "parameter_invalid", "new_password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			w := f.do(http.MethodPost, "/me/password", tc.body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			body := decodeError(t, w)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
		})
	}
}

func TestChangePassword_WrongCurrentPassword(t *testing.T) {
	f := newFixture(t)
	f.profiles.On("ChangePassword", mock.Anything, f.userID, "wrong-password", "new-password").
		Return(nil, users.ErrIncorrectPassword).Once()

	w := f.do(http.MethodPost, "/me/password", `{"current_password":"wrong-password","new_password":"new-password"}`)

	require.Equal(t, http.StatusBadRequest, w.Code, "not 401: the session itself is fine")
	body := decodeError(t, w)
	assert.Equal(t, CodeIncorrectPassword, body.Error.Code)
	assert.Equal(t, "current_password", body.Error.Param)
	assert.Empty(t, w.Header().Get("WWW-Authenticate"))
}

// ── DELETE /me ──────────────────────────────────────────────────────────────

func TestDelete_Success(t *testing.T) {
	f := newFixture(t)
	f.profiles.On("VerifyPassword", mock.Anything, f.userID, "password123").Return(nil).Once()
	f.data.On("Delete", mock.Anything, f.userID).
		Return([]accountsvc.Deleted{{Table: "cvs", Rows: 2}, {Table: "users", Rows: 1}}, nil).Once()

	w := f.do(http.MethodDelete, "/me", `{"password":"password123"}`)

	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Empty(t, w.Body.String())
}

func TestDelete_APIKeyCallerMustSendPassword(t *testing.T) {
	f := newFixture(t)
	w := f.doAs("Bearer "+testAPIKey, http.MethodDelete, "/me", `{}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	body := decodeError(t, w)
	assert.Equal(t, "parameter_missing", body.Error.Code)
	assert.Equal(t, "password", body.Error.Param)

	f.profiles.On("VerifyPassword", mock.Anything, f.userID, "password123").Return(nil).Once()
	f.data.On("Delete", mock.Anything, f.userID).Return([]accountsvc.Deleted{}, nil).Once()
	w = f.doAs("Bearer "+testAPIKey, http.MethodDelete, "/me", `{"password":"password123"}`)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDelete_Errors(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		verifyErr error
		deleteErr error
		status    int
		code      string
		param     string
	}{
		{"no body", ``, nil, nil, http.StatusBadRequest, "invalid_json", ""},
		{"wrong password", `{"password":"nope"}`, users.ErrIncorrectPassword, nil, http.StatusBadRequest, CodeIncorrectPassword, "password"},
		{"user gone before verify", `{"password":"x"}`, users.ErrUserNotFound, nil, http.StatusNotFound, "resource_missing", ""},
		{"user gone before delete", `{"password":"x"}`, nil, accountsvc.ErrUserNotFound, http.StatusNotFound, "resource_missing", ""},
		{"delete fails", `{"password":"x"}`, nil, errors.New("deadlock"), http.StatusInternalServerError, "internal_error", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.body != "" {
				f.profiles.On("VerifyPassword", mock.Anything, f.userID, mock.Anything).Return(tc.verifyErr).Once()
				if tc.verifyErr == nil {
					f.data.On("Delete", mock.Anything, f.userID).Return(nil, tc.deleteErr).Once()
				}
			}
			w := f.do(http.MethodDelete, "/me", tc.body)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			body := decodeError(t, w)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
		})
	}
}

// ── GET /me/export ──────────────────────────────────────────────────────────

func TestExport_Success(t *testing.T) {
	f := newFixture(t)
	exp := &accountsvc.Export{
		Object:  "account_export",
		Profile: accountsvc.ExportProfile{ID: f.userID, Email: "ada@example.com"},
		CVs:     []accountsvc.ExportCV{},
	}
	f.data.On("Export", mock.Anything, f.userID).Return(exp, nil).Once()

	w := f.do(http.MethodGet, "/me/export", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, `attachment; filename="cv-evaluator-export.json"`, w.Header().Get("Content-Disposition"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	var got accountsvc.Export
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "account_export", got.Object)
	assert.Equal(t, f.userID, got.Profile.ID)
}

func TestExport_Errors(t *testing.T) {
	f := newFixture(t)
	f.data.On("Export", mock.Anything, f.userID).Return(nil, accountsvc.ErrUserNotFound).Once()
	w := f.do(http.MethodGet, "/me/export", "")
	require.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, w.Header().Get("Content-Disposition"), "errors are not offered as a download")

	f.data.On("Export", mock.Anything, f.userID).Return(nil, errors.New("boom")).Once()
	w = f.do(http.MethodGet, "/me/export", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
