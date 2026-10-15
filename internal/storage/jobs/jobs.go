package jobs

import (
	"context"
	"errors"
	"fmt"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) SaveJob(ctx context.Context, job *models.Job) error {
	return s.db.WithContext(ctx).Create(job).Error
}

// Upsert stores job unless a job with the same provider and provider job id
// already exists. It returns the stored row (the existing one when there
// was a conflict) and whether it was created by this call. It is safe to
// call concurrently for the same posting: exactly one caller creates it.
func (s *Service) Upsert(ctx context.Context, job *models.Job) (*models.Job, bool, error) {
	if job.Provider == "" || job.JobID == "" {
		return nil, false, errors.New("job provider and job id are required")
	}

	res := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "provider"}, {Name: "job_id"}},
			DoNothing: true,
		}).
		Create(job)
	if res.Error != nil {
		return nil, false, fmt.Errorf("upsert job: %w", res.Error)
	}
	if res.RowsAffected == 1 {
		return job, true, nil
	}

	existing, err := s.FindByProviderAndJobID(ctx, job.Provider, job.JobID)
	if err != nil {
		return nil, false, fmt.Errorf("load existing job: %w", err)
	}
	return existing, false, nil
}

// SaveDetail stores a job's scraped description.
func (s *Service) SaveDetail(ctx context.Context, id ulid.ULID, detail string) error {
	return s.db.WithContext(ctx).Model(&models.Job{}).
		Where("id = ?", id).
		Updates(map[string]any{"detail": detail, "visited": true}).Error
}

// SaveEmbedding stores a job's embedding and marks it completed.
func (s *Service) SaveEmbedding(ctx context.Context, id ulid.ULID, embedding []float32) error {
	vec := pgvector.NewVector(embedding)
	return s.db.WithContext(ctx).Model(&models.Job{}).
		Where("id = ?", id).
		Updates(map[string]any{"embedding": &vec, "status": models.StatusCompleted}).Error
}

func (s *Service) FindByID(ctx context.Context, id ulid.ULID) (*models.Job, error) {
	var job models.Job
	if err := s.db.WithContext(ctx).
		Where("id = ?", id).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Service) FindByProviderAndJobID(ctx context.Context, provider, jobID string) (*models.Job, error) {
	var job models.Job
	if err := s.db.WithContext(ctx).
		Where("provider = ? AND job_id = ?", provider, jobID).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Service) FindByProviders(ctx context.Context, provider string) ([]models.Job, error) {
	var j []models.Job
	if err := s.db.WithContext(ctx).
		Where("provider = ?", provider).Find(&j).Error; err != nil {
		return nil, err
	}
	return j, nil
}
