package affinda

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

func TestService_UploadRawDocument(t *testing.T) {
	// this is to upload the job description
	key := "aff_4cdd61eacb5086ba420d4e2e7cdc8499f05feb3d"
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	content, err := os.ReadFile("./testdata/job-description.txt")
	require.NoError(t, err)
	require.NotEmpty(t, content)

	svc := New(client, key)
	res, err := svc.UploadRawDocument(t.Context(), string(content))
	require.NoError(t, err)

	fmt.Printf("\n\nResult is: %s\n\n", res)
}
