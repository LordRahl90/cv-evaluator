package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLimitedRouter serves POST /limited and POST /free behind mw.
func newLimitedRouter(mw ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(mw...)
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.POST("/limited", ok)
	r.POST("/free", ok)
	return r
}

// post sends POST path from remoteIP.
func post(r *gin.Engine, path, remoteIP string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.RemoteAddr = remoteIP + ":1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGlobal_LimitsAllCallersTogether(t *testing.T) {
	rule := Rule{Method: http.MethodPost, Path: "/limited", Limiter: New(2, time.Hour), What: "things"}
	r := newLimitedRouter(Global(rule))

	assert.Equal(t, http.StatusOK, post(r, "/limited", "10.0.0.1").Code)
	assert.Equal(t, http.StatusOK, post(r, "/limited", "10.0.0.2").Code)

	w := post(r, "/limited", "10.0.0.3")
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "a new IP shares the global allowance")
	assert.Contains(t, w.Body.String(), "Too many things")
	assert.NotEmpty(t, w.Header().Get("Retry-After"))

	assert.Equal(t, http.StatusOK, post(r, "/free", "10.0.0.3").Code, "other routes are not limited")
}

func TestGlobal_CustomMessage(t *testing.T) {
	rule := Rule{Method: http.MethodPost, Path: "/limited", Limiter: New(1, 24*time.Hour),
		What: "things", Message: "Busy right now."}
	r := newLimitedRouter(Global(rule))

	require.Equal(t, http.StatusOK, post(r, "/limited", "10.0.0.1").Code)
	w := post(r, "/limited", "10.0.0.2")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Body.String(), `"message":"Busy right now."`)
	assert.Contains(t, w.Body.String(), `"code":"rate_limited"`)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestByClientIP_LimitsEachIPAndSparesTheGlobalAllowance(t *testing.T) {
	perIP := Rule{Method: http.MethodPost, Path: "/limited", Limiter: New(1, time.Hour), What: "things"}
	global := New(3, time.Hour)
	r := newLimitedRouter(
		ByClientIP(perIP),
		Global(Rule{Method: http.MethodPost, Path: "/limited", Limiter: global, Message: "Busy."}),
	)

	assert.Equal(t, http.StatusOK, post(r, "/limited", "10.0.0.1").Code)
	for range 5 {
		w := post(r, "/limited", "10.0.0.1")
		require.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), "Too many things", "the caller's own limit, not the global one")
	}

	// Requests refused per IP never reached the global limiter, so two
	// other callers still fit in the global allowance of three.
	assert.Equal(t, http.StatusOK, post(r, "/limited", "10.0.0.2").Code)
	assert.Equal(t, http.StatusOK, post(r, "/limited", "10.0.0.3").Code)
	w := post(r, "/limited", "10.0.0.4")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Body.String(), "Busy.")
}
