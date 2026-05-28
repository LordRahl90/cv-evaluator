package jobs

import (
	"context"

	"cv-solution/internal/models"

	"gorm.io/gorm"
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

func (s *Service) FindByID(ctx context.Context, id int) (*models.Job, error) {
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
