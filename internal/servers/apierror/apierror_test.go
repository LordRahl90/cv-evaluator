package apierror

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func render(t *testing.T, fn func(c *gin.Context)) (*httptest.ResponseRecorder, map[string]map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	fn(c)

	var body map[string]map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w, body
}

func TestBadRequest(t *testing.T) {
	w, body := render(t, func(c *gin.Context) {
		BadRequest(c, CodeParameterMissing, "The email parameter is required.", "email")
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, map[string]any{
		"type":    TypeInvalidRequest,
		"code":    CodeParameterMissing,
		"message": "The email parameter is required.",
		"param":   "email",
	}, body["error"])
}

func TestUnauthorized_AbortsAndSetsHeader(t *testing.T) {
	var aborted bool
	w, body := render(t, func(c *gin.Context) {
		Unauthorized(c, CodeInvalidToken, "bad token")
		aborted = c.IsAborted()
	})

	assert.True(t, aborted)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), "Bearer")
	assert.Equal(t, TypeAuthentication, body["error"]["type"])
	assert.Equal(t, CodeInvalidToken, body["error"]["code"])
	_, hasParam := body["error"]["param"]
	assert.False(t, hasParam, "empty param is omitted")
}

func TestStatusHelpers(t *testing.T) {
	cases := []struct {
		name   string
		fn     func(c *gin.Context)
		status int
		typ    string
		code   string
	}{
		{"not found", func(c *gin.Context) { NotFound(c, "missing", "id") }, http.StatusNotFound, TypeInvalidRequest, CodeResourceMissing},
		{"conflict", func(c *gin.Context) { Conflict(c, CodeEmailAlreadyUsed, "taken", "email") }, http.StatusConflict, TypeInvalidRequest, CodeEmailAlreadyUsed},
		{"internal", InternalServerError, http.StatusInternalServerError, TypeAPI, CodeInternal},
		{"timeout", GatewayTimeout, http.StatusGatewayTimeout, TypeAPI, CodeTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, body := render(t, tc.fn)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.typ, body["error"]["type"])
			assert.Equal(t, tc.code, body["error"]["code"])
			assert.NotEmpty(t, body["error"]["message"])
		})
	}
}

func TestError_ImplementsError(t *testing.T) {
	var err error = &Error{Message: "boom"}
	assert.EqualError(t, err, "boom")
}
