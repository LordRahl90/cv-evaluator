package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const baseURL = "http://localhost:1143"

func TestMain(m *testing.M) {
	code := 1
	defer func() {
		os.Exit(code)
	}()

	code = m.Run()
}

func TestService_LoadPrompt(t *testing.T) {
	name := "cv-cleanup.txt"
	svc := &Service{}
	b, err := svc.loadPrompt(name)
	require.NoError(t, err)
	require.NotEmpty(t, b)
}

func TestCallLLM(t *testing.T) {
	svc := New(&http.Client{
		Timeout: 30 * time.Second,
		Transport: &mockClient{
			roundTripper: func(req *http.Request) (*http.Response, error) {
				// Mock response for LLM call
				mockResponse := `{
					"response": "The sky is blue because of the way sunlight interacts with Earth's atmosphere."
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(mockResponse)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}, baseURL)

	req := Request{
		Model:  "llama3.2",
		Prompt: "Why is the sky blue?",
		Stream: false,
	}

	res, err := svc.callLLM(t.Context(), "/api/generate", req)
	require.NoError(t, err)
	require.NotEmpty(t, res)
}

func TestService_CreateEmbedding(t *testing.T) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &mockClient{
			roundTripper: func(req *http.Request) (*http.Response, error) {
				// Mock response for embedding creation
				mockResponse := `{
					"embedding": [0.1, 0.2, 0.3, 0.4]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(mockResponse)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	svc := New(client, baseURL)

	content := "This is a test embedding."
	embedding, err := svc.CreateEmbedding(t.Context(), content)
	require.NoError(t, err)
	require.NotEmpty(t, embedding)
}

func TestService_CreateEmbeddingErrorStatus(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"model \"nomic-embed-text\" not found, try pulling it first"}`))
	}))
	defer api.Close()

	svc := New(&http.Client{Timeout: 5 * time.Second}, api.URL)

	embedding, err := svc.CreateEmbedding(t.Context(), "text")
	require.Error(t, err)
	assert.Nil(t, embedding)
	assert.Contains(t, err.Error(), "404")
	assert.Contains(t, err.Error(), "not found, try pulling it first")
}

func TestService_CreateEmbeddingErrorStatusWithoutJSON(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer api.Close()

	svc := New(&http.Client{Timeout: 5 * time.Second}, api.URL)

	_, err := svc.CreateEmbedding(t.Context(), "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestService_CreateEmbeddingEmptyResponse(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	svc := New(&http.Client{Timeout: 5 * time.Second}, api.URL)

	_, err := svc.CreateEmbedding(t.Context(), "text")
	require.Error(t, err)
}

func TestService_CleanupCVErrorStatus(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"out of memory"}`))
	}))
	defer api.Close()

	svc := New(&http.Client{Timeout: 5 * time.Second}, api.URL)

	_, err := svc.CleanupCV(t.Context(), "cv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of memory")
}

type mockClient struct {
	roundTripper func(req *http.Request) (*http.Response, error)
}

func (m *mockClient) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripper(req)
}
