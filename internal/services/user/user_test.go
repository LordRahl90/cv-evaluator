package user

import (
	"cv-solution/internal/testutil/postgres"
	"net/http"
	"os"
	"testing"
	"time"

	"cv-solution/internal/llm/ollama"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var (
	db *gorm.DB
)

func TestMain(m *testing.M) {
	code := 1

	defer func() {
		os.Exit(code)
	}()

	code = m.Run()
}

func TestService_processCV(t *testing.T) {
	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)
	require.NotNil(t, db)

	llmClient := ollama.New(&http.Client{
		Timeout: 30 * time.Minute,
	}, "http://localhost:11434")

	svc := New(db, llmClient)
	cv, err := os.Open("./testdata/alugbin-abiodun-resume.pdf")
	require.NoError(t, err)
	require.NotNil(t, cv)
	defer func() {
		require.NoError(t, cv.Close())
	}()

	err = svc.processCV(t.Context(), 123, cv)
	require.NoError(t, err)
}
