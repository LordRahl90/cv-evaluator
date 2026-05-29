package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"os"
	"strconv"
	"strings"
	"time"

	"cv-solution/internal/integrations/extractor"
	"cv-solution/internal/models"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/pgvector/pgvector-go"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	defaultJWTSecret = "change-me-in-production"
	defaultTokenTTL  = 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailAlreadyUsed   = errors.New("email already used")
	ErrPhoneAlreadyUsed   = errors.New("phone already used")
	ErrInvalidToken       = errors.New("invalid token")
)

type EmbeddingLLMService interface {
	CreateEmbedding(ctx context.Context, content string) ([]float32, error)
}

type ChatLLMService interface {
	CleanupCV(ctx context.Context, content string) (string, error)
}

type Service struct {
	db           *gorm.DB
	chatLLM      ChatLLMService
	embeddingLLM EmbeddingLLMService
	jwtSecret    []byte
	tokenTTL     time.Duration
}

func New(db *gorm.DB, llm ChatLLMService, embeddingLLM EmbeddingLLMService) *Service {
	return NewWithAuth(db, llm, embeddingLLM, defaultJWTSecret, defaultTokenTTL)
}

func NewWithAuth(db *gorm.DB, llm ChatLLMService, embeddingLLM EmbeddingLLMService, jwtSecret string, tokenTTL time.Duration) *Service {
	if strings.TrimSpace(jwtSecret) == "" {
		jwtSecret = defaultJWTSecret
	}
	if tokenTTL <= 0 {
		tokenTTL = defaultTokenTTL
	}

	return &Service{
		db:           db,
		chatLLM:      llm,
		embeddingLLM: embeddingLLM,
		jwtSecret:    []byte(jwtSecret),
		tokenTTL:     tokenTTL,
	}
}

func (s *Service) Create(ctx context.Context, req CreateUserRequest) (*User, error) {
	newUser, err := s.createUser(ctx, SignupRequest{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Email:     req.Email,
		Phone:     req.Phone,
		Password:  req.Password,
	})
	if err != nil {
		return nil, err
	}

	return &User{
		ID:        newUser.ID,
		FirstName: newUser.FirstName,
		LastName:  newUser.LastName,
		Email:     newUser.Email,
		Phone:     newUser.Phone,
	}, nil
}

func (s *Service) Signup(ctx context.Context, req SignupRequest) (*AuthResponse, error) {
	newUser, err := s.createUser(ctx, req)
	if err != nil {
		return nil, err
	}

	token, err := s.generateToken(newUser.ID)
	if err != nil {
		return nil, err
	}

	return &AuthResponse{Token: token, User: s.profileFromModel(newUser)}, nil
}

func (s *Service) Signin(ctx context.Context, req SigninRequest) (*AuthResponse, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || req.Password == "" {
		return nil, ErrInvalidCredentials
	}

	var existing models.User
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(existing.Password), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	token, err := s.generateToken(existing.ID)
	if err != nil {
		return nil, err
	}

	return &AuthResponse{Token: token, User: s.profileFromModel(&existing)}, nil
}

func (s *Service) Profile(ctx context.Context, req ProfileRequest) (*Profile, error) {
	userID, err := s.parseTokenUserID(req.Token)
	if err != nil {
		return nil, err
	}

	var existing models.User
	err = s.db.WithContext(ctx).Where("id = ?", userID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}

	profile := s.profileFromModel(&existing)
	return &profile, nil
}

func (s *Service) UploadCV(ctx context.Context, cv *multipart.FileHeader) error {
	return nil
}

func (s *Service) ProcessCV(ctx context.Context, userID int, cv *os.File) error {
	cvContent, err := extractor.ExtractFromFile(cv)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "CV content extracted", "content", cvContent)

	cvEmbedding, err := s.embeddingLLM.CreateEmbedding(ctx, cvContent)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "CV embedding created", "embedding", cvEmbedding)

	cvModel := &models.CV{
		UserID:           userID,
		ExtractedContent: cvContent,
		FullEmbedding:    pgvector.NewVector(cvEmbedding),
	}

	if err := s.db.WithContext(ctx).Create(cvModel).Error; err != nil {
		return err
	}

	// we clean it up to generate different sections
	sections, err := s.chatLLM.CleanupCV(ctx, cvContent)
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

		sectionEmbedding, err := s.embeddingLLM.CreateEmbedding(ctx, string(b))
		if err != nil {
			return err
		}

		// we store the embeddings in the database
		sectionEmbeddings = append(sectionEmbeddings, models.SectionEmbedding{
			CVID:           int(cvModel.ID),
			SectionHeading: heading,
			Section:        string(b),
			Embedding:      pgvector.NewVector(sectionEmbedding),
		})
		slog.DebugContext(ctx, "Section embedding created", "heading", heading, "embedding", sectionEmbedding)
	}

	return s.db.WithContext(ctx).Create(&sectionEmbeddings).Error
}

func (s *Service) createUser(ctx context.Context, req SignupRequest) (*models.User, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	phone := strings.TrimSpace(req.Phone)
	if email == "" || req.Password == "" {
		return nil, ErrInvalidCredentials
	}

	var existing models.User
	lookup := s.db.WithContext(ctx).Where("email = ?", email)
	if phone != "" {
		lookup = lookup.Or("phone = ?", phone)
	}

	err := lookup.First(&existing).Error
	if err == nil {
		if strings.EqualFold(existing.Email, email) {
			return nil, ErrEmailAlreadyUsed
		}
		if phone != "" && existing.Phone == phone {
			return nil, ErrPhoneAlreadyUsed
		}
		return nil, ErrEmailAlreadyUsed
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	newUser := &models.User{
		FirstName: strings.TrimSpace(req.FirstName),
		LastName:  strings.TrimSpace(req.LastName),
		Email:     email,
		Phone:     phone,
		Password:  string(passwordHash),
	}

	if err := s.db.WithContext(ctx).Create(newUser).Error; err != nil {
		return nil, err
	}

	return newUser, nil
}

func (s *Service) generateToken(userID uint) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatUint(uint64(userID), 10),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenTTL)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func (s *Service) parseTokenUserID(rawToken string) (uint, error) {
	if strings.TrimSpace(rawToken) == "" {
		return 0, ErrInvalidToken
	}

	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return 0, ErrInvalidToken
	}

	userID, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || userID == 0 {
		return 0, ErrInvalidToken
	}

	return uint(userID), nil
}

func (s *Service) profileFromModel(user *models.User) Profile {
	return Profile{
		ID:        user.ID,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Email:     user.Email,
		Phone:     user.Phone,
	}
}
