package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/servers/middleware"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

var testJWTSecret = []byte("auth-handler-test-secret")

const testAPIKey = "sk_live_testkey"

// newTestRouter wires the auth handler with a verifier that accepts only
// testAPIKey, resolving it to apiKeyOwner.
func newTestRouter(svc Service, apiKeyOwner ulid.ULID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	verifier := middleware.APIKeyVerifierFunc(func(_ context.Context, raw string) (ulid.ULID, error) {
		if raw == testAPIKey {
			return apiKeyOwner, nil
		}
		return ulid.ULID{}, middleware.ErrInvalidToken
	})
	New(r, svc, testJWTSecret, verifier)
	return r
}

func makeToken(t *testing.T, userID ulid.ULID) string {
	t.Helper()
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testJWTSecret)
	require.NoError(t, err)
	return tok
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
