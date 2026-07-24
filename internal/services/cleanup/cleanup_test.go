package cleanup

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/llm/deepseek"
	"cv-evaluator/internal/llm/ollama"

	deepseekLib "github.com/cohesion-org/deepseek-go"
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
	baseURL := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL"))
	if baseURL == "" {
		t.Skip("OLLAMA_BASE_URL not set; skipping Ollama integration test")
	}

	llmClient := ollama.New(&http.Client{
		Timeout: 30 * time.Minute,
	}, strings.TrimRight(baseURL, "/"))

	svc := New(llmClient)

	res, err := svc.CleanupCV(t.Context(), "./testdata/alugbin-abiodun-resume.pdf")
	require.NoError(t, err)
	require.NotEmpty(t, res)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}

func TestService_CleanupDeepSeek(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set; skipping DeepSeek integration test")
	}

	llmClient := deepseek.New(deepseekLib.NewClient(key))
	svc := New(llmClient)

	res, err := svc.CleanupCV(t.Context(), "./testdata/alugbin-abiodun-resume.pdf")
	require.NoError(t, err)
	require.NotEmpty(t, res)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}
