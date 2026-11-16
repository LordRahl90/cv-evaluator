package servers

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHealthz_OK(t *testing.T) {
	s := mustNew(t, &Config{HealthCheck: func(context.Context) error { return nil }})

	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
}

func TestHealthz_Unavailable(t *testing.T) {
	s := mustNew(t, &Config{HealthCheck: func(context.Context) error { return errors.New("db down") }})

	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.JSONEq(t, `{"status":"unavailable"}`, w.Body.String())
}

func TestServe_ShutsDownWhenContextCancelled(t *testing.T) {
	s := mustNew(t, &Config{ShutdownTimeout: time.Second})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.serve(ctx, &http.Server{Handler: s.Router(), ReadHeaderTimeout: time.Second}, ln)
	}()

	require.Eventually(t, func() bool {
		res, err := http.Get("http://" + ln.Addr().String() + "/healthz")
		if err != nil {
			return false
		}
		_ = res.Body.Close()
		return res.StatusCode == http.StatusOK
	}, 2*time.Second, 20*time.Millisecond)

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestNew_TracesRequestsButNotHealthz(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))

	s := mustNew(t, &Config{})
	s.Router().GET("/things/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	s.Router().GET("/panic", func(*gin.Context) { panic("boom") })

	for _, path := range []string{"/healthz", "/things/42", "/panic"} {
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	}

	ended := spans.Ended()
	require.Len(t, ended, 2, "/healthz must not be traced")
	assert.Equal(t, "GET /things/:id", ended[0].Name())
	assert.Equal(t, "GET /panic", ended[1].Name())

	var status int64
	for _, kv := range ended[1].Attributes() {
		if kv.Key == "http.response.status_code" {
			status = kv.Value.AsInt64()
		}
	}
	assert.Equal(t, int64(http.StatusInternalServerError), status, "recovered panic must be recorded as 500")
}

func mustNew(t *testing.T, config *Config) *Server {
	t.Helper()
	s, err := New(config)
	require.NoError(t, err)
	return s
}

func TestNew_RejectsBodiesOverTheLimit(t *testing.T) {
	s := mustNew(t, &Config{MaxBodyBytes: 16})
	s.Router().POST("/echo", func(c *gin.Context) {
		var body map[string]string
		if err := c.ShouldBindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
	})

	t.Run("declared length", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":"0123456789abcdef"}`)))
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
		assert.Contains(t, w.Body.String(), "body_too_large")
	})

	t.Run("undeclared length", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/echo", io.NopCloser(strings.NewReader(`{"a":"0123456789abcdef"}`)))
		req.ContentLength = -1
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("within the limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":"b"}`)))
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestNew_SetsSecurityHeaders(t *testing.T) {
	s := mustNew(t, &Config{})

	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
}

func TestNew_IgnoresForwardedForFromUntrustedPeers(t *testing.T) {
	s := mustNew(t, &Config{})
	var got string
	s.Router().GET("/ip", func(c *gin.Context) { got = c.ClientIP() })

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "203.0.113.7:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	s.Router().ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "203.0.113.7", got)
}
