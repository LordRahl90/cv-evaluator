package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cv-evaluator/db"
	"cv-evaluator/internal/config"
	"cv-evaluator/internal/llm"
	"cv-evaluator/internal/llm/deepseek"
	"cv-evaluator/internal/llm/ollama"
	"cv-evaluator/internal/migrator"
	"cv-evaluator/internal/servers"
	apikeyhttp "cv-evaluator/internal/servers/apikeys"
	authhttp "cv-evaluator/internal/servers/auth"
	"cv-evaluator/internal/servers/cv"
	"cv-evaluator/internal/servers/match"
	"cv-evaluator/internal/servers/middleware"
	apikeysvc "cv-evaluator/internal/services/apikeys"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/services/users"

	deepseekLib "github.com/cohesion-org/deepseek-go"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// .env is optional: in containers the settings come from the environment.
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if !cfg.IsDevelopment() {
		gin.SetMode(gin.ReleaseMode)
	}

	dbase, err := db.Open(cfg.Database)
	if err != nil {
		return err
	}
	sqlDB, err := dbase.DB()
	if err != nil {
		return err
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	if err := migrator.Migrate(dbase); err != nil {
		return err
	}

	ollamaLLM := ollama.New(&http.Client{Timeout: 30 * time.Minute}, cfg.OllamaBaseURL)
	deepseekLLM := deepseek.New(deepseekLib.NewClient(cfg.DeepSeekAPIKey))
	cleaner, err := llm.NewCVCleaner(cfg.CleanupProvider, deepseekLLM, ollamaLLM)
	if err != nil {
		return err
	}

	usersService := users.NewWithAuth(dbase, cleaner, ollamaLLM, cfg.JWTSecret, 0)
	matchService := matcher.New(dbase, ollamaLLM, deepseekLLM)
	apiKeyService := apikeysvc.New(dbase)
	apiKeyVerifier := apikeyhttp.NewVerifier(apiKeyService)

	server := servers.New(&servers.Config{
		Port:        cfg.Port,
		HealthCheck: sqlDB.PingContext,
	})
	router := server.Router()
	jwtSecret := []byte(cfg.JWTSecret)
	acceptAPIKeys := middleware.WithAPIKeyVerifier(apiKeyVerifier)
	authhttp.New(router, usersService, jwtSecret, apiKeyVerifier)
	apikeyhttp.New(router, apiKeyService, jwtSecret, apiKeyVerifier)
	cv.New(router, usersService, jwtSecret, acceptAPIKeys)
	match.New(router, matchService, jwtSecret, acceptAPIKeys)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("configuration loaded", "environment", cfg.Environment, "cv_cleanup_provider", cfg.CleanupProvider)
	return server.Start(ctx)
}
