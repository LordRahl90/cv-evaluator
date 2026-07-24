package jobs

import (
	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newJobsServiceTestDB(t *testing.T) (*Service, *gorm.DB, *MockEmbeddingLLMService, ulid.ULID) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	user := &models.User{
		FirstName: "Job",
		LastName:  "Tester",
		Email:     "jobs.service@example.com",
		Password:  "hashed-password",
	}
	require.NoError(t, db.Create(user).Error)

	embedding := new(MockEmbeddingLLMService)
	return New(db, embedding), db, embedding, user.ID
}

func makeEmbedding768() []float32 {
	return make([]float32, 768)
}

func makeJobEmbeddingVector() pgvector.Vector {
	return pgvector.NewVector(makeEmbedding768())
}

func makeJobPost(id string, details string) *entities.JobPost {
	return &entities.JobPost{
		ID:         id,
		Provider:   "linkedin",
		Title:      "Senior Software Engineer",
		Company:    "ACME",
		Location:   "Remote",
		PostedAt:   "today",
		DetailsURL: "https://example.com/jobs/" + id,
		Details: &entities.JobDetails{
			ID:      id,
			Details: details,
		},
	}
}
