package jobs

import (
	"testing"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newJobsServiceTestDB(t *testing.T) (*Service, *gorm.DB, *MockEmbeddingLLMService) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	embedding := new(MockEmbeddingLLMService)
	return New(db, embedding), db, embedding
}

func makeEmbedding768() []float32 {
	return make([]float32, 768)
}

func makeJobEmbeddingVector() *pgvector.Vector {
	v := pgvector.NewVector(makeEmbedding768())
	return &v
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
