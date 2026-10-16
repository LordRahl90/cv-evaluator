package jobs

import (
	"sync"
	"testing"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestService_SaveJob(t *testing.T) {
	svc, db := newStorageServiceTestDB(t)

	job := makeJob("linkedin", "job-1")
	require.Equal(t, ulid.ULID{}, job.ID)

	err := svc.SaveJob(t.Context(), job)
	require.NoError(t, err)
	require.NotEqual(t, ulid.ULID{}, job.ID)

	var stored models.Job
	err = db.WithContext(t.Context()).Where("id = ?", job.ID).First(&stored).Error
	require.NoError(t, err)
	assert.Equal(t, job.ID, stored.ID)
	assert.Equal(t, "linkedin", stored.Provider)
	assert.Equal(t, "job-1", stored.JobID)
	assert.True(t, stored.IsEmbedded())
}

func TestService_SaveJob_RejectsDuplicatePosting(t *testing.T) {
	svc, _ := newStorageServiceTestDB(t)

	require.NoError(t, svc.SaveJob(t.Context(), makeJob("linkedin", "dup")))
	err := svc.SaveJob(t.Context(), makeJob("linkedin", "dup"))
	require.Error(t, err, "(provider, job_id) must be unique")

	// The same provider job id from another provider is a different posting.
	require.NoError(t, svc.SaveJob(t.Context(), makeJob("jobindex", "dup")))
}

func TestService_Upsert(t *testing.T) {
	svc, db := newStorageServiceTestDB(t)

	first := makeJob("linkedin", "job-up")
	first.Embedding = nil
	stored, created, err := svc.Upsert(t.Context(), first)
	require.NoError(t, err)
	assert.True(t, created)
	assert.False(t, stored.IsEmbedded())

	second := makeJob("linkedin", "job-up")
	second.Title = "a different title"
	again, created, err := svc.Upsert(t.Context(), second)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, stored.ID, again.ID)
	assert.Equal(t, "Senior Software Engineer", again.Title, "an existing job is not overwritten")

	var count int64
	require.NoError(t, db.Model(&models.Job{}).Where("job_id = ?", "job-up").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestService_Upsert_Concurrent(t *testing.T) {
	svc, db := newStorageServiceTestDB(t)

	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ids     = map[ulid.ULID]int{}
		created int
	)
	for range n {
		wg.Go(func() {
			job, c, err := svc.Upsert(t.Context(), makeJob("linkedin", "race"))
			assert.NoError(t, err)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ids[job.ID]++
			if c {
				created++
			}
		})
	}
	wg.Wait()

	assert.Equal(t, 1, created)
	assert.Len(t, ids, 1)

	var count int64
	require.NoError(t, db.Model(&models.Job{}).Where("job_id = ?", "race").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestService_Upsert_RequiresIdentity(t *testing.T) {
	svc, _ := newStorageServiceTestDB(t)

	_, _, err := svc.Upsert(t.Context(), &models.Job{Provider: "linkedin"})
	require.Error(t, err)
}

func TestService_SaveDetailAndEmbedding(t *testing.T) {
	svc, _ := newStorageServiceTestDB(t)

	job := makeJob("linkedin", "job-emb")
	job.Embedding = nil
	job.Detail = ""
	require.NoError(t, svc.SaveJob(t.Context(), job))

	require.NoError(t, svc.SaveDetail(t.Context(), job.ID, "a fresh description"))
	vec := make([]float32, 768)
	vec[0] = 1
	require.NoError(t, svc.SaveEmbedding(t.Context(), job.ID, vec))

	got, err := svc.FindByID(t.Context(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, "a fresh description", got.Detail)
	assert.True(t, got.Visited)
	assert.Equal(t, models.JobStatusEmbedded, got.Status)
	require.True(t, got.IsEmbedded())
	assert.InDelta(t, 1, got.Embedding.Slice()[0], 0.0001)
}

func TestService_FindByID(t *testing.T) {
	svc, _ := newStorageServiceTestDB(t)

	job := makeJob("linkedin", "job-2")
	require.NoError(t, svc.SaveJob(t.Context(), job))

	got, err := svc.FindByID(t.Context(), job.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, job.ID, got.ID)
	assert.Equal(t, job.Provider, got.Provider)
	assert.Equal(t, job.JobID, got.JobID)
}

func TestService_FindByID_NotFound(t *testing.T) {
	svc, _ := newStorageServiceTestDB(t)

	missingID := ulid.Make()
	got, err := svc.FindByID(t.Context(), missingID)
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assert.Nil(t, got)
}

func TestService_FindByProviderAndJobID(t *testing.T) {
	svc, _ := newStorageServiceTestDB(t)

	require.NoError(t, svc.SaveJob(t.Context(), makeJob("linkedin", "job-3")))
	require.NoError(t, svc.SaveJob(t.Context(), makeJob("jobindex", "job-3")))

	got, err := svc.FindByProviderAndJobID(t.Context(), "linkedin", "job-3")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "linkedin", got.Provider)
	assert.Equal(t, "job-3", got.JobID)
}

func TestService_FindByProviders(t *testing.T) {
	svc, _ := newStorageServiceTestDB(t)

	require.NoError(t, svc.SaveJob(t.Context(), makeJob("linkedin", "job-10")))
	require.NoError(t, svc.SaveJob(t.Context(), makeJob("linkedin", "job-11")))
	require.NoError(t, svc.SaveJob(t.Context(), makeJob("jobindex", "job-12")))

	jobs, err := svc.FindByProviders(t.Context(), "linkedin")
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	for _, j := range jobs {
		assert.Equal(t, "linkedin", j.Provider)
	}
}
