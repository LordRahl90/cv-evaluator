package users

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/llm/ollama"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	code := 1

	defer func() {
		os.Exit(code)
	}()

	code = m.Run()
}

func TestService_processCV(t *testing.T) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL")), "/")
	if baseURL == "" {
		t.Skip("OLLAMA_BASE_URL not set; skipping Ollama integration test")
	}

	probeClient := &http.Client{Timeout: 2 * time.Second}
	probeResp, err := probeClient.Get(baseURL + "/api/tags")
	if err != nil {
		t.Skipf("Ollama is not reachable at %s: %v", baseURL, err)
	}
	defer func() {
		require.NoError(t, probeResp.Body.Close())
	}()
	if probeResp.StatusCode >= http.StatusBadRequest {
		t.Skipf("Ollama probe at %s returned status %d", baseURL, probeResp.StatusCode)
	}

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)
	require.NotNil(t, db)

	llmClient := ollama.New(&http.Client{
		Timeout: 30 * time.Minute,
	}, baseURL)

	svc := New(db, llmClient, llmClient)
	cv, err := os.Open("./testdata/alugbin-abiodun-resume.pdf")
	require.NoError(t, err)
	require.NotNil(t, cv)
	defer func() {
		require.NoError(t, cv.Close())
	}()

	err = svc.ProcessCV(t.Context(), ulid.Make(), cv)
	require.NoError(t, err)
}
