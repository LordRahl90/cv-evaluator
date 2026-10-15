package servers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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
	s := New(&Config{HealthCheck: func(context.Context) error { return nil }})

	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
}

func TestHealthz_Unavailable(t *testing.T) {
	s := New(&Config{HealthCheck: func(context.Context) error { return errors.New("db down") }})

	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.JSONEq(t, `{"status":"unavailable"}`, w.Body.String())
}

func TestServe_ShutsDownWhenContextCancelled(t *testing.T) {
	s := New(&Config{ShutdownTimeout: time.Second})

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

	s := New(&Config{})
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
