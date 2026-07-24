package jobs

import (
	"testing"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newStorageServiceTestDB(t *testing.T) (*Service, *gorm.DB, *models.User) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	user := &models.User{
		FirstName: "Storage",
		LastName:  "Tester",
		Email:     "storage.jobs@example.com",
		Password:  "hashed-password",
	}
	require.NoError(t, db.Create(user).Error)

	return NewService(db), db, user
}

func makeVector768() pgvector.Vector {
	return pgvector.NewVector(make([]float32, 768))
}

func makeJob(userID ulid.ULID, provider, jobID string) *models.Job {
	return &models.Job{
		UserID:     userID,
		JobID:      jobID,
		Provider:   provider,
		Title:      "Senior Software Engineer",
		Company:    "ACME",
		Location:   "Remote",
		PostedAt:   "today",
		DetailsURL: "https://example.com/jobs/" + jobID,
		Status:     models.StatusPending,
		Detail:     "Build production systems in Go",
		Embedding:  makeVector768(),
	}
}
