package jobs

import (
	"errors"
	"testing"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestService_ProcessJob_CreatesNewJob(t *testing.T) {
	svc, db, embedding, userID := newJobsServiceTestDB(t)
	job := makeJobPost("job-1", "build production systems in Go")

	embedding.On("CreateEmbedding", mock.Anything, job.Details.Details).
		Return(makeEmbedding768(), nil).
		Once()

	err := svc.ProcessJob(t.Context(), userID, job)
	require.NoError(t, err)

	var stored models.Job
	err = db.WithContext(t.Context()).Where("user_id = ? AND job_id = ?", userID, job.ID).First(&stored).Error
	require.NoError(t, err)
	assert.Equal(t, userID, stored.UserID)
	assert.Equal(t, models.StatusPending, stored.Status)
	assert.Equal(t, job.Details.Details, stored.Detail)

	embedding.AssertExpectations(t)
}

func TestService_ProcessJob_SkipsWhenAlreadyProcessed(t *testing.T) {
	svc, db, embedding, userID := newJobsServiceTestDB(t)
	existing := &models.Job{
		UserID:     userID,
		JobID:      "job-2",
		Provider:   "linkedin",
		Title:      "Senior Software Engineer",
		Company:    "ACME",
		Location:   "Remote",
		PostedAt:   "today",
		DetailsURL: "https://example.com/jobs/job-2",
		Status:     models.StatusCompleted,
		Detail:     "already saved",
		Embedding:  makeJobEmbeddingVector(),
	}
	require.NoError(t, db.Create(existing).Error)

	err := svc.ProcessJob(t.Context(), userID, makeJobPost("job-2", "new details should be ignored"))
	require.NoError(t, err)

	var count int64
	err = db.WithContext(t.Context()).Model(&models.Job{}).
		Where("user_id = ? AND job_id = ?", userID, "job-2").
		Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	embedding.AssertNotCalled(t, "CreateEmbedding", mock.Anything, mock.Anything)
}

func TestService_ProcessJob_ReturnsEmbeddingError(t *testing.T) {
	svc, db, embedding, userID := newJobsServiceTestDB(t)
	job := makeJobPost("job-3", "details for embedding")
	expectedErr := errors.New("embedding failed")

	embedding.On("CreateEmbedding", mock.Anything, job.Details.Details).
		Return(nil, expectedErr).
		Once()

	err := svc.ProcessJob(t.Context(), userID, job)
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)

	var count int64
	err = db.WithContext(t.Context()).Model(&models.Job{}).
		Where("user_id = ? AND job_id = ?", userID, "job-3").
		Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	embedding.AssertExpectations(t)
}

func TestService_IsJobProcessed_Found(t *testing.T) {
	svc, db, _, userID := newJobsServiceTestDB(t)
	job := &models.Job{
		UserID:     userID,
		JobID:      "job-4",
		Provider:   "linkedin",
		Title:      "Senior Software Engineer",
		Company:    "ACME",
		Location:   "Remote",
		PostedAt:   "today",
		DetailsURL: "https://example.com/jobs/job-4",
		Status:     models.StatusPending,
		Detail:     "stored details",
		Embedding:  makeJobEmbeddingVector(),
	}
	require.NoError(t, db.Create(job).Error)

	exists, err := svc.IsJobProcessed(t.Context(), userID, "job-4")
	require.NoError(t, err)
	require.NotNil(t, exists)
	assert.Equal(t, userID, exists.UserID)
	assert.Equal(t, "job-4", exists.JobID)
}

func TestService_IsJobProcessed_NotFound(t *testing.T) {
	svc, _, _, userID := newJobsServiceTestDB(t)

	exists, err := svc.IsJobProcessed(t.Context(), userID, "missing-job")
	require.NoError(t, err)
	assert.Nil(t, exists)
}

func TestService_IsJobProcessed_UserScoped(t *testing.T) {
	svc, db, _, userID := newJobsServiceTestDB(t)
	otherUserID := ulid.Make()

	otherUser := &models.User{
		ID:        otherUserID,
		FirstName: "Other",
		LastName:  "User",
		Email:     "other.jobs.service@example.com",
		Password:  "hashed-password",
	}
	require.NoError(t, db.Create(otherUser).Error)

	job := &models.Job{
		UserID:     otherUserID,
		JobID:      "job-5",
		Provider:   "linkedin",
		Title:      "Senior Software Engineer",
		Company:    "ACME",
		Location:   "Remote",
		PostedAt:   "today",
		DetailsURL: "https://example.com/jobs/job-5",
		Status:     models.StatusPending,
		Detail:     "stored details",
		Embedding:  makeJobEmbeddingVector(),
	}
	require.NoError(t, db.Create(job).Error)

	exists, err := svc.IsJobProcessed(t.Context(), userID, "job-5")
	require.NoError(t, err)
	assert.Nil(t, exists)
}

func TestService_IsJobProcessed_PropagatesDBError(t *testing.T) {
	svc, db, _, userID := newJobsServiceTestDB(t)
	require.NoError(t, db.WithContext(t.Context()).Migrator().DropTable(&models.Job{}))

	exists, err := svc.IsJobProcessed(t.Context(), userID, "job-6")
	require.Error(t, err)
	assert.Nil(t, exists)
	assert.False(t, errors.Is(err, gorm.ErrRecordNotFound))
}
