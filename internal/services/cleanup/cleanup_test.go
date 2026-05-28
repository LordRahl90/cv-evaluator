package cleanup

import (
	"cv-solution/internal/llm/deepseek"
	"cv-solution/internal/llm/ollama"
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

func TestService_CleanupCV(t *testing.T) {
	llmClient := ollama.New(&http.Client{
		Timeout: 30 * time.Minute,
	}, "http://localhost:11434/api/generate")

	svc := New(llmClient)

	res, err := svc.CleanupCV(t.Context(), "./testdata/alugbin-abiodun-resume.pdf")
	require.NoError(t, err)
	require.NotEmpty(t, res)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}

func TestService_CleanupDeepSeek(t *testing.T) {
	key := "sk-3f601982cfe149538e5493c50f87a06b"
	llmClient := deepseek.New(key)
	svc := New(llmClient)

	res, err := svc.CleanupCV(t.Context(), "./testdata/alugbin-abiodun-resume.pdf")
	require.NoError(t, err)
	require.NotEmpty(t, res)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}
