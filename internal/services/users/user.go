package users

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"strings"
	"time"

	"cv-evaluator/internal/integrations/extractor"
	"cv-evaluator/internal/models"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	defaultTokenTTL = 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailAlreadyUsed   = errors.New("email already used")
	ErrPhoneAlreadyUsed   = errors.New("phone already used")
	ErrInvalidToken       = errors.New("invalid token")
	ErrCVNotFound         = errors.New("cv not found")
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
	return NewWithAuth(db, llm, embeddingLLM, os.Getenv("JWT_SECRET"), defaultTokenTTL)
}

func NewWithAuth(db *gorm.DB, llm ChatLLMService, embeddingLLM EmbeddingLLMService, jwtSecret string, tokenTTL time.Duration) *Service {
	if strings.TrimSpace(jwtSecret) == "" {
		jwtSecret = mustGenerateJWTSecret()
		slog.Warn("JWT secret not configured; using an ephemeral in-memory secret", "env", "JWT_SECRET")
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

func (s *Service) Create(ctx context.Context, req SignupRequest) (*User, error) {
	newUser, err := s.createUser(ctx, req)
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

func (s *Service) UploadCV(ctx context.Context, userID ulid.ULID, cv *multipart.FileHeader) error {
	if cv == nil {
		return errors.New("cv file is required")
	}

	src, err := cv.Open()
	if err != nil {
		return err
	}
	defer func() {
		_ = src.Close()
	}()

	tmp, err := os.CreateTemp("", "cv-upload-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if _, err := io.Copy(tmp, src); err != nil {
		return err
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}

	return s.ProcessCV(ctx, userID, tmp)
}

func (s *Service) ListUserCVs(ctx context.Context, userID ulid.ULID) ([]models.CV, error) {
	var cvs []models.CV
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&cvs).Error; err != nil {
		return nil, err
	}
	return cvs, nil
}

func (s *Service) GetCV(ctx context.Context, userID, cvID ulid.ULID) (*models.CV, error) {
	var cv models.CV
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", cvID, userID).
		First(&cv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCVNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cv, nil
}

func (s *Service) DeleteCV(ctx context.Context, userID, cvID ulid.ULID) error {
	result := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", cvID, userID).
		Delete(&models.CV{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrCVNotFound
	}
	return nil
}

func (s *Service) ProcessCV(ctx context.Context, userID ulid.ULID, cv *os.File) error {
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
			UserID:         userID,
			CVID:           cvModel.ID,
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

func (s *Service) generateToken(userID ulid.ULID) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenTTL)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func (s *Service) parseTokenUserID(rawToken string) (ulid.ULID, error) {
	if strings.TrimSpace(rawToken) == "" {
		return ulid.ULID{}, ErrInvalidToken
	}

	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return ulid.ULID{}, ErrInvalidToken
	}

	userID, err := ulid.ParseStrict(claims.Subject)
	if err != nil || userID == (ulid.ULID{}) {
		return ulid.ULID{}, ErrInvalidToken
	}

	return userID, nil
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

func mustGenerateJWTSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("generate JWT secret: %w", err))
	}

	return base64.RawURLEncoding.EncodeToString(b)
}
