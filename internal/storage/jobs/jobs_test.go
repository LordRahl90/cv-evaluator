package jobs

import (
	"testing"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestService_SaveJob(t *testing.T) {
	svc, db, user := newStorageServiceTestDB(t)

	job := makeJob(user.ID, "linkedin", "job-1")
	require.Equal(t, ulid.ULID{}, job.ID)

	err := svc.SaveJob(t.Context(), job)
	require.NoError(t, err)
	require.NotEqual(t, ulid.ULID{}, job.ID)

	var stored models.Job
	err = db.WithContext(t.Context()).Where("id = ?", job.ID).First(&stored).Error
	require.NoError(t, err)
	assert.Equal(t, job.ID, stored.ID)
	assert.Equal(t, user.ID, stored.UserID)
	assert.Equal(t, "linkedin", stored.Provider)
	assert.Equal(t, "job-1", stored.JobID)
}

func TestService_FindByID(t *testing.T) {
	svc, _, user := newStorageServiceTestDB(t)

	job := makeJob(user.ID, "linkedin", "job-2")
	require.NoError(t, svc.SaveJob(t.Context(), job))

	got, err := svc.FindByID(t.Context(), job.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, job.ID, got.ID)
	assert.Equal(t, job.Provider, got.Provider)
	assert.Equal(t, job.JobID, got.JobID)
}

func TestService_FindByID_NotFound(t *testing.T) {
	svc, _, _ := newStorageServiceTestDB(t)

	missingID := ulid.Make()
	got, err := svc.FindByID(t.Context(), missingID)
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assert.Nil(t, got)
}

func TestService_FindByProviderAndJobID(t *testing.T) {
	svc, _, user := newStorageServiceTestDB(t)

	require.NoError(t, svc.SaveJob(t.Context(), makeJob(user.ID, "linkedin", "job-3")))
	require.NoError(t, svc.SaveJob(t.Context(), makeJob(user.ID, "jobindex", "job-3")))

	got, err := svc.FindByProviderAndJobID(t.Context(), "linkedin", "job-3")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "linkedin", got.Provider)
	assert.Equal(t, "job-3", got.JobID)
}

func TestService_FindByProviders(t *testing.T) {
	svc, _, user := newStorageServiceTestDB(t)

	require.NoError(t, svc.SaveJob(t.Context(), makeJob(user.ID, "linkedin", "job-10")))
	require.NoError(t, svc.SaveJob(t.Context(), makeJob(user.ID, "linkedin", "job-11")))
	require.NoError(t, svc.SaveJob(t.Context(), makeJob(user.ID, "jobindex", "job-12")))

	jobs, err := svc.FindByProviders(t.Context(), "linkedin")
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	for _, j := range jobs {
		assert.Equal(t, "linkedin", j.Provider)
	}
}
