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
	"cv-evaluator/internal/providers"
	"cv-evaluator/internal/servers"
	apikeyhttp "cv-evaluator/internal/servers/apikeys"
	authhttp "cv-evaluator/internal/servers/auth"
	"cv-evaluator/internal/servers/cv"
	jobsearchhttp "cv-evaluator/internal/servers/jobsearches"
	"cv-evaluator/internal/servers/match"
	"cv-evaluator/internal/servers/middleware"
	portalhttp "cv-evaluator/internal/servers/portalsuggestions"
	apikeysvc "cv-evaluator/internal/services/apikeys"
	jobsearchsvc "cv-evaluator/internal/services/jobsearches"
	"cv-evaluator/internal/services/matcher"
	portalsvc "cv-evaluator/internal/services/portalsuggestions"
	"cv-evaluator/internal/services/users"
	"cv-evaluator/internal/telemetry"

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

	shutdownTelemetry, err := telemetry.Setup(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer telemetry.Flush(shutdownTelemetry)

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
	deepseekLLM := deepseek.New(deepseek.NewClient(cfg.DeepSeekAPIKey))
	cleaner, err := llm.NewCVCleaner(cfg.CleanupProvider, deepseekLLM, ollamaLLM)
	if err != nil {
		return err
	}

	usersService := users.NewWithAuth(dbase, cleaner, ollamaLLM, cfg.JWTSecret, 0)
	matchService := matcher.New(dbase, deepseekLLM)
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

	// Job searches run against every supported portal; users can suggest more.
	// Pass providers.WithTransport(rt) to Default to instrument scraper requests.
	portals := providers.Default(providers.WithTransport(telemetry.NewJobBoardTransport(nil)))
	jobsearchhttp.New(router, jobsearchsvc.New(dbase), portals, jwtSecret, apiKeyVerifier)
	portalhttp.New(router, portalsvc.New(dbase, portalWebsites(portals)...), jwtSecret, apiKeyVerifier)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("configuration loaded", "environment", cfg.Environment, "cv_cleanup_provider", cfg.CleanupProvider)
	return server.Start(ctx)
}

// portalWebsites returns the websites of the supported portals, so users
// cannot suggest a portal that is already searched.
func portalWebsites(r *providers.Registry) []string {
	var sites []string
	for _, name := range r.Names() {
		p, _ := r.Get(name)
		if d, ok := p.(providers.Describer); ok {
			sites = append(sites, d.Info().Website)
		}
	}
	return sites
}
