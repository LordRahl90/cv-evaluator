package linkedin

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchURL(t *testing.T) {
	tests := []struct {
		name, base, query, location string
		wantHost, wantPath          string
		wantParams                  url.Values
	}{
		{
			name: "default base", query: "Senior Software Engineer", location: "Copenhagen",
			wantHost: "www.linkedin.com", wantPath: "/jobs/search",
			wantParams: url.Values{"keywords": {"Senior Software Engineer"}, "location": {"Copenhagen"}},
		},
		{
			name: "location only", location: " Denmark ",
			wantHost: "www.linkedin.com", wantPath: "/jobs/search",
			wantParams: url.Values{"location": {"Denmark"}},
		},
		{
			name: "keeps base params", base: "https://www.linkedin.com/jobs/search?f_TPR=r604800", query: "go",
			wantHost: "www.linkedin.com", wantPath: "/jobs/search",
			wantParams: url.Values{"keywords": {"go"}, "f_TPR": {"r604800"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := SearchURL(tt.base, tt.query, tt.location)
			require.NoError(t, err)
			u, err := url.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, u.Host)
			assert.Equal(t, tt.wantPath, u.Path)
			assert.Equal(t, tt.wantParams, u.Query())
		})
	}
}

func TestSearchURL_InvalidBase(t *testing.T) {
	_, err := SearchURL("://bad", "go", "")
	require.Error(t, err)
}

func TestSearchJobs_RequiresURL(t *testing.T) {
	_, err := New().SearchJobs(t.Context(), "", 1)
	require.ErrorIs(t, err, ErrSearchURLRequired)
}
