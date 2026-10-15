package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"cv-evaluator/internal/services/users"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func sampleAuthResponse(id ulid.ULID) *users.AuthResponse {
	return &users.AuthResponse{
		Token: "jwt-token",
		User: users.Profile{
			ID:        id,
			FirstName: "Ada",
			LastName:  "Lovelace",
			Email:     "ada@example.com",
			Phone:     "+4512345678",
		},
	}
}

// ── POST /auth/signup ───────────────────────────────────────────────────────

func TestSignup_Success(t *testing.T) {
	svc := new(MockAuthService)
	id := ulid.Make()
	svc.On("Signup", mock.Anything, users.SignupRequest{
		FirstName: "Ada",
		LastName:  "Lovelace",
		Email:     "ada@example.com",
		Phone:     "+4512345678",
		Password:  "password123",
	}).Return(sampleAuthResponse(id), nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/auth/signup", "",
		`{"first_name":"Ada","last_name":"Lovelace","email":" ada@example.com ","phone":"+4512345678","password":"password123"}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "session", resp.Object)
	assert.Equal(t, "jwt-token", resp.Token)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, "user", resp.User.Object)
	assert.Equal(t, id.String(), resp.User.ID)
	assert.Equal(t, "ada@example.com", resp.User.Email)
	assert.NotContains(t, w.Body.String(), "password")
	svc.AssertExpectations(t)
}

func TestSignup_Validation(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		code  string
		param string
	}{
		{"malformed json", `{"email":`, "invalid_json", ""},
		{"empty body", ``, "invalid_json", ""},
		{"missing email", `{"password":"password123"}`, "parameter_missing", "email"},
		{"invalid email", `{"email":"not-an-email","password":"password123"}`, "parameter_invalid", "email"},
		{"email with display name", `{"email":"Ada <ada@example.com>","password":"password123"}`, "parameter_invalid", "email"},
		{"missing password", `{"email":"ada@example.com"}`, "parameter_missing", "password"},
		{"short password", `{"email":"ada@example.com","password":"short"}`, "parameter_invalid", "password"},
		{"long password", `{"email":"ada@example.com","password":"` + strings.Repeat("a", 73) + `"}`, "parameter_invalid", "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(MockAuthService)
			w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/auth/signup", "", tc.body)

			require.Equal(t, http.StatusBadRequest, w.Code)
			body := decodeError(t, w)
			assert.Equal(t, "invalid_request_error", body.Error.Type)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
			assert.NotEmpty(t, body.Error.Message)
			svc.AssertNotCalled(t, "Signup")
		})
	}
}

func TestSignup_ServiceErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		typ    string
		code   string
		param  string
	}{
		{"email used", users.ErrEmailAlreadyUsed, http.StatusConflict, "invalid_request_error", "email_already_used", "email"},
		{"phone used", users.ErrPhoneAlreadyUsed, http.StatusConflict, "invalid_request_error", "phone_already_used", "phone"},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, "api_error", "timeout", ""},
		{"unexpected", errors.New("db exploded"), http.StatusInternalServerError, "api_error", "internal_error", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(MockAuthService)
			svc.On("Signup", mock.Anything, mock.Anything).Return(nil, tc.err).Once()

			w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/auth/signup", "",
				`{"email":"ada@example.com","password":"password123"}`)

			require.Equal(t, tc.status, w.Code)
			body := decodeError(t, w)
			assert.Equal(t, tc.typ, body.Error.Type)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
			assert.NotContains(t, w.Body.String(), "db exploded")
			svc.AssertExpectations(t)
		})
	}
}

// ── POST /auth/signin ───────────────────────────────────────────────────────

func TestSignin_Success(t *testing.T) {
	svc := new(MockAuthService)
	id := ulid.Make()
	svc.On("Signin", mock.Anything, users.SigninRequest{Email: "ada@example.com", Password: "password123"}).
		Return(sampleAuthResponse(id), nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/auth/signin", "",
		`{"email":"ada@example.com","password":"password123"}`)

	require.Equal(t, http.StatusOK, w.Code)
	var resp SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "jwt-token", resp.Token)
	assert.Equal(t, id.String(), resp.User.ID)
	svc.AssertExpectations(t)
}

func TestSignin_InvalidCredentials(t *testing.T) {
	svc := new(MockAuthService)
	svc.On("Signin", mock.Anything, mock.Anything).Return(nil, users.ErrInvalidCredentials).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/auth/signin", "",
		`{"email":"ada@example.com","password":"wrong-password"}`)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	body := decodeError(t, w)
	assert.Equal(t, "authentication_error", body.Error.Type)
	assert.Equal(t, "invalid_credentials", body.Error.Code)
	svc.AssertExpectations(t)
}

func TestSignin_Validation(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		code  string
		param string
	}{
		{"malformed json", `nope`, "invalid_json", ""},
		{"missing email", `{"password":"x"}`, "parameter_missing", "email"},
		{"missing password", `{"email":"ada@example.com"}`, "parameter_missing", "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(MockAuthService)
			w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/auth/signin", "", tc.body)

			require.Equal(t, http.StatusBadRequest, w.Code)
			body := decodeError(t, w)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
			svc.AssertNotCalled(t, "Signin")
		})
	}
}

// ── GET /me ─────────────────────────────────────────────────────────────────

func TestMe_WithJWT(t *testing.T) {
	svc := new(MockAuthService)
	id := ulid.Make()
	profile := sampleAuthResponse(id).User
	svc.On("GetProfile", mock.Anything, id).Return(&profile, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/me", "Bearer "+makeToken(t, id), "")

	require.Equal(t, http.StatusOK, w.Code)
	var resp UserResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "user", resp.Object)
	assert.Equal(t, id.String(), resp.ID)
	assert.Equal(t, "Ada", resp.FirstName)
	svc.AssertExpectations(t)
}

func TestMe_WithAPIKey(t *testing.T) {
	svc := new(MockAuthService)
	owner := ulid.Make()
	profile := sampleAuthResponse(owner).User
	svc.On("GetProfile", mock.Anything, owner).Return(&profile, nil).Once()

	w := doRequest(newTestRouter(svc, owner), http.MethodGet, "/me", "Bearer "+testAPIKey, "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), owner.String())
	svc.AssertExpectations(t)
}

func TestMe_Unauthenticated(t *testing.T) {
	cases := map[string]string{
		"no header":   "",
		"bad jwt":     "Bearer garbage",
		"bad api key": "Bearer sk_live_unknown",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			svc := new(MockAuthService)
			w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/me", header, "")

			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, "authentication_error", decodeError(t, w).Error.Type)
			svc.AssertNotCalled(t, "GetProfile")
		})
	}
}

func TestMe_UserDeleted(t *testing.T) {
	svc := new(MockAuthService)
	id := ulid.Make()
	svc.On("GetProfile", mock.Anything, id).Return(nil, users.ErrUserNotFound).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/me", "Bearer "+makeToken(t, id), "")

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid_token", decodeError(t, w).Error.Code)
}

func TestMe_ServiceError(t *testing.T) {
	svc := new(MockAuthService)
	id := ulid.Make()
	svc.On("GetProfile", mock.Anything, id).Return(nil, errors.New("boom")).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/me", "Bearer "+makeToken(t, id), "")

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "api_error", decodeError(t, w).Error.Type)
}

func TestUsersServiceSatisfiesInterface(t *testing.T) {
	var _ Service = (*users.Service)(nil)
}
