package sources

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/pipeline"
	"cv-evaluator/internal/providers"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	container := postgres.MustRun(t)
	db, err := container.OpenGorm(&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.JobSearch{}))
	return db
}

func ptr(t time.Time) *time.Time { return &t }

func TestStore(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	userID := ulid.Make()

	due := &models.JobSearch{UserID: userID, Name: "due", Query: "go", Frequency: models.FrequencyDaily, Enabled: true, NextRunAt: ptr(now.Add(-time.Minute))}
	older := &models.JobSearch{UserID: userID, Name: "older", Query: "go", Frequency: models.FrequencyHourly, Enabled: true, NextRunAt: ptr(now.Add(-time.Hour))}
	future := &models.JobSearch{UserID: userID, Name: "future", Query: "go", Frequency: models.FrequencyDaily, Enabled: true, NextRunAt: ptr(now.Add(time.Hour))}
	disabled := &models.JobSearch{UserID: userID, Name: "disabled", Query: "go", Frequency: models.FrequencyDaily, Enabled: false, NextRunAt: ptr(now.Add(-time.Minute))}
	unscheduled := &models.JobSearch{UserID: userID, Name: "unscheduled", Query: "go", Frequency: models.FrequencyDaily, Enabled: true}
	deleted := &models.JobSearch{UserID: userID, Name: "deleted", Query: "go", Frequency: models.FrequencyDaily, Enabled: true, NextRunAt: ptr(now.Add(-time.Minute))}
	for _, js := range []*models.JobSearch{due, older, future, disabled, unscheduled, deleted} {
		require.NoError(t, db.Create(js).Error)
	}
	require.NoError(t, db.Delete(deleted).Error)

	t.Run("DueSearches returns enabled, scheduled, due searches oldest first", func(t *testing.T) {
		got, err := store.DueSearches(ctx, now, 10)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, older.ID, got[0].ID)
		assert.Equal(t, due.ID, got[1].ID)
		assert.Equal(t, "go", got[1].Query)
		assert.Equal(t, models.FrequencyDaily, got[1].Frequency)

		limited, err := store.DueSearches(ctx, now, 1)
		require.NoError(t, err)
		require.Len(t, limited, 1)
		assert.Equal(t, older.ID, limited[0].ID)
	})

	t.Run("ClaimRun lets exactly one caller win a due run", func(t *testing.T) {
		lease := now.Add(30 * time.Minute)

		var wins atomic.Int32
		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				ok, err := store.ClaimRun(ctx, older.ID, now, lease)
				assert.NoError(t, err)
				if ok {
					wins.Add(1)
				}
			})
		}
		wg.Wait()
		assert.Equal(t, int32(1), wins.Load())

		var row models.JobSearch
		require.NoError(t, db.First(&row, "id = ?", older.ID).Error)
		require.NotNil(t, row.NextRunAt)
		assert.True(t, row.NextRunAt.Equal(lease))
	})

	t.Run("ClaimRun refuses searches that are not due, disabled or deleted", func(t *testing.T) {
		for _, js := range []*models.JobSearch{future, disabled, unscheduled, deleted} {
			ok, err := store.ClaimRun(ctx, js.ID, now, now.Add(time.Hour))
			require.NoError(t, err)
			assert.False(t, ok, js.Name)
		}
	})

	t.Run("MarkRun sets last and next run", func(t *testing.T) {
		next := now.Add(24 * time.Hour)
		require.NoError(t, store.MarkRun(ctx, due.ID, now, next))

		var row models.JobSearch
		require.NoError(t, db.First(&row, "id = ?", due.ID).Error)
		require.NotNil(t, row.LastRunAt)
		require.NotNil(t, row.NextRunAt)
		assert.True(t, row.LastRunAt.Equal(now))
		assert.True(t, row.NextRunAt.Equal(next))
	})

	t.Run("MarkRun leaves a disabled search unscheduled", func(t *testing.T) {
		require.NoError(t, store.MarkRun(ctx, disabled.ID, now, now.Add(time.Hour)))

		var row models.JobSearch
		require.NoError(t, db.First(&row, "id = ?", disabled.ID).Error)
		require.NotNil(t, row.LastRunAt)
		assert.Nil(t, row.NextRunAt)
	})
}

type fakeProvider struct {
	name     string
	criteria providers.Criteria
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Search(_ context.Context, c providers.Criteria) ([]entities.JobPost, error) {
	f.criteria = c
	return []entities.JobPost{{ID: "1", Provider: f.name}}, nil
}

func (f *fakeProvider) Details(_ context.Context, job *entities.JobPost) (*entities.JobDetails, error) {
	return &entities.JobDetails{ID: job.ID, Provider: f.name, Details: "details"}, nil
}

func TestFetcher(t *testing.T) {
	alpha, beta := &fakeProvider{name: "beta"}, &fakeProvider{name: "alpha"}
	fetcher := NewFetcher(providers.NewRegistry(alpha, beta))
	ctx := context.Background()

	assert.Equal(t, []string{"alpha", "beta"}, fetcher.Providers())

	posts, err := fetcher.Search(ctx, "beta", pipeline.Criteria{Query: "go", Location: "Copenhagen", MaxPages: 2})
	require.NoError(t, err)
	require.Len(t, posts, 1)
	assert.Equal(t, providers.Criteria{Query: "go", Location: "Copenhagen", MaxPages: 2}, alpha.criteria)

	details, err := fetcher.Details(ctx, "alpha", &posts[0])
	require.NoError(t, err)
	assert.Equal(t, "alpha", details.Provider)

	_, err = fetcher.Search(ctx, "stepstone", pipeline.Criteria{Query: "go"})
	require.ErrorIs(t, err, ErrUnknownProvider)
	_, err = fetcher.Details(ctx, "stepstone", &posts[0])
	require.ErrorIs(t, err, ErrUnknownProvider)
}
