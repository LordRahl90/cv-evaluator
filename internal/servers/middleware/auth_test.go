package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSecret = []byte("test-secret-key")

// makeToken generates a signed HS256 JWT with the given subject and TTL.
func makeToken(t *testing.T, subject string, ttl time.Duration) string {
	t.Helper()
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testSecret)
	require.NoError(t, err)
	return tok
}

// newAuthRouter wires Authenticate onto a minimal gin engine with a single
// GET /ping handler that echoes the resolved user_id back to the caller.
func newAuthRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Authenticate(testSecret))
	r.GET("/ping", func(c *gin.Context) {
		id, _ := c.Get(UserIDKey)
		c.JSON(http.StatusOK, gin.H{"user_id": id})
	})
	return r
}

func performRequest(r *gin.Engine, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthenticate_ValidToken(t *testing.T) {
	userID := ulid.Make()
	token := makeToken(t, userID.String(), time.Hour)
	w := performRequest(newAuthRouter(), "Bearer "+token)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), userID.String())
}

func TestAuthenticate_MissingHeader(t *testing.T) {
	w := performRequest(newAuthRouter(), "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_MalformedHeader_NoBearer(t *testing.T) {
	token := makeToken(t, ulid.Make().String(), time.Hour)
	w := performRequest(newAuthRouter(), token) // missing "Bearer " prefix

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_MalformedHeader_EmptyToken(t *testing.T) {
	w := performRequest(newAuthRouter(), "Bearer ")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_ExpiredToken(t *testing.T) {
	token := makeToken(t, ulid.Make().String(), -time.Minute) // already expired
	w := performRequest(newAuthRouter(), "Bearer "+token)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_WrongSecret(t *testing.T) {
	wrongSecret := []byte("wrong-secret")
	claims := jwt.RegisteredClaims{
		Subject:   ulid.Make().String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(wrongSecret)
	require.NoError(t, err)

	w := performRequest(newAuthRouter(), "Bearer "+tok)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_WrongAlgorithm(t *testing.T) {
	// HS512 is valid JWT but the middleware must reject non-HS256 tokens.
	claims := jwt.RegisteredClaims{
		Subject:   ulid.Make().String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString(testSecret)
	require.NoError(t, err)

	w := performRequest(newAuthRouter(), "Bearer "+tok)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_ZeroULIDSubject(t *testing.T) {
	token := makeToken(t, ulid.ULID{}.String(), time.Hour)
	w := performRequest(newAuthRouter(), "Bearer "+token)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_NonULIDSubject(t *testing.T) {
	token := makeToken(t, "not-a-ulid", time.Hour)
	w := performRequest(newAuthRouter(), "Bearer "+token)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestExtractBearer(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		want    string
		wantErr bool
	}{
		{"valid", "Bearer abc123", "abc123", false},
		{"valid mixed case", "bearer abc123", "abc123", false},
		{"empty header", "", "", true},
		{"missing token", "Bearer ", "", true},
		{"no scheme", "abc123", "", true},
		{"wrong scheme", "Basic abc123", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractBearer(tc.header)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}
