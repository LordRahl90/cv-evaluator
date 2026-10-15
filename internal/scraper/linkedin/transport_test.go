package linkedin

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cv-evaluator/internal/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingTransport struct{ n atomic.Int32 }

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return http.DefaultTransport.RoundTrip(r)
}

func TestTransport_DefaultsToHTTPDefault(t *testing.T) {
	assert.Equal(t, http.DefaultTransport, (&Service{}).transport())
}

func TestTransport_IsUsed(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	rt := &countingTransport{}
	s := &Service{Transport: rt}

	_, err := s.SearchJobs(t.Context(), server.URL+"/result-1.html", 1)
	require.NoError(t, err)
	_, err = s.GetJobDetails(t.Context(), &entities.JobPost{DetailsURL: server.URL + "/job-1001.html"})
	require.NoError(t, err)
	assert.EqualValues(t, 2, rt.n.Load(), "requests go through the injected transport")
}
