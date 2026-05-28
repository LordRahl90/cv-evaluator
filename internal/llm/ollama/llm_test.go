package ollama

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
	}, "http://localhost:11434")

	req := Request{
		Model:  "llama3.2",
		Prompt: "Why is the sky blue?",
		Stream: false,
	}

	res, err := svc.callLLM(t.Context(), "/api/generate", req)
	require.NoError(t, err)
	require.NotEmpty(t, res)

	fmt.Printf("\n\nLLM Response: %s\n\n", res)
}

func TestService_CreateEmbedding(t *testing.T) {
	svc := New(&http.Client{
		Timeout: 10 * time.Second,
	}, "http://localhost:11434")

	content := "This is a test embedding."
	embedding, err := svc.CreateEmbedding(t.Context(), content)
	require.NoError(t, err)
	require.NotEmpty(t, embedding)

	fmt.Printf("\n\nEmbedding: %v\n\n", embedding)
}

//func TestService_CleanupCV(t *testing.T) {
//	svc := New(&http.Client{
//		Timeout: 30 * time.Second,
//	}, "http://localhost:11434")
//
//}
