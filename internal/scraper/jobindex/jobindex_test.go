package jobindex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cv-solution/internal/entities"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	code := 1
	defer func() { os.Exit(code) }()

	code = m.Run()
}

func TestSearch(t *testing.T) {
	testDataDir := filepath.Join("testdata")
	server := httptest.NewServer(http.FileServer(http.Dir(testDataDir)))
	defer server.Close()

	s := &Service{
		SearchLinkPage: fmt.Sprintf("%s/result-1.html", server.URL),
		MaxPages:       1,
	}

	posts, err := s.SearchJobs(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, posts)
	t.Log("Posts found:", len(posts))
	require.NotEmpty(t, strings.TrimSpace(posts[0].Title))
	require.NotEmpty(t, strings.TrimSpace(posts[0].Company))
	require.NotEmpty(t, strings.TrimSpace(posts[0].DetailsURL))
	require.NotEmpty(t, strings.TrimSpace(posts[0].ID))
}

func TestGetJobDetails(t *testing.T) {
	testDataDir := filepath.Join("testdata")
	server := httptest.NewServer(http.FileServer(http.Dir(testDataDir)))
	defer server.Close()

	s := &Service{}
	details, err := s.GetJobDetails(t.Context(), &entities.JobPost{
		ID:         "2001",
		Provider:   provider,
		DetailsURL: fmt.Sprintf("%s/jobannonce/2001", server.URL),
	})
	require.NoError(t, err)
	require.Equal(t, "2001", details.ID)
	require.Equal(t, provider, details.Provider)
	require.NotEmpty(t, strings.TrimSpace(details.Details))
	require.True(t, len(details.Details) > 50)
	require.NotContains(t, details.Details, "<ul>")
	require.NotContains(t, details.Details, "<div")
}
