// Command worker runs the background pipeline: a scheduler that runs due
// job searches, and pools of goroutines that embed and evaluate jobs.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"cv-evaluator/db"
	"cv-evaluator/internal/config"
	"cv-evaluator/internal/llm/deepseek"
	"cv-evaluator/internal/llm/ollama"
	"cv-evaluator/internal/migrator"
	"cv-evaluator/internal/pipeline"
	pipelinesources "cv-evaluator/internal/pipeline/sources"
	"cv-evaluator/internal/providers"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/telemetry"

	deepseekLib "github.com/cohesion-org/deepseek-go"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	if err := run(options{Observer: telemetry.RunObserver{}}); err != nil {
		log.Fatal(err)
	}
}

// options are the worker's injectable dependencies.
type options struct {
	// Transport is the base for every outbound HTTP call the worker makes.
	// nil means http.DefaultTransport. The worker adds telemetry itself, so
	// don't pass an instrumented transport or requests are recorded twice.
	Transport http.RoundTripper
	// Observer is told about every pipeline step and scheduler tick. nil means
	// no-op.
	Observer pipeline.RunObserver
}

// sources returns the job search store and the portal fetcher. Scrapers use
// the client's transport so outbound portal requests are instrumented.
func sources(dbase *gorm.DB, client *http.Client) (pipeline.SearchStore, pipeline.Fetcher) {
	return pipelinesources.NewStore(dbase), pipelinesources.NewFetcher(providers.Default(providers.WithTransport(client.Transport)))
}

func run(opts options) error {
	// .env is optional: in containers the settings come from the environment.
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	cfg, err := config.Load(config.WithoutJWTSecret())
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer telemetry.Flush(shutdownTelemetry)
	settings, err := loadSettings()
	if err != nil {
		return err
	}

	dbase, err := db.Open(cfg.Database)
	if err != nil {
		return err
	}
	sqlDB, err := dbase.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	// No connection is held while scraping, embedding or calling the LLM:
	// each step claims its row and writes its result in short statements,
	// so a small pool serves any worker concurrency.
	sqlDB.SetMaxOpenConns(settings.dbMaxOpenConns)
	sqlDB.SetMaxIdleConns(settings.dbMaxOpenConns)

	if err := migrator.Migrate(dbase); err != nil {
		return err
	}

	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	// Keep these well below WORKER_STALE_TIMEOUT, so a row is only
	// reclaimed when its worker is really gone. ollama.New instruments its
	// own client; DeepSeek and the portal fetcher are wrapped here, so no
	// request is recorded twice.
	llmClient := &http.Client{Timeout: llmTimeout, Transport: transport}
	fetchClient := &http.Client{Timeout: fetchTimeout, Transport: telemetry.NewJobBoardTransport(transport)}
	if settings.staleTimeout <= llmTimeout {
		return fmt.Errorf("WORKER_STALE_TIMEOUT (%s) must be longer than the LLM request timeout (%s)",
			settings.staleTimeout, llmTimeout)
	}

	ollamaLLM := ollama.New(llmClient, cfg.OllamaBaseURL)
	deepseekClient, err := deepseekLib.NewClientWithOptions(cfg.DeepSeekAPIKey, deepseekLib.WithHTTPClient(telemetry.NewHTTPClient(telemetry.PeerDeepSeek, llmClient)))
	if err != nil {
		return fmt.Errorf("deepseek client: %w", err)
	}
	deepseekLLM := deepseek.New(deepseekClient)

	searches, fetcher := sources(dbase, fetchClient)

	p, err := pipeline.New(pipeline.Config{
		DB:                dbase,
		Searches:          searches,
		Fetcher:           fetcher,
		Embedder:          ollamaLLM,
		Evaluator:         matcher.New(dbase, ollamaLLM, deepseekLLM),
		SearchConcurrency: settings.searchConcurrency,
		EmbedWorkers:      settings.embedConcurrency,
		EvaluateWorkers:   settings.evalConcurrency,
		StaleTimeout:      settings.staleTimeout,
		PollInterval:      settings.pollInterval,
		SchedulerInterval: settings.schedulerInterval,
		ShutdownGrace:     settings.shutdownGrace,
		Observer:          opts.Observer,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("worker starting",
		"environment", cfg.Environment,
		"search_concurrency", settings.searchConcurrency,
		"embed_concurrency", settings.embedConcurrency,
		"eval_concurrency", settings.evalConcurrency,
		"stale_timeout", settings.staleTimeout,
		"scheduler_interval", settings.schedulerInterval,
		"db_max_open_conns", settings.dbMaxOpenConns,
		"providers", fetcher.Providers())

	p.Run(ctx)

	slog.Info("worker stopped")
	return nil
}

// HTTP timeouts for outbound calls. WORKER_STALE_TIMEOUT must exceed them.
const (
	llmTimeout   = 2 * time.Minute
	fetchTimeout = time.Minute
)

type settings struct {
	searchConcurrency int
	embedConcurrency  int
	evalConcurrency   int
	staleTimeout      time.Duration
	pollInterval      time.Duration
	schedulerInterval time.Duration
	shutdownGrace     time.Duration
	dbMaxOpenConns    int
}

// loadSettings reads the worker's tuning knobs from the environment.
func loadSettings() (settings, error) {
	var (
		s    settings
		errs []error
	)
	s.searchConcurrency = envInt("WORKER_SEARCH_CONCURRENCY", pipeline.DefaultSearchConcurrency, &errs)
	s.embedConcurrency = envInt("WORKER_EMBED_CONCURRENCY", pipeline.DefaultEmbedWorkers, &errs)
	s.evalConcurrency = envInt("WORKER_EVAL_CONCURRENCY", pipeline.DefaultEvaluateWorkers, &errs)
	s.staleTimeout = envDuration("WORKER_STALE_TIMEOUT", pipeline.DefaultStaleTimeout, &errs)
	s.pollInterval = envDuration("WORKER_POLL_INTERVAL", pipeline.DefaultPollInterval, &errs)
	s.schedulerInterval = envDuration("SCHEDULER_INTERVAL", pipeline.DefaultSchedulerInterval, &errs)
	s.shutdownGrace = envDuration("WORKER_SHUTDOWN_GRACE", pipeline.DefaultShutdownGrace, &errs)
	s.dbMaxOpenConns = envInt("WORKER_DB_MAX_OPEN_CONNS", 10, &errs)
	return s, errors.Join(errs...)
}

func envInt(key string, fallback int, errs *[]error) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive integer, got %q", key, v))
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration, errs *[]error) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive duration like 30s, got %q", key, v))
		return fallback
	}
	return d
}
