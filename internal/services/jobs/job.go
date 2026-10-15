package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	jobstorage "cv-evaluator/internal/storage/jobs"

	"gorm.io/gorm"
)

// ErrNoDetail is returned when a job has no description to embed.
var ErrNoDetail = errors.New("job has no description to embed")

type EmbeddingLLMService interface {
	CreateEmbedding(ctx context.Context, text string) ([]float32, error)
}

// Service ingests global job postings: it stores each posting once per
// (provider, job id) and embeds its description once.
type Service struct {
	store            *jobstorage.Service
	embeddingService EmbeddingLLMService
}

func New(db *gorm.DB, embeddingLLMService EmbeddingLLMService) *Service {
	return &Service{
		store:            jobstorage.NewService(db),
		embeddingService: embeddingLLMService,
	}
}

// ProcessJob stores the posting if it is new and embeds it if it is not
// embedded yet. Calling it again for the same posting is a no-op once the
// job is embedded, so a posting is never embedded twice.
func (s *Service) ProcessJob(ctx context.Context, job *entities.JobPost) (*models.Job, error) {
	stored, created, err := s.store.Upsert(ctx, job.ToModel())
	if err != nil {
		return nil, err
	}
	if !created {
		slog.DebugContext(ctx, "job already stored", "provider", job.Provider, "job_id", job.ID)
	}

	if stored.Detail == "" && job.Details != nil && job.Details.Details != "" {
		if err := s.store.SaveDetail(ctx, stored.ID, job.Details.Details); err != nil {
			return nil, err
		}
		stored.Detail = job.Details.Details
		stored.Visited = true
	}

	if err := s.Embed(ctx, stored); err != nil {
		return nil, err
	}
	return stored, nil
}

// Embed embeds the job's description and stores the vector. It does nothing
// when the job is already embedded.
func (s *Service) Embed(ctx context.Context, job *models.Job) error {
	if job.IsEmbedded() {
		return nil
	}
	if strings.TrimSpace(job.Detail) == "" {
		return fmt.Errorf("embed job %s/%s: %w", job.Provider, job.JobID, ErrNoDetail)
	}

	embedding, err := s.embeddingService.CreateEmbedding(ctx, job.Detail)
	if err != nil {
		return err
	}
	if err := s.store.SaveEmbedding(ctx, job.ID, embedding); err != nil {
		return err
	}

	reloaded, err := s.store.FindByID(ctx, job.ID)
	if err != nil {
		return err
	}
	*job = *reloaded
	return nil
}

// IsJobProcessed returns the stored job when the posting exists and is
// already embedded, or nil when it still needs processing.
func (s *Service) IsJobProcessed(ctx context.Context, provider, jobID string) (*models.Job, error) {
	job, err := s.store.FindByProviderAndJobID(ctx, provider, jobID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !job.IsEmbedded() {
		return nil, nil
	}
	return job, nil
}
