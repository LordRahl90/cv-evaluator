package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/queue"
	jobstorage "cv-evaluator/internal/storage/jobs"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Defaults for Config.
const (
	DefaultMaxPages                 = 1
	DefaultMaxConcurrentEvaluations = 2
	DefaultModel                    = "deepseek-chat"
	DefaultPromptVersion            = "jobmatch.tpl/v1"
)

// RunSearchPayload is the payload of a run_search task: one search against
// one portal.
type RunSearchPayload struct {
	SearchID ulid.ULID `json:"search_id"`
	Provider string    `json:"provider"`
}

// EmbedJobPayload is the payload of an embed_job task.
type EmbedJobPayload struct {
	JobID ulid.ULID `json:"job_id"`
}

// EvaluateJobPayload is the payload of an evaluate_job task.
type EvaluateJobPayload struct {
	SearchResultID ulid.ULID `json:"search_result_id"`
}

// Unique task keys. While a task with a key is queued or running, enqueueing
// the same key again is a no-op.
func runSearchKey(searchID ulid.ULID, provider string) string {
	return fmt.Sprintf("%s:%s:%s", queue.KindRunSearch, searchID, provider)
}

func embedJobKey(jobID ulid.ULID) string {
	return fmt.Sprintf("%s:%s", queue.KindEmbedJob, jobID)
}

func evaluateJobKey(resultID ulid.ULID) string {
	return fmt.Sprintf("%s:%s", queue.KindEvaluateJob, resultID)
}

// Config wires a Pipeline.
type Config struct {
	DB        *gorm.DB
	Queue     *queue.Queue
	Searches  SearchStore
	Fetcher   Fetcher
	Embedder  Embedder
	Evaluator Evaluator
	// CVs picks the CV to evaluate against. Defaults to the user's most
	// recent CV.
	CVs CVSelector
	// RateLimit configures per-portal rate limiting of Fetcher calls.
	RateLimit RateLimitOptions
	// MaxPages is how many result pages are fetched per portal. Default 1.
	MaxPages int
	// MaxConcurrentEvaluations caps LLM evaluations running at once in this
	// process, whatever the worker concurrency. Default 2.
	MaxConcurrentEvaluations int
	// Model and PromptVersion are recorded on each evaluation.
	Model         string
	PromptVersion string
	// Observer is told about every handler run. Defaults to NopObserver.
	Observer RunObserver
}

// Pipeline holds the task handlers.
type Pipeline struct {
	queue     *queue.Queue
	searches  SearchStore
	fetcher   Fetcher
	embedder  Embedder
	evaluator Evaluator
	cvs       CVSelector
	jobs      *jobstorage.Service
	store     *store

	maxPages      int
	evalSlots     chan struct{}
	model         string
	promptVersion string
	observer      RunObserver
}

