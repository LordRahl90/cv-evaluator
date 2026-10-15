package jobsearches

import (
	"context"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var fixedNow = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	svc := New(db)
	svc.now = func() time.Time { return fixedNow }
	return svc, db
}

func ptr[T any](v T) *T { return &v }

func validParams() CreateParams {
	return CreateParams{
		Name:     "Go jobs in Copenhagen",
		Query:    "golang",
		Location: "Copenhagen",
	}
}

func TestService_CRUD(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	userID := ulid.Make()

	p := validParams()
	p.Name = "  Go jobs in Copenhagen  "
	created, err := svc.Create(ctx, userID, p)
	require.NoError(t, err)
	assert.NotEqual(t, ulid.ULID{}, created.ID)
	assert.Equal(t, "Go jobs in Copenhagen", created.Name)
	assert.Equal(t, DefaultFrequency, created.Frequency)
	assert.True(t, created.Enabled)
	require.NotNil(t, created.NextRunAt)
	assert.True(t, fixedNow.Equal(*created.NextRunAt), "a new search is due immediately")
	assert.Nil(t, created.LastRunAt)

	got, err := svc.Get(ctx, userID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "golang", got.Query)

	second, err := svc.Create(ctx, userID, CreateParams{
		Name:      "Jobindex weekly",
		Query:     "go",
		Frequency: models.FrequencyWeekly,
		Enabled:   ptr(false),
	})
	require.NoError(t, err)
	assert.False(t, second.Enabled)
	assert.Nil(t, second.NextRunAt, "a disabled search is not scheduled")

	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list, 2)

	updated, err := svc.Update(ctx, userID, created.ID, UpdateParams{
		Name:     ptr(" Renamed "),
		Location: ptr(""),
	})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Empty(t, updated.Location)
	assert.Equal(t, "golang", updated.Query, "unset fields are kept")

	got, err = svc.Get(ctx, userID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Name)
	assert.Empty(t, got.Location)

	require.NoError(t, svc.Delete(ctx, userID, created.ID))
	_, err = svc.Get(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, svc.Delete(ctx, userID, created.ID), ErrNotFound)

	// Deletion is soft.
	var count int64
	require.NoError(t, db.Unscoped().Model(&models.JobSearch{}).Where("id = ?", created.ID).Count(&count).Error)
	assert.EqualValues(t, 1, count)

	list, err = svc.List(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, second.ID, list[0].ID)
}

func TestService_ScopedToOwner(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	owner, other := ulid.Make(), ulid.Make()

	created, err := svc.Create(ctx, owner, validParams())
	require.NoError(t, err)

	_, err = svc.Get(ctx, other, created.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.Update(ctx, other, created.ID, UpdateParams{Name: ptr("hijack")})
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.RunNow(ctx, other, created.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, svc.Delete(ctx, other, created.ID), ErrNotFound)

	list, err := svc.List(ctx, other)
	require.NoError(t, err)
	assert.Empty(t, list)

	got, err := svc.Get(ctx, owner, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Go jobs in Copenhagen", got.Name)
}

func TestService_Scheduling(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	userID := ulid.Make()

	created, err := svc.Create(ctx, userID, validParams())
	require.NoError(t, err)

	// Disabling clears the schedule.
	disabled, err := svc.Update(ctx, userID, created.ID, UpdateParams{Enabled: ptr(false)})
	require.NoError(t, err)
	assert.Nil(t, disabled.NextRunAt)
	got, err := svc.Get(ctx, userID, created.ID)
	require.NoError(t, err)
	assert.Nil(t, got.NextRunAt)

	_, err = svc.RunNow(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrDisabled)

	// Re-enabling makes it due now.
	later := fixedNow.Add(time.Hour)
	svc.now = func() time.Time { return later }
	enabled, err := svc.Update(ctx, userID, created.ID, UpdateParams{Enabled: ptr(true)})
	require.NoError(t, err)
	require.NotNil(t, enabled.NextRunAt)
	assert.True(t, later.Equal(*enabled.NextRunAt))

	// Simulate a scheduler run, then change the frequency: the next run is
	// recomputed from the last run.
	lastRun := later.Add(time.Minute)
	next := enabled.NextRun(lastRun)
	require.NoError(t, db.Model(&models.JobSearch{}).Where("id = ?", created.ID).
		Updates(map[string]any{"last_run_at": lastRun, "next_run_at": next}).Error)

	hourly, err := svc.Update(ctx, userID, created.ID, UpdateParams{Frequency: ptr(models.FrequencyHourly)})
	require.NoError(t, err)
	require.NotNil(t, hourly.NextRunAt)
	assert.True(t, lastRun.Add(time.Hour).Equal(*hourly.NextRunAt), "got %v", hourly.NextRunAt)

	// Updating other fields leaves the schedule alone.
	renamed, err := svc.Update(ctx, userID, created.ID, UpdateParams{Name: ptr("x")})
	require.NoError(t, err)
	assert.True(t, lastRun.Add(time.Hour).Equal(*renamed.NextRunAt))

	// RunNow makes it due immediately and persists that.
	runAt := lastRun.Add(5 * time.Minute)
	svc.now = func() time.Time { return runAt }
	ran, err := svc.RunNow(ctx, userID, created.ID)
	require.NoError(t, err)
	require.NotNil(t, ran.NextRunAt)
	assert.True(t, runAt.Equal(*ran.NextRunAt))

	got, err = svc.Get(ctx, userID, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.NextRunAt)
	assert.True(t, runAt.Equal(*got.NextRunAt))
}

func TestService_Validation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	userID := ulid.Make()

	tests := []struct {
		name    string
		mutate  func(p *CreateParams)
		param   string
		missing bool
	}{
		{"long name", func(p *CreateParams) { p.Name = strings.Repeat("a", MaxNameLength+1) }, "name", false},
		{"bad frequency", func(p *CreateParams) { p.Frequency = "monthly" }, "frequency", false},
		{"missing query", func(p *CreateParams) { p.Query = "  " }, "query", true},
		{"long query", func(p *CreateParams) { p.Query = strings.Repeat("a", MaxQueryLength+1) }, "query", false},
		{"long location", func(p *CreateParams) { p.Location = strings.Repeat("a", MaxLocationLength+1) }, "location", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validParams()
			tt.mutate(&p)
			_, err := svc.Create(ctx, userID, p)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tt.param, verr.Param)
			assert.Equal(t, tt.missing, verr.Missing)
			assert.NotEmpty(t, verr.Error())
		})
	}

	// Name defaults to the query; location is optional.
	p := validParams()
	p.Name = ""
	p.Location = ""
	defaulted, err := svc.Create(ctx, userID, p)
	require.NoError(t, err)
	assert.Equal(t, "golang", defaulted.Name)
	assert.Empty(t, defaulted.Location)

	// Updates are validated against the resulting search.
	created, err := svc.Create(ctx, userID, validParams())
	require.NoError(t, err)
	_, err = svc.Update(ctx, userID, created.ID, UpdateParams{Query: ptr("")})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "query", verr.Param)

	_, err = svc.Update(ctx, userID, created.ID, UpdateParams{Frequency: ptr("monthly")})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "frequency", verr.Param)

	got, err := svc.Get(ctx, userID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "golang", got.Query, "a rejected update changes nothing")

	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}
