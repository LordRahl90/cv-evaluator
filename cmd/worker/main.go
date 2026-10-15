// Command worker runs the background pipeline: a scheduler that queues due
// job searches, and workers that fetch, embed and evaluate jobs.
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
	"sync"
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
	"cv-evaluator/internal/queue"
	"cv-evaluator/internal/services/jobs"
	"cv-evaluator/internal/services/matcher"

	deepseekLib "github.com/cohesion-org/deepseek-go"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	if err := run(options{}); err != nil {
		log.Fatal(err)
	}
}

// options are the worker's injectable dependencies.
type options struct {
	// Transport is used for every outbound HTTP call the worker makes (LLM
	// and embedding APIs, and the portal fetcher once wired). nil means
	// http.DefaultTransport. Wrap it to add tracing.
	Transport http.RoundTripper
	// Observer is told about every task run and scheduler tick. nil means
	// no-op.
	Observer pipeline.RunObserver
}

// sources returns the job search store and the portal fetcher. Scrapers use
// the client's transport so outbound portal requests can be instrumented.
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

	if err := migrator.Migrate(dbase); err != nil {
		return err
	}

	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	llmClient := &http.Client{Timeout: 30 * time.Minute, Transport: transport}
	fetchClient := &http.Client{Timeout: time.Minute, Transport: transport}

	ollamaLLM := ollama.New(llmClient, cfg.OllamaBaseURL)
	deepseekClient, err := deepseekLib.NewClientWithOptions(cfg.DeepSeekAPIKey, deepseekLib.WithHTTPClient(llmClient))
	if err != nil {
		return fmt.Errorf("deepseek client: %w", err)
	}
	deepseekLLM := deepseek.New(deepseekClient)

	searches, fetcher := sources(dbase, fetchClient)

	q := queue.New(dbase, queue.Options{})
	p, err := pipeline.New(pipeline.Config{
		DB:                       dbase,
		Queue:                    q,
		Searches:                 searches,
		Fetcher:                  fetcher,
		Embedder:                 jobs.New(dbase, ollamaLLM),
		Evaluator:                matcher.New(dbase, ollamaLLM, deepseekLLM),
		MaxConcurrentEvaluations: settings.maxEvaluations,
		Observer:                 opts.Observer,
	})
	if err != nil {
		return err
	}
	scheduler := pipeline.NewScheduler(q, searches, fetcher, pipeline.SchedulerOptions{
		Interval: settings.schedulerInterval,
		Observer: opts.Observer,
	})
	worker := queue.NewWorker(q, p.Handlers(), queue.WorkerOptions{
		Concurrency:   settings.concurrency,
		PollInterval:  settings.pollInterval,
		ShutdownGrace: settings.shutdownGrace,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("worker starting",
		"environment", cfg.Environment,
		"worker_id", worker.ID(),
		"concurrency", settings.concurrency,
		"max_evaluations", settings.maxEvaluations,
		"scheduler_interval", settings.schedulerInterval,
		"providers", fetcher.Providers())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scheduler.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		worker.Run(ctx)
	}()
	wg.Wait()

	slog.Info("worker stopped")
	return nil
}

type settings struct {
	concurrency       int
	maxEvaluations    int
	pollInterval      time.Duration
	schedulerInterval time.Duration
	shutdownGrace     time.Duration
}

// loadSettings reads the worker's tuning knobs from the environment.
func loadSettings() (settings, error) {
	var (
		s    settings
		errs []error
	)
	s.concurrency = envInt("WORKER_CONCURRENCY", 4, &errs)
	s.maxEvaluations = envInt("WORKER_MAX_EVALUATIONS", pipeline.DefaultMaxConcurrentEvaluations, &errs)
	s.pollInterval = envDuration("WORKER_POLL_INTERVAL", time.Second, &errs)
	s.schedulerInterval = envDuration("SCHEDULER_INTERVAL", time.Minute, &errs)
	s.shutdownGrace = envDuration("WORKER_SHUTDOWN_GRACE", 30*time.Second, &errs)
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
