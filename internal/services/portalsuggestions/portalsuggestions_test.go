package portalsuggestions

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) *Service {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	return New(db, "https://www.linkedin.com/jobs", "https://www.jobindex.dk", "::not a url")
}

func TestService_CreateList(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	userID, other := ulid.Make(), ulid.Make()

	created, err := svc.Create(ctx, userID, CreateParams{
		Name: "  StepStone  ",
		URL:  " https://www.StepStone.de/ ",
		Note: "  Big in Germany  ",
	})
	require.NoError(t, err)
	assert.NotEqual(t, ulid.ULID{}, created.ID)
	assert.Equal(t, "StepStone", created.Name)
	assert.Equal(t, "https://stepstone.de", created.URL)
	assert.Equal(t, "Big in Germany", created.Note)
	assert.Equal(t, models.PortalSuggestionSubmitted, created.Status)

	// Same portal spelled differently is a duplicate for the same user.
	existing, err := svc.Create(ctx, userID, CreateParams{URL: "stepstone.de#jobs"})
	require.ErrorIs(t, err, ErrAlreadySuggested)
	require.NotNil(t, existing)
	assert.Equal(t, created.ID, existing.ID)

	// Another user can suggest the same portal.
	otherCreated, err := svc.Create(ctx, other, CreateParams{URL: "https://stepstone.de"})
	require.NoError(t, err)
	assert.Equal(t, "stepstone.de", otherCreated.Name, "name defaults to the host")

	second, err := svc.Create(ctx, userID, CreateParams{URL: "http://www.indeed.com/jobs?l=Berlin"})
	require.NoError(t, err)
	assert.Equal(t, "https://indeed.com/jobs?l=Berlin", second.URL)

	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	ids := []ulid.ULID{list[0].ID, list[1].ID}
	assert.ElementsMatch(t, []ulid.ULID{created.ID, second.ID}, ids)

	list, err = svc.List(ctx, other)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, otherCreated.ID, list[0].ID)

	list, err = svc.List(ctx, ulid.Make())
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestService_ConcurrentDuplicates(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	userID := ulid.Make()

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	ids := make([]ulid.ULID, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := svc.Create(ctx, userID, CreateParams{URL: "https://www.monster.dk"})
			errs[i] = err
			if rec != nil {
				ids[i] = rec.ID
			}
		}()
	}
	wg.Wait()

	created := 0
	for i, err := range errs {
		if err == nil {
			created++
			continue
		}
		require.ErrorIs(t, err, ErrAlreadySuggested)
		assert.NotEqual(t, ulid.ULID{}, ids[i])
	}
	assert.Equal(t, 1, created)

	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestService_Validation(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		params  CreateParams
		param   string
		missing bool
	}{
		{"missing url", CreateParams{URL: "  "}, "url", true},
		{"long url", CreateParams{URL: "https://a.com/" + strings.Repeat("a", MaxURLLength)}, "url", false},
		{"ftp url", CreateParams{URL: "ftp://files.example.com"}, "url", false},
		{"no domain", CreateParams{URL: "localhost"}, "url", false},
		{"credentials", CreateParams{URL: "https://user:pass@example.com"}, "url", false},
		{"garbage", CreateParams{URL: "https://exa mple.com"}, "url", false},
		{"long name", CreateParams{URL: "example.com", Name: strings.Repeat("a", MaxNameLength+1)}, "name", false},
		{"long note", CreateParams{URL: "example.com", Note: strings.Repeat("a", MaxNoteLength+1)}, "note", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(ctx, ulid.Make(), tt.params)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tt.param, verr.Param)
			assert.Equal(t, tt.missing, verr.Missing)
			assert.NotEmpty(t, verr.Error())
		})
	}
}

func TestService_AlreadySupported(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	for _, raw := range []string{"https://www.jobindex.dk/jobsoegning", "linkedin.com", "https://dk.linkedin.com/jobs"} {
		_, err := svc.Create(ctx, ulid.Make(), CreateParams{URL: raw})
		assert.ErrorIs(t, err, ErrAlreadySupported, raw)
	}

	// A look-alike domain is not the supported portal.
	_, err := svc.Create(ctx, ulid.Make(), CreateParams{URL: "https://notjobindex.dk"})
	require.NoError(t, err)
}

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"https://Example.com/":            "https://example.com",
		"http://www.example.com:80/jobs/": "https://example.com/jobs",
		"https://example.com:8443/a?b=c":  "https://example.com:8443/a?b=c",
		"https://example.com/a#frag":      "https://example.com/a",
	}
	for in, want := range tests {
		u, err := url.Parse(in)
		require.NoError(t, err)
		assert.Equal(t, want, normalize(u), in)
	}
}
