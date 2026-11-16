package servers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newWebServer serves a small built app next to an API route that clashes
// with an app page (/api-keys), as the real API does.
func newWebServer(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>app</title>"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "favicon.svg"), []byte("<svg/>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("outside"), 0o600))

	s := mustNew(t, &Config{WebDir: dir})
	s.Router().GET("/api-keys", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"api": true}) })
	h, err := s.Handler()
	require.NoError(t, err)
	return h
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func TestWeb_APIIsUnderPrefixAndPagesGetTheApp(t *testing.T) {
	h := newWebServer(t)

	w := get(h, http.MethodGet, "/api/api-keys")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"api":true}`, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "API responses keep the API's headers")

	for _, page := range []string{"/", "/api-keys", "/matches/01JA2B3C4D5E6F7G8H9J0KR001", "/index.html"} {
		w = get(h, http.MethodGet, page)
		assert.Equal(t, http.StatusOK, w.Code, page)
		assert.Contains(t, w.Body.String(), "<title>app</title>", page)
		assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"), page)
		assert.Contains(t, w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'", page)
	}

	w = get(h, http.MethodGet, "/healthz")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
}

func TestWeb_ServesAssets(t *testing.T) {
	h := newWebServer(t)

	w := get(h, http.MethodGet, "/assets/app-abc123.js")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "console.log(1)", w.Body.String())
	assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")

	w = get(h, http.MethodGet, "/favicon.svg")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))

	w = get(h, http.MethodGet, "/assets/app-old999.js")
	assert.Equal(t, http.StatusNotFound, w.Code, "a missing asset must not get the app's HTML")
}

func TestWeb_StaysInsideTheDirectory(t *testing.T) {
	h := newWebServer(t)
	for _, target := range []string{"/../secret.txt", "/assets/../../secret.txt", "/%2e%2e/secret.txt"} {
		w := get(h, http.MethodGet, target)
		assert.NotContains(t, w.Body.String(), "outside", target)
	}
}

func TestWeb_OnlyGetAndHead(t *testing.T) {
	h := newWebServer(t)
	assert.Equal(t, http.StatusMethodNotAllowed, get(h, http.MethodPost, "/matches").Code)
	w := get(h, http.MethodHead, "/matches")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestWeb_WithoutWebDirTheAPIStaysAtTheRoot(t *testing.T) {
	s := mustNew(t, &Config{})
	s.Router().GET("/api-keys", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"api": true}) })
	h, err := s.Handler()
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, get(h, http.MethodGet, "/api-keys").Code)
}
