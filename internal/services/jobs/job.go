package jobs

import (
	"context"
	"cv-solution/internal/entities"
	"cv-solution/internal/models"
	"errors"
	"log/slog"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

type EmbeddingLLMService interface {
	CreateEmbedding(ctx context.Context, text string) ([]float32, error)
}

type Service struct {
	db               *gorm.DB
	embeddingService EmbeddingLLMService
}

func New(db *gorm.DB, embeddingLLMService EmbeddingLLMService) *Service {
	return &Service{
		db:               db,
		embeddingService: embeddingLLMService,
	}
}

func (s *Service) ProcessJob(ctx context.Context, userID int, job *entities.JobPost) error {
	// let's check if we've processed this job before
	exists, err := s.IsJobProcessed(ctx, userID, job.ID)
	if err != nil {
		return err
	}
	if exists != nil {
		slog.WarnContext(ctx, "job already processed", "job_id", job.ID, "user_id", userID)
		return nil
	}

	// we need to store jobs per user.
	jobModel := job.ToModel()
	jobModel.UserID = uint(userID)

	descEmbedding, err := s.embeddingService.CreateEmbedding(ctx, job.Details.Details)
	if err != nil {
		return err
	}
	jobModel.Embedding = pgvector.NewVector(descEmbedding)

	return s.db.WithContext(ctx).Create(&jobModel).Error
}

func (s *Service) IsJobProcessed(ctx context.Context, userID int, jobID string) (*models.Job, error) {
	var exists *models.Job

	err := s.db.WithContext(ctx).
		Where("job_id = ? AND user_id = ?", jobID, userID).
		First(&exists).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	return exists, nil
}
