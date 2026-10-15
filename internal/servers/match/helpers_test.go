package match

import (
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
)

var testJWTSecret = []byte("match-handler-test-secret")

// newTestRouter builds a router with the match handler and a pre-handler that
// attaches a valid Bearer token for userID, so requests pass the auth middleware.
func newTestRouter(svc JobMatcher, userID ulid.ULID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if userID != (ulid.ULID{}) {
			c.Request.Header.Set("Authorization", "Bearer "+makeToken(userID))
		}
		c.Next()
	})
	New(router, svc, testJWTSecret)
	return router
}

func noAuthRouter(svc JobMatcher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	New(router, svc, testJWTSecret)
	return router
}

// makeToken creates a signed HS256 JWT whose subject is userID.
func makeToken(userID ulid.ULID) string {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testJWTSecret)
	if err != nil {
		panic(err)
	}
	return tok
}

func performRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	return performRequestWithAuth(router, method, path, body, "")
}

func performRequestWithAuth(router *gin.Engine, method, path, body, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
