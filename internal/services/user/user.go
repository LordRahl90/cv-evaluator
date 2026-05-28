package user

import (
	"context"
	"cv-solution/internal/integrations/extractor"
	"cv-solution/internal/models"
	"encoding/json"
	"mime/multipart"
	"os"

	"gorm.io/gorm"
)

type LLMService interface {
	CleanupCV(ctx context.Context, content string) (string, error)
	CreateEmbedding(ctx context.Context, content string) ([]float64, error)
}

type Service struct {
	db  *gorm.DB
	llm LLMService
}

func New(db *gorm.DB, llm LLMService) *Service {
	return &Service{
		db:  db,
		llm: llm,
	}
}

func (s *Service) Create(ctx context.Context, req CreateUserRequest) (*User, error) {
	return nil, nil
}

func (s *Service) UploadCV(ctx context.Context, cv *multipart.FileHeader) error {
	return nil
}

func (s *Service) processCV(ctx context.Context, userID int, cv *os.File) error {
	cvContent, err := extractor.ExtractFromFile(cv)
	if err != nil {
		return err
	}

	cvEmbedding, err := s.llm.CreateEmbedding(ctx, cvContent)
	if err != nil {
		return err
	}

	cvModel := &models.CV{
		UserID:           userID,
		ExtractedContent: cvContent,
		FullEmbedding:    cvEmbedding,
	}

	if err := s.db.WithContext(ctx).Create(cvModel).Error; err != nil {
		return err
	}

	// we clean it up to generate different sections
	sections, err := s.llm.CleanupCV(ctx, cvContent)
	if err != nil {
		return err
	}
	sectionMap := make(map[string]interface{})
	// we parse the content of the sections into the map

	if err := json.Unmarshal([]byte(sections), &sectionMap); err != nil {
		return err
	}

	var sectionEmbeddings []models.SectionEmbedding

	// then we create embeddings for each section
	for heading, v := range sectionMap {
		// let's parse the content back to string
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}

		sectionEmbedding, err := s.llm.CreateEmbedding(ctx, string(b))
		if err != nil {
			return err
		}

		// we store the embeddings in the database
		sectionEmbeddings = append(sectionEmbeddings, models.SectionEmbedding{
			CVID:           cvModel.ID,
			SectionHeading: heading,
			Section:        string(b),
			Embedding:      sectionEmbedding,
		})
	}

	return s.db.WithContext(ctx).Create(&sectionEmbeddings).Error
}