// New validates cfg and returns a pipeline.
func New(cfg Config) (*Pipeline, error) {
	var missing []string
	if cfg.DB == nil {
		missing = append(missing, "DB")
	}
	if cfg.Queue == nil {
		missing = append(missing, "Queue")
	}
	if cfg.Searches == nil {
		missing = append(missing, "Searches")
	}
	if cfg.Fetcher == nil {
		missing = append(missing, "Fetcher")
	}
	if cfg.Embedder == nil {
		missing = append(missing, "Embedder")
	}
	if cfg.Evaluator == nil {
		missing = append(missing, "Evaluator")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("pipeline: missing %s", strings.Join(missing, ", "))
	}

	if cfg.CVs == nil {
		cfg.CVs = NewLatestCVSelector(cfg.DB)
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = DefaultMaxPages
	}
	if cfg.MaxConcurrentEvaluations <= 0 {
		cfg.MaxConcurrentEvaluations = DefaultMaxConcurrentEvaluations
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.PromptVersion == "" {
		cfg.PromptVersion = DefaultPromptVersion
	}

	return &Pipeline{
		queue:         cfg.Queue,
		searches:      cfg.Searches,
		fetcher:       &limitedFetcher{Fetcher: cfg.Fetcher, limiter: NewRateLimiter(cfg.RateLimit)},
		embedder:      cfg.Embedder,
		evaluator:     cfg.Evaluator,
		cvs:           cfg.CVs,
		jobs:          jobstorage.NewService(cfg.DB),
		store:         &store{db: cfg.DB},
		maxPages:      cfg.MaxPages,
		evalSlots:     make(chan struct{}, cfg.MaxConcurrentEvaluations),
		model:         cfg.Model,
		promptVersion: cfg.PromptVersion,
		observer:      observerOrNop(cfg.Observer),
	}, nil
}

// Handlers returns the queue handlers for the pipeline's task kinds.
func (p *Pipeline) Handlers() map[queue.Kind]queue.Handler {
	return map[queue.Kind]queue.Handler{
		queue.KindRunSearch:   observed(p.observer, queue.KindRunSearch, p.RunSearch),
		queue.KindEmbedJob:    observed(p.observer, queue.KindEmbedJob, p.EmbedJob),
		queue.KindEvaluateJob: observed(p.observer, queue.KindEvaluateJob, p.EvaluateJob),
	}
}

// RunSearch runs one search against one portal. Each posting is upserted as
// a global job and linked to the search; new jobs are queued for
// embedding, already embedded ones go straight to evaluation.
func (p *Pipeline) RunSearch(ctx context.Context, task *queue.Task) error {
	var payload RunSearchPayload
	if err := queue.Decode(task, &payload); err != nil {
		return err
	}
	log := slog.With("search_id", payload.SearchID.String(), "provider", payload.Provider)

	search, err := p.searches.GetSearch(ctx, payload.SearchID)
	if err != nil {
		return fmt.Errorf("load search: %w", err)
	}
	if search == nil || !search.Enabled {
		log.InfoContext(ctx, "search deleted or disabled; skipping")
		return nil
	}

	posts, err := p.fetcher.Search(ctx, payload.Provider, Criteria{
		Query:    search.Query,
		Location: search.Location,
		MaxPages: p.maxPages,
	})
	if err != nil {
		return fmt.Errorf("search %s: %w", payload.Provider, err)
	}

	var newJobs, toEvaluate int
	for i := range posts {
		post := &posts[i]
		if strings.TrimSpace(post.ID) == "" {
			log.WarnContext(ctx, "skipping posting without an id", "url", post.DetailsURL)
			continue
		}
		post.Provider = payload.Provider

		job, created, err := p.jobs.Upsert(ctx, post.ToModel())
		if err != nil {
			return err
		}
		if created {
			newJobs++
		}

		result, err := p.store.upsertSearchResult(ctx, search, job.ID)
		if err != nil {
			return err
		}

		if job.IsEmbedded() {
			toEvaluate++
			if err := p.enqueueEvaluation(ctx, result.ID); err != nil {
				return err
			}
			continue
		}
		if _, _, err := p.queue.Enqueue(ctx, queue.KindEmbedJob, EmbedJobPayload{JobID: job.ID},
			queue.WithUniqueKey(embedJobKey(job.ID))); err != nil {
			return err
		}
	}

	log.InfoContext(ctx, "search ran", "postings", len(posts), "new_jobs", newJobs, "queued_evaluations", toEvaluate)
	return nil
}

// EmbedJob fetches a job's description when it is missing, embeds it, and
// queues an evaluation for every search result of the job that has none.
func (p *Pipeline) EmbedJob(ctx context.Context, task *queue.Task) error {
	var payload EmbedJobPayload
	if err := queue.Decode(task, &payload); err != nil {
		return err
	}

	job, err := p.jobs.FindByID(ctx, payload.JobID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queue.Permanent(fmt.Errorf("job %s not found", payload.JobID))
	}
	if err != nil {
		return err
	}

	if !job.IsEmbedded() {
		if strings.TrimSpace(job.Detail) == "" {
			details, err := p.fetcher.Details(ctx, job.Provider, jobPost(job))
			if err != nil {
				return fmt.Errorf("fetch details for %s/%s: %w", job.Provider, job.JobID, err)
			}
			if details == nil || strings.TrimSpace(details.Details) == "" {
				return queue.Permanent(fmt.Errorf("job %s/%s has no description", job.Provider, job.JobID))
			}
			if err := p.jobs.SaveDetail(ctx, job.ID, details.Details); err != nil {
				return err
			}
			job.Detail = details.Details
		}
		if err := p.embedder.Embed(ctx, job); err != nil {
			return fmt.Errorf("embed job %s: %w", job.ID, err)
		}
	}

	results, err := p.store.unevaluatedResults(ctx, job.ID)
	if err != nil {
		return err
	}
	for _, r := range results {
		if err := p.enqueueEvaluation(ctx, r.ID); err != nil {
			return err
		}
	}
	return nil
}

// EvaluateJob evaluates a search result's job against the user's CV and
// stores the evaluation. A completed evaluation for the same CV is not
// redone. If the user has no CV the task completes without evaluating.
func (p *Pipeline) EvaluateJob(ctx context.Context, task *queue.Task) error {
	var payload EvaluateJobPayload
	if err := queue.Decode(task, &payload); err != nil {
		return err
	}
	log := slog.With("search_result_id", payload.SearchResultID.String())

	result, err := p.store.searchResult(ctx, payload.SearchResultID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		log.InfoContext(ctx, "search result gone; skipping evaluation")
		return nil
	}
	if err != nil {
		return err
	}

	job, err := p.jobs.FindByID(ctx, result.JobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", result.JobID, err)
	}
	if !job.IsEmbedded() {
		// embed_job queues this evaluation again once the job is embedded.
		log.InfoContext(ctx, "job not embedded yet; queueing embedding first", "job_id", job.ID.String())
		_, _, err := p.queue.Enqueue(ctx, queue.KindEmbedJob, EmbedJobPayload{JobID: job.ID},
			queue.WithUniqueKey(embedJobKey(job.ID)))
		return err
	}

	cv, err := p.cvs.SelectCV(ctx, result.UserID)
	if err != nil {
		return err
	}
	if cv == nil {
		log.InfoContext(ctx, "user has no CV; skipping evaluation", "user_id", result.UserID.String())
		return nil
	}

	existing, err := p.store.evaluation(ctx, result.ID, cv.ID)
	if err != nil {
		return err
	}
	if existing != nil && existing.Status == models.StatusCompleted {
		log.DebugContext(ctx, "already evaluated", "cv_id", cv.ID.String())
		return nil
	}

	release, err := p.acquireEvalSlot(ctx)
	if err != nil {
		return err
	}
	resp, raw, matchErr := p.evaluator.MatchJob(ctx, cv.ID, job)
	release()

	eval := &models.Evaluation{
		SearchResultID: result.ID,
		CVID:           cv.ID,
		RawResponse:    raw,
		Model:          p.model,
		PromptVersion:  p.promptVersion,
		Strengths:      models.StringList{},
		MissingSkills:  models.StringList{},
	}
	if matchErr != nil {
		eval.Status = models.StatusFailed
		eval.Error = matchErr.Error()
		if err := p.store.saveEvaluation(context.WithoutCancel(ctx), eval); err != nil {
			log.ErrorContext(ctx, "record failed evaluation", "error", err)
		}
		return fmt.Errorf("evaluate: %w", matchErr)
	}

	eval.Status = models.StatusCompleted
	eval.OverallScore = resp.OverallScore
	eval.SkillsScore = resp.SkillsScore
	eval.ExperienceScore = resp.ExperienceScore
	eval.SeniorityScore = resp.SeniorityScore
	eval.DomainScore = resp.DomainScore
	eval.LocationScore = resp.LocationScore
	eval.Summary = resp.Summary
	if resp.Strengths != nil {
		eval.Strengths = resp.Strengths
	}
	if resp.MissingSkills != nil {
		eval.MissingSkills = resp.MissingSkills
	}
	if err := p.store.saveEvaluation(ctx, eval); err != nil {
		return err
	}

	log.InfoContext(ctx, "job evaluated", "cv_id", cv.ID.String(), "overall_score", resp.OverallScore)
	return nil
}

func (p *Pipeline) enqueueEvaluation(ctx context.Context, resultID ulid.ULID) error {
	_, _, err := p.queue.Enqueue(ctx, queue.KindEvaluateJob, EvaluateJobPayload{SearchResultID: resultID},
		queue.WithUniqueKey(evaluateJobKey(resultID)))
	return err
}

// acquireEvalSlot waits for one of the MaxConcurrentEvaluations slots.
func (p *Pipeline) acquireEvalSlot(ctx context.Context) (func(), error) {
	select {
	case p.evalSlots <- struct{}{}:
		return func() { <-p.evalSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func jobPost(job *models.Job) *entities.JobPost {
	return &entities.JobPost{
		ID:         job.JobID,
		Provider:   job.Provider,
		Title:      job.Title,
		Company:    job.Company,
		Location:   job.Location,
		PostedAt:   job.PostedAt,
		DetailsURL: job.DetailsURL,
	}
}

// Scheduler periodically finds due searches and queues one run_search task
// per (search, portal).
type Scheduler struct {
	queue     *queue.Queue
	searches  SearchStore
	providers func() []string
	interval  time.Duration
	batchSize int
	now       func() time.Time
	observer  RunObserver
}

// SchedulerOptions configures a Scheduler.
type SchedulerOptions struct {
	// Observer is told about every tick (kind schedule_tick). Defaults to
	// NopObserver.
	Observer RunObserver
	// Interval between scans for due searches. Default 1m.
	Interval time.Duration
	// BatchSize is how many due searches are read at a time. Default 100.
	BatchSize int
	// Now returns the current time. Default time.Now.
	Now func() time.Time
}

// NewScheduler returns a scheduler that runs every search against all of
// fetcher's portals.
func NewScheduler(q *queue.Queue, searches SearchStore, fetcher Fetcher, opts SchedulerOptions) *Scheduler {
	if opts.Interval <= 0 {
		opts.Interval = time.Minute
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Scheduler{
		queue:     q,
		searches:  searches,
		providers: fetcher.Providers,
		interval:  opts.Interval,
		batchSize: opts.BatchSize,
		now:       opts.Now,
		observer:  observerOrNop(opts.Observer),
	}
}

// maxBatchesPerTick bounds one Tick in case a SearchStore keeps returning
// the same searches (e.g. MarkRun not taking effect).
const maxBatchesPerTick = 50

// Tick queues every due search once and returns how many run_search tasks
// it added. A search is marked as run (LastRunAt now, NextRunAt from its
// frequency) only after a task for each portal has been queued; if
// queueing fails part way, the search stays due and the next tick retries
// it, and the unique keys stop the portals already queued from being
// queued twice.
func (s *Scheduler) Tick(ctx context.Context) (int, error) {
	var added int
	err := observe(ctx, s.observer, KindScheduleTick, func(ctx context.Context) error {
		var err error
		added, err = s.tick(ctx)
		return err
	})
	return added, err
}

func (s *Scheduler) tick(ctx context.Context) (int, error) {
	providers := s.providers()
	if len(providers) == 0 {
		slog.WarnContext(ctx, "scheduler: no job providers configured; not running searches")
		return 0, nil
	}

	var added int
	for range maxBatchesPerTick {
		now := s.now().UTC()
		due, err := s.searches.DueSearches(ctx, now, s.batchSize)
		if err != nil {
			return added, fmt.Errorf("scheduler: load due searches: %w", err)
		}

		var errs []error
		for _, search := range due {
			n, err := s.schedule(ctx, &search, providers, now)
			added += n
			if err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return added, errors.Join(errs...)
		}
		if len(due) < s.batchSize {
			break
		}
	}
	return added, nil
}

func (s *Scheduler) schedule(ctx context.Context, search *Search, providers []string, now time.Time) (int, error) {
	if !search.Enabled {
		return 0, nil
	}

	var added int
	for _, provider := range providers {
		_, inserted, err := s.queue.Enqueue(ctx, queue.KindRunSearch,
			RunSearchPayload{SearchID: search.ID, Provider: provider},
			queue.WithUniqueKey(runSearchKey(search.ID, provider)))
		if err != nil {
			return added, fmt.Errorf("scheduler: queue search %s on %s: %w", search.ID, provider, err)
		}
		if inserted {
			added++
		}
	}

	next := NextRunAfter(search.Frequency, now)
	if err := s.searches.MarkRun(ctx, search.ID, now, next); err != nil {
		return added, fmt.Errorf("scheduler: mark search %s run: %w", search.ID, err)
	}
	slog.InfoContext(ctx, "scheduled search", "search_id", search.ID.String(), "providers", providers, "next_run_at", next)
	return added, nil
}

// Run ticks immediately and then every Interval until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if _, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "scheduler tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
