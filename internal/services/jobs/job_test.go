package jobs

import (
	"errors"
	"testing"

	"cv-evaluator/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestService_ProcessJob_CreatesNewJob(t *testing.T) {
	svc, db, embedding := newJobsServiceTestDB(t)
	job := makeJobPost("job-1", "build production systems in Go")

	embedding.On("CreateEmbedding", mock.Anything, job.Details.Details).
		Return(makeEmbedding768(), nil).
		Once()

	stored, err := svc.ProcessJob(t.Context(), job)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.True(t, stored.IsEmbedded())

	var got models.Job
	err = db.WithContext(t.Context()).Where("provider = ? AND job_id = ?", job.Provider, job.ID).First(&got).Error
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusEmbedded, got.Status)
	assert.Equal(t, job.Details.Details, got.Detail)
	assert.True(t, got.IsEmbedded())

	embedding.AssertExpectations(t)
}

func TestService_ProcessJob_EmbedsOnceAcrossCalls(t *testing.T) {
	svc, db, embedding := newJobsServiceTestDB(t)
	job := makeJobPost("job-once", "details")

	embedding.On("CreateEmbedding", mock.Anything, "details").
		Return(makeEmbedding768(), nil).
		Once()

	first, err := svc.ProcessJob(t.Context(), job)
	require.NoError(t, err)
	second, err := svc.ProcessJob(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)

	var count int64
	require.NoError(t, db.Model(&models.Job{}).Where("job_id = ?", "job-once").Count(&count).Error)
	assert.Equal(t, int64(1), count)

	embedding.AssertNumberOfCalls(t, "CreateEmbedding", 1)
}

func TestService_ProcessJob_SkipsWhenAlreadyEmbedded(t *testing.T) {
	svc, db, embedding := newJobsServiceTestDB(t)
	existing := &models.Job{
		JobID:      "job-2",
		Provider:   "linkedin",
		Title:      "Senior Software Engineer",
		Company:    "ACME",
		DetailsURL: "https://example.com/jobs/job-2",
		Status:     models.JobStatusEmbedded,
		Detail:     "already saved",
		Embedding:  makeJobEmbeddingVector(),
	}
	require.NoError(t, db.Create(existing).Error)

	stored, err := svc.ProcessJob(t.Context(), makeJobPost("job-2", "new details should be ignored"))
	require.NoError(t, err)
	assert.Equal(t, existing.ID, stored.ID)
	assert.Equal(t, "already saved", stored.Detail)

	embedding.AssertNotCalled(t, "CreateEmbedding", mock.Anything, mock.Anything)
}

func TestService_ProcessJob_ReturnsEmbeddingError(t *testing.T) {
	svc, db, embedding := newJobsServiceTestDB(t)
	job := makeJobPost("job-3", "details for embedding")
	expectedErr := errors.New("embedding failed")

	embedding.On("CreateEmbedding", mock.Anything, job.Details.Details).
		Return(nil, expectedErr).
		Once()

	_, err := svc.ProcessJob(t.Context(), job)
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)

	// The posting is kept, unembedded, so a retry only redoes the embedding.
	var got models.Job
	require.NoError(t, db.Where("job_id = ?", "job-3").First(&got).Error)
	assert.False(t, got.IsEmbedded())
	assert.Equal(t, models.JobStatusDescribed, got.Status)

	embedding.AssertExpectations(t)
}

func TestService_Embed_RequiresDetail(t *testing.T) {
	svc, db, embedding := newJobsServiceTestDB(t)
	job := &models.Job{Provider: "linkedin", JobID: "no-detail", Status: models.JobStatusNew}
	require.NoError(t, db.Create(job).Error)

	err := svc.Embed(t.Context(), job)
	require.ErrorIs(t, err, ErrNoDetail)
	embedding.AssertNotCalled(t, "CreateEmbedding", mock.Anything, mock.Anything)
}

func TestService_IsJobProcessed(t *testing.T) {
	svc, db, _ := newJobsServiceTestDB(t)
	embedded := &models.Job{
		JobID: "job-4", Provider: "linkedin", Status: models.JobStatusEmbedded,
		Detail: "stored details", Embedding: makeJobEmbeddingVector(),
	}
	pending := &models.Job{JobID: "job-5", Provider: "linkedin", Status: models.JobStatusNew}
	require.NoError(t, db.Create(embedded).Error)
	require.NoError(t, db.Create(pending).Error)

	got, err := svc.IsJobProcessed(t.Context(), "linkedin", "job-4")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, embedded.ID, got.ID)

	got, err = svc.IsJobProcessed(t.Context(), "linkedin", "job-5")
	require.NoError(t, err)
	assert.Nil(t, got, "a stored but unembedded job still needs processing")

	got, err = svc.IsJobProcessed(t.Context(), "jobindex", "job-4")
	require.NoError(t, err)
	assert.Nil(t, got, "jobs are scoped by provider")

	got, err = svc.IsJobProcessed(t.Context(), "linkedin", "missing-job")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestService_IsJobProcessed_PropagatesDBError(t *testing.T) {
	svc, db, _ := newJobsServiceTestDB(t)
	require.NoError(t, db.WithContext(t.Context()).Migrator().DropTable(&models.Job{}))

	exists, err := svc.IsJobProcessed(t.Context(), "linkedin", "job-6")
	require.Error(t, err)
	assert.Nil(t, exists)
	assert.False(t, errors.Is(err, gorm.ErrRecordNotFound))
}
