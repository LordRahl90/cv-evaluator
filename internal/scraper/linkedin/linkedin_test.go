package linkedin

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
		MaxPages:       defaultMaxPages,
	}

	posts, err := s.SearchJobs(t.Context(), fmt.Sprintf("%s/result-1.html", server.URL), defaultMaxPages)
	if err != nil {
		t.Fatal(err)
	}

	require.Equalf(t, 4, len(posts), "expected 4 posts from 3 pages")
	require.Equalf(t, "Backend Engineer I", posts[0].Title, "unexpected first title")
	require.Equalf(t, "Delta Systems", posts[3].Company, "unexpected last company")
	require.Equalf(t, "4386362563", posts[0].ID, "unexpected first job ID")
	require.Equalf(t, "4419494874", posts[3].ID, "unexpected last job ID")
}

func TestExtractLinkedInJobID(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "slug with trailing numeric id 1",
			url:  "https://dk.linkedin.com/jobs/view/senior-software-engineer-backend-at-contimo-4419494874?position=6&pageNum=0&refId=R5Y4%2B2bslV%2FRJeAkEYyZEw%3D%3D&trackingId=GRylC44uRfjexVSJ4T%2B58w%3D%3D",
			want: "4419494874",
		},
		{
			name: "slug with trailing numeric id 2",
			url:  "https://dk.linkedin.com/jobs/view/senior-backend-developer-at-joe-the-juice-4403796314?position=7&pageNum=0&refId=R5Y4%2B2bslV%2FRJeAkEYyZEw%3D%3D&trackingId=ifCQvRcyX3GzkT5EJD%2FIMw%3D%3D",
			want: "4403796314",
		},
		{
			name: "slug with trailing numeric id 3",
			url:  "https://dk.linkedin.com/jobs/view/senior-software-engineer-at-subsets-4384191633?position=8&pageNum=0&refId=R5Y4%2B2bslV%2FRJeAkEYyZEw%3D%3D&trackingId=DvQOci2WrsySOUYkLI%2FTPw%3D%3D",
			want: "4384191633",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, extractLinkedInJobID(tc.url))
		})
	}
}

func TestGetJobDetails(t *testing.T) {
	testDataDir := filepath.Join("testdata")
	server := httptest.NewServer(http.FileServer(http.Dir(testDataDir)))
	defer server.Close()

	s := &Service{}
	details, err := s.GetJobDetails(t.Context(), &entities.JobPost{
		ID:         "1001",
		Provider:   provider,
		DetailsURL: fmt.Sprintf("%s/job-1001.html", server.URL),
	})
	require.NoError(t, err)
	require.Equal(t, "1001", details.ID)
	require.Equal(t, provider, details.Provider)
	require.NotEmpty(t, strings.TrimSpace(details.Details))
	require.True(t, len(details.Details) > 100)
	require.NotContains(t, details.Details, "<ul>")
	require.NotContains(t, details.Details, "<div")
}
