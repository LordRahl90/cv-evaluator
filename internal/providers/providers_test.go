package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cv-evaluator/internal/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct{ name string }

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) Search(context.Context, Criteria) ([]entities.JobPost, error) {
	return nil, nil
}
func (f fakeProvider) Details(context.Context, *entities.JobPost) (*entities.JobDetails, error) {
	return nil, nil
}

func TestRegistry(t *testing.T) {
	r := NewRegistry(fakeProvider{"b"}, fakeProvider{"a"})

	assert.Equal(t, []string{"a", "b"}, r.Names())

	p, ok := r.Get("a")
	require.True(t, ok)
	assert.Equal(t, "a", p.Name())

	_, ok = r.Get("missing")
	assert.False(t, ok)

	// Names returns a copy.
	names := r.Names()
	names[0] = "mutated"
	assert.Equal(t, []string{"a", "b"}, r.Names())
}

func TestRegistry_Panics(t *testing.T) {
	assert.Panics(t, func() { NewRegistry(fakeProvider{"a"}, fakeProvider{"a"}) })
	assert.Panics(t, func() { NewRegistry(fakeProvider{""}) })
}

func TestDefault(t *testing.T) {
	r := Default()
	assert.Equal(t, []string{JobindexName, LinkedInName}, r.Names())
	for _, name := range r.Names() {
		p, ok := r.Get(name)
		require.True(t, ok)
		d, ok := p.(Describer)
		require.True(t, ok, "%s documents itself", name)
		info := d.Info()
		assert.NotEmpty(t, info.DisplayName)
		assert.True(t, strings.HasPrefix(info.Website, "https://"), info.Website)
		assert.NotEmpty(t, info.Coverage)
	}
}

// recordingServer serves a scraper's testdata directory and records the
// URLs that were requested.
type recordingServer struct {
	*httptest.Server
	mu   sync.Mutex
	urls []string
}

func newRecordingServer(t *testing.T, dir string) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	files := http.FileServer(http.Dir(dir))
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.urls = append(rs.urls, r.URL.String())
		rs.mu.Unlock()
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *recordingServer) requested() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.urls...)
}

func TestLinkedIn_SearchBuildsURL(t *testing.T) {
	srv := newRecordingServer(t, filepath.Join("..", "scraper", "linkedin", "testdata"))
	p := NewLinkedIn()
	p.searchBase = srv.URL + "/result-1.html"

	posts, err := p.Search(t.Context(), Criteria{Query: "backend engineer", Location: "Copenhagen", MaxPages: 1})
	require.NoError(t, err)
	require.NotEmpty(t, posts)
	assert.Equal(t, LinkedInName, posts[0].Provider)

	reqs := srv.requested()
	require.Len(t, reqs, 1)
	assert.Equal(t, "/result-1.html?keywords=backend+engineer&location=Copenhagen", reqs[0])
}

func TestLinkedIn_Details(t *testing.T) {
	srv := newRecordingServer(t, filepath.Join("..", "scraper", "linkedin", "testdata"))

	details, err := NewLinkedIn().Details(t.Context(), &entities.JobPost{
		ID:         "1001",
		DetailsURL: srv.URL + "/job-1001.html",
	})
	require.NoError(t, err)
	assert.Equal(t, "1001", details.ID)
	assert.NotEmpty(t, details.Details)
}

func TestJobindex_SearchBuildsURL(t *testing.T) {
	srv := newRecordingServer(t, filepath.Join("..", "scraper", "jobindex", "testdata"))
	p := NewJobindex()
	p.searchBase = srv.URL + "/result-1.html"

	posts, err := p.Search(t.Context(), Criteria{Query: "go", Location: "Aarhus", MaxPages: 1})
	require.NoError(t, err)
	require.NotEmpty(t, posts)
	assert.Equal(t, JobindexName, posts[0].Provider)

	reqs := srv.requested()
	require.Len(t, reqs, 1)
	assert.Equal(t, "/result-1.html?address=Aarhus&q=go", reqs[0])
}

func TestJobindex_Details(t *testing.T) {
	srv := newRecordingServer(t, filepath.Join("..", "scraper", "jobindex", "testdata"))

	details, err := NewJobindex().Details(t.Context(), &entities.JobPost{
		ID:         "2001",
		DetailsURL: srv.URL + "/jobannonce/2001",
	})
	require.NoError(t, err)
	assert.Equal(t, "2001", details.ID)
	assert.NotEmpty(t, details.Details)
}

func TestSearch_CriteriaErrors(t *testing.T) {
	for _, p := range []Provider{NewLinkedIn(), NewJobindex()} {
		t.Run(p.Name(), func(t *testing.T) {
			_, err := p.Search(t.Context(), Criteria{Location: "Copenhagen"})
			assert.ErrorIs(t, err, ErrEmptyCriteria)

			_, err = p.Search(t.Context(), Criteria{Query: "   "})
			assert.ErrorIs(t, err, ErrEmptyCriteria)
		})
	}
}
