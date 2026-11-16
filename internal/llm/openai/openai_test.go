package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"cv-evaluator/internal/telemetry"
	"cv-evaluator/internal/telemetry/telemetrytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKey = "sk-test-secret"

// embeddingJSON returns an embeddings response with one vector of n floats.
func embeddingJSON(n int) string {
	values := make([]string, n)
	for i := range values {
		values[i] = "0.5"
	}
	return fmt.Sprintf(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[%s]}],"model":"text-embedding-3-small"}`,
		strings.Join(values, ","))
}

func newServer(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)
	return New(&http.Client{Timeout: 5 * time.Second}, testKey, api.URL+"/v1/")
}

func TestService_CreateEmbedding(t *testing.T) {
	var got embeddingRequest
	svc := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/embeddings", r.URL.Path)
		assert.Equal(t, "Bearer "+testKey, r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(embeddingJSON(EmbeddingDimensions)))
	})

	embedding, err := svc.CreateEmbedding(t.Context(), "Senior Go engineer")
	require.NoError(t, err)
	assert.Len(t, embedding, EmbeddingDimensions)
	assert.Equal(t, float32(0.5), embedding[0])
	assert.Equal(t, embeddingRequest{Model: "text-embedding-3-small", Input: "Senior Go engineer", Dimensions: 768}, got)
}

func TestService_CreateEmbeddingErrorStatus(t *testing.T) {
	svc := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided.","type":"invalid_request_error","code":"invalid_api_key"}}`))
	})

	embedding, err := svc.CreateEmbedding(t.Context(), "text")
	require.Error(t, err)
	assert.Nil(t, embedding)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "Incorrect API key provided.")
	assert.NotContains(t, err.Error(), testKey)
}

func TestService_CreateEmbeddingErrorStatusWithoutJSON(t *testing.T) {
	svc := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	})

	_, err := svc.CreateEmbedding(t.Context(), "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestService_CreateEmbeddingWrongDimensions(t *testing.T) {
	svc := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(embeddingJSON(1536)))
	})

	_, err := svc.CreateEmbedding(t.Context(), "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 768 dimensions, got 1536")
}

func TestService_CreateEmbeddingNoData(t *testing.T) {
	svc := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})

	_, err := svc.CreateEmbedding(t.Context(), "text")
	require.Error(t, err)
}

func TestService_CreateEmbeddingTruncatesInput(t *testing.T) {
	var got embeddingRequest
	svc := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(embeddingJSON(EmbeddingDimensions)))
	})

	long := strings.Repeat("æ", MaxInputChars+500)
	_, err := svc.CreateEmbedding(t.Context(), long)
	require.NoError(t, err)
	assert.Equal(t, MaxInputChars, utf8.RuneCountInString(got.Input))
	assert.True(t, utf8.ValidString(got.Input))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hello", truncate("hello", 5))
	assert.Equal(t, "hel", truncate("hello", 3))
	assert.Equal(t, "æøå", truncate("æøåæøå", 3))
	assert.Equal(t, "", truncate("", 3))
}

func TestService_RecordsExternalRequests(t *testing.T) {
	p := telemetrytest.Install(t)

	svc := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(embeddingJSON(EmbeddingDimensions)))
	})

	_, err := svc.CreateEmbedding(t.Context(), "text")
	require.NoError(t, err)

	points := p.Histogram(t, telemetry.MetricExternalRequestDuration)
	require.Len(t, points, 1)
	attrs := telemetrytest.Attrs(points[0].Attributes)
	assert.Equal(t, "openai", attrs["peer.service"])
	assert.Equal(t, "embedding", attrs["operation"])
	assert.Equal(t, "success", attrs["outcome"])

	spans := p.Spans()
	require.Len(t, spans, 1)
	assert.Equal(t, "openai embedding", spans[0].Name())
}
