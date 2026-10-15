package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cv-evaluator/db"
	"cv-evaluator/internal/config"
	"cv-evaluator/internal/llm"
	"cv-evaluator/internal/llm/deepseek"
	"cv-evaluator/internal/llm/ollama"
	"cv-evaluator/internal/migrator"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/users"
	"cv-evaluator/internal/telemetry"

	"github.com/joho/godotenv"
	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

type options struct {
	cvPath string
	user   users.SignupRequest
	force  bool
}

func main() {
	var opts options

	flag.StringVar(&opts.cvPath, "cv", "./data/dummy-resume.pdf", "Path to CV file")
	flag.StringVar(&opts.user.FirstName, "first-name", "Jane", "User first name")
	flag.StringVar(&opts.user.LastName, "last-name", "Doe", "User last name")
	flag.StringVar(&opts.user.Email, "email", "jane.doe@example.com", "User email")
	flag.StringVar(&opts.user.Phone, "phone", "+15550100", "User phone")
	flag.StringVar(&opts.user.Password, "password", "password123", "User password (only used on first seed)")
	flag.BoolVar(&opts.force, "force", false, "Process CV even if user already has CVs")
	flag.Parse()

	// run returns instead of exiting so deferred cleanup (telemetry flush)
	// happens before the process exits.
	if err := run(opts); err != nil {
		log.Fatal(err)
	}
}

func run(opts options) error {
	if err := godotenv.Load(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()
	shutdownTelemetry, err := telemetry.Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer telemetry.Flush(shutdownTelemetry)

	dbase, err := db.Open(cfg.Database)
	if err != nil {
		return err
	}

	if err := migrator.Migrate(dbase); err != nil {
		return err
	}

	ollamaLLM := ollama.New(&http.Client{Timeout: 30 * time.Minute}, cfg.OllamaBaseURL)
	deepseekLLM := deepseek.New(deepseek.NewClient(cfg.DeepSeekAPIKey))
	cleaner, err := llm.NewCVCleaner(cfg.CleanupProvider, deepseekLLM, ollamaLLM)
	if err != nil {
		return err
	}

	svc := users.NewWithAuth(dbase, cleaner, ollamaLLM, cfg.JWTSecret, time.Hour)

	userID, created, err := ensureUser(ctx, dbase, svc, opts.user)
	if err != nil {
		return err
	}

	if created {
		fmt.Printf("created user: %s (%s)\n", opts.user.Email, userID)
	} else {
		fmt.Printf("using existing user: %s (%s)\n", opts.user.Email, userID)
	}

	cvs, err := svc.ListUserCVs(ctx, userID)
	if err != nil {
		return err
	}
	if len(cvs) > 0 && !opts.force {
		fmt.Printf("user already has %d CV(s); skipping processing. pass -force to process again\n", len(cvs))
		return nil
	}

	file, err := os.Open(opts.cvPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	runCtx, endRun := telemetry.StartRun(ctx, "seed.process_cv")
	err = svc.ProcessCV(runCtx, userID, file)
	endRun(err)
	if err != nil {
		return err
	}

	fmt.Printf("processed CV for user %s from %s\n", userID, opts.cvPath)
	return nil
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
