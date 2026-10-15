package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validTestKey = "sk_live_valid"

// newRouterWithOpts echoes the resolved user ID and auth method.
func newRouterWithOpts(opts ...Option) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Authenticate(testSecret, opts...))
	r.GET("/ping", func(c *gin.Context) {
		id, ok := UserID(c)
		method, _ := c.Get(AuthMethodKey)
		c.JSON(http.StatusOK, gin.H{"user_id": id.String(), "ok": ok, "method": method})
	})
	return r
}

func stubVerifier(owner ulid.ULID, calls *int) APIKeyVerifier {
	return APIKeyVerifierFunc(func(_ context.Context, rawKey string) (ulid.ULID, error) {
		*calls++
		switch rawKey {
		case validTestKey:
			return owner, nil
		case "sk_live_dberror":
			return ulid.ULID{}, errors.New("connection refused")
		case "sk_live_zero":
			return ulid.ULID{}, nil
		default:
			return ulid.ULID{}, fmt.Errorf("lookup: %w", ErrInvalidToken)
		}
	})
}

type errorBody struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestAuthenticate_APIKey_Valid(t *testing.T) {
	owner := ulid.Make()
	calls := 0
	w := performRequest(newRouterWithOpts(WithAPIKeyVerifier(stubVerifier(owner, &calls))), "Bearer "+validTestKey)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, owner.String(), body["user_id"])
	assert.Equal(t, AuthMethodAPIKey, body["method"])
	assert.Equal(t, 1, calls)
}

func TestAuthenticate_APIKey_Invalid(t *testing.T) {
	calls := 0
	w := performRequest(newRouterWithOpts(WithAPIKeyVerifier(stubVerifier(ulid.Make(), &calls))), "Bearer sk_live_unknown")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	body := decodeError(t, w)
	assert.Equal(t, "authentication_error", body.Error.Type)
	assert.Equal(t, "invalid_api_key", body.Error.Code)
	assert.NotEmpty(t, body.Error.Message)
}

func TestAuthenticate_APIKey_ZeroUserIsRejected(t *testing.T) {
	calls := 0
	w := performRequest(newRouterWithOpts(WithAPIKeyVerifier(stubVerifier(ulid.Make(), &calls))), "Bearer sk_live_zero")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_APIKey_VerifierFailureIs500(t *testing.T) {
	calls := 0
	w := performRequest(newRouterWithOpts(WithAPIKeyVerifier(stubVerifier(ulid.Make(), &calls))), "Bearer sk_live_dberror")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := decodeError(t, w)
	assert.Equal(t, "api_error", body.Error.Type)
	assert.NotContains(t, w.Body.String(), "connection refused", "internal details must not leak")
}

func TestAuthenticate_APIKey_WithoutVerifier(t *testing.T) {
	// Default construction (existing callers) rejects API keys.
	w := performRequest(newAuthRouter(), "Bearer "+validTestKey)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid_api_key", decodeError(t, w).Error.Code)
}

func TestAuthenticate_NilVerifierOption(t *testing.T) {
	w := performRequest(newRouterWithOpts(WithAPIKeyVerifier(nil)), "Bearer "+validTestKey)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_JWTStillWorksWithVerifier(t *testing.T) {
	userID := ulid.Make()
	calls := 0
	r := newRouterWithOpts(WithAPIKeyVerifier(stubVerifier(ulid.Make(), &calls)))

	w := performRequest(r, "Bearer "+makeToken(t, userID.String(), time.Hour))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), userID.String())
	assert.Contains(t, w.Body.String(), AuthMethodJWT)
	assert.Equal(t, 0, calls, "JWTs must not reach the API key verifier")
}

func TestAuthenticate_ErrorBodies(t *testing.T) {
	cases := []struct {
		name   string
		header string
		code   string
	}{
		{"missing header", "", "missing_credentials"},
		{"wrong scheme", "Basic abc", "missing_credentials"},
		{"bad jwt", "Bearer not-a-jwt", "invalid_token"},
		{"expired jwt", "Bearer " + makeToken(t, ulid.Make().String(), -time.Minute), "invalid_token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := performRequest(newAuthRouter(), tc.header)
			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Header().Get("WWW-Authenticate"), "Bearer")
			body := decodeError(t, w)
			assert.Equal(t, "authentication_error", body.Error.Type)
			assert.Equal(t, tc.code, body.Error.Code)
		})
	}
}

func TestUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := ulid.Make()
	cases := []struct {
		name  string
		value any
		set   bool
		want  ulid.ULID
		found bool
	}{
		{"unset", nil, false, ulid.ULID{}, false},
		{"ulid", id, true, id, true},
		{"zero ulid", ulid.ULID{}, true, ulid.ULID{}, false},
		{"string", id.String(), true, id, true},
		{"bad string", "nope", true, ulid.ULID{}, false},
		{"other type", 42, true, ulid.ULID{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tc.set {
				c.Set(UserIDKey, tc.value)
			}
			got, ok := UserID(c)
			assert.Equal(t, tc.found, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
