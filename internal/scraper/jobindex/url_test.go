package jobindex

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
			name: "default base", query: "senior software engineer", location: "København",
			wantHost: "www.jobindex.dk", wantPath: "/jobsoegning",
			wantParams: url.Values{"q": {"senior software engineer"}, "address": {"København"}},
		},
		{
			name: "query only", query: " golang ",
			wantHost: "www.jobindex.dk", wantPath: "/jobsoegning",
			wantParams: url.Values{"q": {"golang"}},
		},
		{
			name: "keeps base params", base: "https://www.jobindex.dk/jobsoegning?jobage=7", query: "go",
			wantHost: "www.jobindex.dk", wantPath: "/jobsoegning",
			wantParams: url.Values{"q": {"go"}, "jobage": {"7"}},
		},
		{
			name: "custom base", base: "http://127.0.0.1:1234/result-1.html", query: "go",
			wantHost: "127.0.0.1:1234", wantPath: "/result-1.html",
			wantParams: url.Values{"q": {"go"}},
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
	_, err := (&Service{}).SearchJobs(t.Context())
	require.ErrorIs(t, err, ErrSearchURLRequired)
}
