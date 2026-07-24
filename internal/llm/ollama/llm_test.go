package ollama

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

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

type mockClient struct {
	roundTripper func(req *http.Request) (*http.Response, error)
}

func (m *mockClient) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripper(req)
}
