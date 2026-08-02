package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cv-evaluator/db"
	"cv-evaluator/internal/llm/ollama"
	"cv-evaluator/internal/migrator"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/users"

	"github.com/joho/godotenv"
	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

func main() {
	var (
		cvPath    string
		firstName string
		lastName  string
		email     string
		phone     string
		password  string
		force     bool
	)

	flag.StringVar(&cvPath, "cv", "./data/alugbin-abiodun-resume.pdf", "Path to CV file")
	flag.StringVar(&firstName, "first-name", "Alugbin", "User first name")
	flag.StringVar(&lastName, "last-name", "Abiodun", "User last name")
	flag.StringVar(&email, "email", "alugbin.abiodun@gmail.com", "User email")
	flag.StringVar(&phone, "phone", "+4571358113", "User phone")
	flag.StringVar(&password, "password", "password123", "User password (only used on first seed)")
	flag.BoolVar(&force, "force", false, "Process CV even if user already has CVs")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	dbase, err := db.SetupDatabase()
	if err != nil {
		log.Fatal(err)
	}

	if err := migrator.Migrate(dbase); err != nil {
		log.Fatal(err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL")), "/")
	if baseURL == "" {
		log.Fatal("OLLAMA_BASE_URL is required")
	}

	llm := ollama.New(&http.Client{Timeout: 30 * time.Minute}, baseURL)
	svc := users.NewWithAuth(dbase, localCleanupService{}, llm, "seed-local-jwt-secret", time.Hour)

	userID, created, err := ensureUser(ctx, dbase, svc, users.SignupRequest{
		FirstName: firstName,
		LastName:  lastName,
		Email:     email,
		Phone:     phone,
		Password:  password,
	})
	if err != nil {
		log.Fatal(err)
	}

	if created {
		fmt.Printf("created user: %s (%s)\n", email, userID)
	} else {
		fmt.Printf("using existing user: %s (%s)\n", email, userID)
	}

	cvs, err := svc.ListUserCVs(ctx, userID)
	if err != nil {
		log.Fatal(err)
	}
	if len(cvs) > 0 && !force {
		fmt.Printf("user already has %d CV(s); skipping processing. pass -force to process again\n", len(cvs))
		return
	}

	file, err := os.Open(cvPath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = file.Close()
	}()

	if err := svc.ProcessCV(ctx, userID, file); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("processed CV for user %s from %s\n", userID, cvPath)
}

func ensureUser(ctx context.Context, dbase *gorm.DB, svc *users.Service, req users.SignupRequest) (ulid.ULID, bool, error) {
	var existing models.User
	err := dbase.WithContext(ctx).Where("email = ?", strings.ToLower(strings.TrimSpace(req.Email))).First(&existing).Error
	if err == nil {
		return existing.ID, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ulid.ULID{}, false, err
	}

	created, err := svc.Create(ctx, req)
	if err != nil {
		return ulid.ULID{}, false, err
	}

	return created.ID, true, nil
}

type localCleanupService struct{}

func (localCleanupService) CleanupCV(_ context.Context, content string) (string, error) {
	payload := map[string]interface{}{
		"summary":    content,
		"experience": content,
		"skills":     []string{},
		"education":  []string{},
		"title":      "Software Engineer",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
