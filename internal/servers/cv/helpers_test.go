package cv

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

var testJWTSecret = []byte("cv-handler-test-secret")

// newTestRouter builds a gin engine with the CV handler and its auth middleware
// using the shared testJWTSecret.
func newTestRouter(svc Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	New(r, svc, testJWTSecret)
	return r
}

// makeToken creates a signed HS256 JWT whose subject is the given userID,
// valid for the specified duration.
func makeToken(t *testing.T, userID ulid.ULID, ttl time.Duration) string {
	t.Helper()
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testJWTSecret)
	require.NoError(t, err)
	return tok
}

// authHeader returns a Bearer token header value for the given user.
func authHeader(t *testing.T, userID ulid.ULID) string {
	return "Bearer " + makeToken(t, userID, time.Hour)
}

// doRequest performs a plain JSON or body-less request.
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

// doMultipartUpload crafts a multipart/form-data request with the cv field set to fileContent.
func doMultipartUpload(router *gin.Engine, auth, fileContent string) *httptest.ResponseRecorder {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("cv", "resume.txt")
	if err != nil {
		panic(fmt.Sprintf("create form file: %v", err))
	}
	_, _ = io.WriteString(part, fileContent)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/cv/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
