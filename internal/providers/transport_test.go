package providers

import (
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"cv-evaluator/internal/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingTransport struct {
	mu sync.Mutex
	n  int
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return http.DefaultTransport.RoundTrip(r)
}

func (c *countingTransport) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestWithTransport(t *testing.T) {
	li := newRecordingServer(t, filepath.Join("..", "scraper", "linkedin", "testdata"))
	ji := newRecordingServer(t, filepath.Join("..", "scraper", "jobindex", "testdata"))
	rt := &countingTransport{}

	r := Default(WithTransport(rt))
	lp, _ := r.Get(LinkedInName)
	jp, _ := r.Get(JobindexName)
	lp.(*LinkedIn).searchBase = li.URL + "/result-1.html"
	jp.(*Jobindex).searchBase = ji.URL + "/result-1.html"

	_, err := lp.Search(t.Context(), Criteria{Query: "go", MaxPages: 1})
	require.NoError(t, err)
	_, err = lp.Details(t.Context(), &entities.JobPost{DetailsURL: li.URL + "/job-1001.html"})
	require.NoError(t, err)
	_, err = jp.Search(t.Context(), Criteria{Query: "go", MaxPages: 1})
	require.NoError(t, err)
	_, err = jp.Details(t.Context(), &entities.JobPost{DetailsURL: ji.URL + "/jobannonce/2001"})
	require.NoError(t, err)

	assert.Equal(t, 4, rt.count(), "every request goes through the injected transport")
}

func TestWithTransport_NilMeansDefault(t *testing.T) {
	assert.Equal(t, http.DefaultTransport, NewJobindex(WithTransport(nil)).transport)
	assert.Equal(t, http.DefaultTransport, NewJobindex().transport)
	assert.Equal(t, http.DefaultTransport, NewLinkedIn().scraper.Transport)
}
