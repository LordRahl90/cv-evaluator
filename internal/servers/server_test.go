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
