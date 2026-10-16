package jobs

import (
	"testing"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newStorageServiceTestDB(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	return NewService(db), db
}

func makeVector768() *pgvector.Vector {
	v := pgvector.NewVector(make([]float32, 768))
	return &v
}

func makeJob(provider, jobID string) *models.Job {
	return &models.Job{
		JobID:      jobID,
		Provider:   provider,
		Title:      "Senior Software Engineer",
		Company:    "ACME",
		Location:   "Remote",
		PostedAt:   "today",
		DetailsURL: "https://example.com/jobs/" + jobID,
		Status:     models.JobStatusNew,
		Detail:     "Build production systems in Go",
		Embedding:  makeVector768(),
	}
}
