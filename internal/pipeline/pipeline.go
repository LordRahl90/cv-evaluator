package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/matcher"
	jobstorage "cv-evaluator/internal/storage/jobs"
	"cv-evaluator/internal/telemetry"

	"github.com/pgvector/pgvector-go"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

// Defaults for Config.
const (
	DefaultMaxPages          = 1
	DefaultSearchConcurrency = 2
	DefaultEmbedWorkers      = 4
	DefaultEvaluateWorkers   = 2
	DefaultMaxAttempts       = 5
	DefaultBaseBackoff       = 30 * time.Second
	DefaultMaxBackoff        = time.Hour
	DefaultPortalAttempts    = 3
	DefaultPortalBackoff     = 5 * time.Second
	DefaultPollInterval      = 2 * time.Second
	DefaultSchedulerInterval = time.Minute
	DefaultSearchBatch       = 100
	DefaultRunTimeout        = 30 * time.Minute
	DefaultNoCVRetry         = time.Hour
	DefaultShutdownGrace     = 30 * time.Second
	DefaultStaleTimeout      = 5 * time.Minute
	DefaultModel             = "deepseek-chat"
	// DefaultPromptVersion identifies the match prompt and criteria list
	// recorded on each evaluation. Bump it when either changes.
	DefaultPromptVersion = "jobmatch.tpl/v3"
)

// Config wires a Pipeline. Zero values take the defaults above.
type Config struct {
	DB        *gorm.DB
	Searches  SearchStore
	Fetcher   Fetcher
	Embedder  Embedder
	Evaluator Evaluator
	// CVs picks the CV to evaluate against. Defaults to the user's most
	// recent CV.
	CVs CVSelector
	// RateLimit configures per-portal rate limiting of Fetcher calls.
	RateLimit RateLimitOptions
	// MaxPages is how many result pages are fetched per portal.
	MaxPages int

	// SearchConcurrency is how many searches run at once; each runs all
	// of its portals concurrently.
	SearchConcurrency int
	// EmbedWorkers and EvaluateWorkers are how many embed and evaluate
	// goroutines run. Each claims one row at a time; no connection is held
	// while the slow work runs, so the pool size does not need to match.
	// EvaluateWorkers is the cap on concurrent LLM evaluations in this
	// process.
	EmbedWorkers    int
	EvaluateWorkers int
	// StaleTimeout is how long a row may stay processing before another
	// worker reclaims it (its worker is assumed dead). Keep it well above
	// the HTTP client timeouts of the slow steps.
	StaleTimeout time.Duration

	// MaxAttempts is how many times a job or search result is tried before
	// it is marked failed. Retries wait BaseBackoff, doubling each attempt,
	// up to MaxBackoff.
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	// PortalAttempts and PortalBackoff bound the in-process retries of one
	// portal search within a run.
	PortalAttempts int
	PortalBackoff  time.Duration

	// PollInterval is how long an idle loop waits before claiming again.
	PollInterval time.Duration
	// SchedulerInterval is how often due searches are looked for, and
	// SearchBatch how many are read per tick.
	SchedulerInterval time.Duration
	SearchBatch       int
	// RunTimeout is the lease taken on a search run (see
	// SearchStore.ClaimRun): if this process dies mid-run, the search is
	// due again after it.
	RunTimeout time.Duration
	// NoCVRetry is how long a result waits before being looked at again
	// when the user has no CV yet.
	NoCVRetry time.Duration
	// ShutdownGrace is how long in-flight steps may run after Run's
	// context is cancelled. After it they are cancelled and their rows are
	// released back to their ready state, without using up an attempt.
	ShutdownGrace time.Duration

	// Model and PromptVersion are recorded on each evaluation.
	Model         string
	PromptVersion string
	// Observer is told about every step and scheduler tick. Defaults to
	// NopObserver.
	Observer RunObserver
	// Now returns the current time. Tests use it to control the clock.
	Now func() time.Time
}

func (c Config) withDefaults() Config {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	defDur := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&c.MaxPages, DefaultMaxPages)
	def(&c.SearchConcurrency, DefaultSearchConcurrency)
	def(&c.EmbedWorkers, DefaultEmbedWorkers)
	def(&c.EvaluateWorkers, DefaultEvaluateWorkers)
	def(&c.MaxAttempts, DefaultMaxAttempts)
	def(&c.PortalAttempts, DefaultPortalAttempts)
	def(&c.SearchBatch, DefaultSearchBatch)
	defDur(&c.BaseBackoff, DefaultBaseBackoff)
	defDur(&c.MaxBackoff, DefaultMaxBackoff)
	defDur(&c.PortalBackoff, DefaultPortalBackoff)
	defDur(&c.PollInterval, DefaultPollInterval)
	defDur(&c.SchedulerInterval, DefaultSchedulerInterval)
	defDur(&c.RunTimeout, DefaultRunTimeout)
	defDur(&c.NoCVRetry, DefaultNoCVRetry)
	defDur(&c.ShutdownGrace, DefaultShutdownGrace)
	defDur(&c.StaleTimeout, DefaultStaleTimeout)
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.PromptVersion == "" {
		c.PromptVersion = DefaultPromptVersion
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	c.Observer = observerOrNop(c.Observer)
	return c
}

// Pipeline runs the scheduler and the embed and evaluate loops.
type Pipeline struct {
	cfg     Config
	db      *gorm.DB
	fetcher Fetcher
	jobs    *jobstorage.Service
}

// New validates cfg and returns a pipeline.
func New(cfg Config) (*Pipeline, error) {
	var missing []string
	if cfg.DB == nil {
		missing = append(missing, "DB")
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

	cfg = cfg.withDefaults()
	if cfg.CVs == nil {
		cfg.CVs = NewLatestCVSelector(cfg.DB)
	}
	return &Pipeline{
		cfg:     cfg,
		db:      cfg.DB,
		fetcher: &limitedFetcher{Fetcher: cfg.Fetcher, limiter: NewRateLimiter(cfg.RateLimit)},
		jobs:    jobstorage.NewService(cfg.DB),
	}, nil
}

func (p *Pipeline) now() time.Time {
	return p.cfg.Now().UTC()
}

// backoff returns the delay after the given (1-based) failed attempt.
func (p *Pipeline) backoff(attempt int) time.Duration {
	delay := p.cfg.BaseBackoff
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= p.cfg.MaxBackoff {
			return p.cfg.MaxBackoff
		}
	}
	return min(delay, p.cfg.MaxBackoff)
}

// Run starts the scheduler and the worker loops and blocks until ctx is
// cancelled and they have stopped. Steps in flight when ctx is cancelled
// get ShutdownGrace to finish.
func (p *Pipeline) Run(ctx context.Context) {
	// Steps run on a context that outlives ctx by ShutdownGrace.
	work, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()
	stop := context.AfterFunc(ctx, func() {
		time.AfterFunc(p.cfg.ShutdownGrace, cancelWork)
	})
	defer stop()

	var wg sync.WaitGroup
	wg.Go(func() {
		p.loop(ctx, p.cfg.SchedulerInterval, func() (bool, error) {
			n, err := p.Tick(work)
			return n > 0, err
		})
	})
	wg.Go(func() { p.pool(ctx, p.cfg.EmbedWorkers, func() (bool, error) { return p.EmbedOne(work) }) })
	wg.Go(func() { p.pool(ctx, p.cfg.EvaluateWorkers, func() (bool, error) { return p.EvaluateOne(work) }) })
	wg.Wait()
}

// pool runs n loops of step until ctx is cancelled.
func (p *Pipeline) pool(ctx context.Context, n int, step func() (bool, error)) {
	var g errgroup.Group
	g.SetLimit(n)
	for range n {
		g.Go(func() error {
			p.loop(ctx, p.cfg.PollInterval, step)
			return nil
		})
	}
	_ = g.Wait()
}

// loop calls step until ctx is cancelled. It goes straight on while step
// finds work and waits idle after an empty or failed pass.
func (p *Pipeline) loop(ctx context.Context, idle time.Duration, step func() (bool, error)) {
	for ctx.Err() == nil {
		worked, err := step()
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "pipeline step failed", "error", err)
		}
		if worked && err == nil {
			continue
		}
		_ = sleepCtx(ctx, idle)
	}
}

// --- searches --------------------------------------------------------------

// Tick runs every due search once and returns how many it ran. Each search
// is claimed atomically first (SearchStore.ClaimRun), so with several
// worker processes each run happens once. A claimed search runs all portals
// concurrently; a portal that keeps failing is logged and does not affect
// the others. The search is then marked run (next run from its frequency).
func (p *Pipeline) Tick(ctx context.Context) (int, error) {
	var ran int
	err := observe(ctx, p.cfg.Observer, KindScheduleTick, func(ctx context.Context) error {
		var err error
		ran, err = p.tick(ctx)
		return err
	})
	return ran, err
}

func (p *Pipeline) tick(ctx context.Context) (int, error) {
	providers := p.fetcher.Providers()
	if len(providers) == 0 {
		slog.DebugContext(ctx, "scheduler: no job providers configured; not running searches")
		return 0, nil
	}

	due, err := p.cfg.Searches.DueSearches(ctx, p.now(), p.cfg.SearchBatch)
	if err != nil {
		return 0, fmt.Errorf("scheduler: load due searches: %w", err)
	}

	// A plain errgroup (no shared context): one search failing must not
	// cancel the others. Wait returns the first error.
	var (
		g   errgroup.Group
		ran atomic.Int32
	)
	g.SetLimit(p.cfg.SearchConcurrency)
	for _, search := range due {
		g.Go(func() error {
			ok, err := p.runSearch(ctx, &search, providers)
			if ok {
				ran.Add(1)
			}
			if err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "search run failed", "search_id", search.ID.String(), "error", err)
			}
			return err
		})
	}
	err = g.Wait()
	return int(ran.Load()), err
}

// runSearch claims and runs one search. It reports whether it ran it.
func (p *Pipeline) runSearch(ctx context.Context, search *Search, providers []string) (_ bool, err error) {
	ctx, end := telemetry.Start(ctx, "pipeline.Pipeline.runSearch", telemetry.ID(telemetry.AttrJobSearchID, search.ID), telemetry.ID(telemetry.AttrUserID, search.UserID))
	defer end(&err)
	if !search.Enabled {
		return false, nil
	}
	started := p.now()
	claimed, err := p.cfg.Searches.ClaimRun(ctx, search.ID, started, started.Add(p.cfg.RunTimeout))
	if err != nil {
		return false, fmt.Errorf("claim search %s: %w", search.ID, err)
	}
	if !claimed {
		return false, nil // another worker took this run
	}

	log := slog.With("search_id", search.ID.String())
	var wg sync.WaitGroup
	for _, provider := range providers {
		wg.Go(func() {
			if err := p.runPortal(ctx, search, provider); err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "portal failed for search", "provider", provider, "error", err)
			}
		})
	}
	wg.Wait()

	if ctx.Err() != nil {
		// Shutting down: leave the lease to expire so the run is retried.
		return true, ctx.Err()
	}
	next := NextRunAfter(search.Frequency, started)
	if err := p.cfg.Searches.MarkRun(ctx, search.ID, started, next); err != nil {
		return true, fmt.Errorf("mark search %s run: %w", search.ID, err)
	}
	log.InfoContext(ctx, "search ran", "providers", providers, "next_run_at", next)
	return true, nil
}

// runPortal searches one portal, retrying a few times with backoff, and
// stores what it finds: each posting as a global job and a search result.
func (p *Pipeline) runPortal(ctx context.Context, search *Search, provider string) error {
	return observe(ctx, p.cfg.Observer, KindRunSearch, func(ctx context.Context) error {
		telemetry.SetAttributes(ctx, attribute.String(telemetry.AttrProvider, provider), telemetry.ID(telemetry.AttrJobSearchID, search.ID))
		criteria := Criteria{Query: search.Query, Location: search.Location, MaxPages: p.cfg.MaxPages}

		var (
			posts []entities.JobPost
			err   error
		)
		for attempt := 1; ; attempt++ {
			posts, err = p.fetcher.Search(ctx, provider, criteria)
			if err == nil {
				break
			}
			// Handled here: retried, then logged and skipped by runSearch.
			telemetry.RecordRetry(ctx, err, attempt)
			if attempt >= p.cfg.PortalAttempts || ctx.Err() != nil {
				return fmt.Errorf("search %s after %d attempts: %w", provider, attempt, err)
			}
			if err := sleepCtx(ctx, p.cfg.PortalBackoff<<(attempt-1)); err != nil {
				return err
			}
		}

		created, err := p.storePostings(ctx, search, provider, posts)
		if err != nil {
			return err
		}
		slog.InfoContext(ctx, "portal searched", "search_id", search.ID.String(), "provider", provider,
			"postings", len(posts), "new_jobs", created)
		return nil
	})
}

// storePostings stores each posting a portal returned as a global job and
// a search result, and returns how many jobs were new.
func (p *Pipeline) storePostings(ctx context.Context, search *Search, provider string, posts []entities.JobPost) (_ int, err error) {
	ctx, end := telemetry.Start(ctx, "pipeline.Pipeline.storePostings", telemetry.Count("postings", len(posts)))
	defer end(&err)
	var created int
	for i := range posts {
		post := &posts[i]
		if strings.TrimSpace(post.ID) == "" {
			continue
		}
		post.Provider = provider
		job, isNew, err := p.jobs.Upsert(ctx, post.ToModel())
		if err != nil {
			return created, err
		}
		if isNew {
			created++
		}
		if err := upsertSearchResult(p.db.WithContext(ctx), search, job.ID); err != nil {
			return created, err
		}
	}
	telemetry.SetAttributes(ctx, telemetry.Count("jobs.created", created))
	return created, nil
}

// --- jobs ------------------------------------------------------------------

// errPermanent marks a failure that retrying cannot fix.
var errPermanent = errors.New("permanent")

// writeCtx returns a context for writing a step's outcome: it is not
// cancelled with ctx, so a result computed just before shutdown is still
// recorded, but it is bounded.
func writeCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
}

// EmbedOne claims one job that needs work (a short statement, see
// claimJob), then with no connection held fetches its description if it
// is missing and embeds it, then writes the outcome. It reports whether
// there was a job to handle.
func (p *Pipeline) EmbedOne(ctx context.Context) (bool, error) {
	now, claimStart := p.now(), time.Now()
	job, err := claimJob(p.db.WithContext(ctx), now, now.Add(-p.cfg.StaleTimeout))
	if err != nil || job == nil {
		return false, err
	}
	ctx, end := telemetry.StartClaimed(ctx, "pipeline.Pipeline.EmbedOne", claimStart, "pipeline.claimJob",
		telemetry.ID(telemetry.AttrJobID, job.ID), attribute.String(telemetry.AttrProvider, job.Provider), telemetry.Count(telemetry.AttrAttempt, job.Attempts))
	defer end(nil)

	var embedding []float32
	fetched := false
	stepErr := observe(ctx, p.cfg.Observer, KindEmbedJob, func(ctx context.Context) error {
		if strings.TrimSpace(job.Detail) == "" {
			details, err := p.fetcher.Details(ctx, job.Provider, jobPost(job))
			if err != nil {
				return fmt.Errorf("fetch details: %w", err)
			}
			if details == nil || strings.TrimSpace(details.Details) == "" {
				return fmt.Errorf("%w: job has no description", errPermanent)
			}
			job.Detail = details.Details
			fetched = true
		}
		var err error
		embedding, err = p.cfg.Embedder.CreateEmbedding(ctx, job.Detail)
		if err != nil {
			return fmt.Errorf("embed: %w", err)
		}
		return nil
	})

	wctx, cancel := writeCtx(ctx)
	defer cancel()
	db := p.db.WithContext(wctx)

	updates := map[string]any{"claimed_at": nil}
	if fetched {
		// Keep a fetched description even if embedding failed.
		updates["detail"] = job.Detail
		updates["visited"] = true
	}
	ready := models.JobStatusNew
	if job.Detail != "" {
		ready = models.JobStatusDescribed
	}

	switch {
	case stepErr == nil:
		updates["embedding"] = vector(embedding)
		updates["status"] = models.JobStatusEmbedded
		updates["next_attempt_at"] = nil
		updates["last_error"] = ""
	case ctx.Err() != nil:
		// Shutting down: release the job without using up the attempt.
		updates["status"] = ready
		updates["attempts"] = job.Attempts - 1
	default:
		// Handled here: the job is retried later or marked failed.
		telemetry.RecordError(ctx, stepErr)
		p.applyFailure(updates, "status", ready, models.JobStatusFailed, job.Attempts, stepErr)
		slog.WarnContext(ctx, "job step failed", "job_id", job.ID.String(), "provider", job.Provider,
			"attempt", job.Attempts, "error", stepErr)
	}
	if err := db.Model(&models.Job{}).Where("id = ?", job.ID).Updates(updates).Error; err != nil {
		return true, fmt.Errorf("record job %s: %w", job.ID, err)
	}
	return true, nil
}

// applyFailure sets the retry state for a failed attempt in updates: back
// to ready with an exponential backoff, or failed once attempts reach
// MaxAttempts or the error is permanent. attempts already counts this one.
func (p *Pipeline) applyFailure(updates map[string]any, statusCol, ready, failed string, attempts int, cause error) {
	updates["last_error"] = cause.Error()
	if errors.Is(cause, errPermanent) || attempts >= p.cfg.MaxAttempts {
		updates[statusCol] = failed
		updates["next_attempt_at"] = nil
		return
	}
	updates[statusCol] = ready
	updates["next_attempt_at"] = p.now().Add(p.backoff(attempts))
}

// --- evaluations -----------------------------------------------------------

// evaluationOutcome is what an evaluation attempt produced, to be written
// once the slow work is done.
type evaluationOutcome struct {
	cv       *models.CV
	resp     *matcher.Response
	raw      string
	matchErr error
}

// EvaluateOne claims one search result whose job is embedded and that
// waits for evaluation (a short statement, see claimResult), then with no
// transaction held evaluates it against the user's CV, then writes the
// evaluation, its scores and the result's state in one short transaction.
// It reports whether there was a result to handle. If the result was
// deleted meanwhile (the user deleted their account), nothing is written.
func (p *Pipeline) EvaluateOne(ctx context.Context) (bool, error) {
	now, claimStart := p.now(), time.Now()
	result, err := claimResult(p.db.WithContext(ctx), now, now.Add(-p.cfg.StaleTimeout))
	if err != nil || result == nil {
		return false, err
	}
	ctx, end := telemetry.StartClaimed(ctx, "pipeline.Pipeline.EvaluateOne", claimStart, "pipeline.claimResult",
		telemetry.ID(telemetry.AttrResultID, result.ID), telemetry.ID(telemetry.AttrUserID, result.UserID), telemetry.Count(telemetry.AttrAttempt, result.Attempts))
	defer end(nil)
	log := slog.With("search_result_id", result.ID.String())

	var out evaluationOutcome
	stepErr := observe(ctx, p.cfg.Observer, KindEvaluateJob, func(ctx context.Context) error {
		var job models.Job
		if err := p.db.WithContext(ctx).Where("id = ?", result.JobID).Take(&job).Error; err != nil {
			return fmt.Errorf("load job: %w", err)
		}
		cv, err := p.cfg.CVs.SelectCV(ctx, result.UserID)
		if err != nil || cv == nil {
			return err
		}
		out.cv = cv
		out.resp, out.raw, out.matchErr = p.cfg.Evaluator.MatchJob(ctx, cv.ID, &job)
		if out.matchErr != nil {
			return fmt.Errorf("evaluate: %w", out.matchErr)
		}
		return nil
	})

	wctx, cancel := writeCtx(ctx)
	defer cancel()
	updates := map[string]any{"claimed_at": nil}

	switch {
	case ctx.Err() != nil && (stepErr != nil || out.resp == nil):
		// Shutting down: release the result without using up the attempt.
		updates["evaluation_status"] = models.EvaluationPending
		updates["attempts"] = result.Attempts - 1
	case stepErr == nil && out.cv == nil:
		log.InfoContext(ctx, "user has no CV; skipping evaluation for now",
			"user_id", result.UserID.String(), "retry_in", p.cfg.NoCVRetry)
		updates["evaluation_status"] = models.EvaluationPending
		updates["attempts"] = result.Attempts - 1
		updates["next_attempt_at"] = p.now().Add(p.cfg.NoCVRetry)
		updates["last_error"] = "user has no CV"
	case stepErr == nil:
		updates["evaluation_status"] = models.EvaluationDone
		updates["next_attempt_at"] = nil
		updates["last_error"] = ""
	default:
		// Handled here: the result is retried later or marked failed.
		telemetry.RecordError(ctx, stepErr)
		p.applyFailure(updates, "evaluation_status", models.EvaluationPending, models.EvaluationFailed,
			result.Attempts, stepErr)
		log.WarnContext(ctx, "evaluation failed", "attempt", result.Attempts, "error", stepErr)
	}

	gone, err := p.writeEvaluation(wctx, result, &out, updates)
	if err != nil {
		return true, fmt.Errorf("record search result %s: %w", result.ID, err)
	}
	if gone {
		log.InfoContext(ctx, "search result deleted while it was evaluated; outcome dropped")
		return true, nil
	}
	if out.resp != nil {
		log.InfoContext(ctx, "job evaluated", "cv_id", out.cv.ID.String(), "overall_score", out.resp.OverallScore)
	}
	return true, nil
}

// writeEvaluation writes the outcome of evaluating result in one short
// transaction: the evaluation and its scores (when there is one) and the
// result's new state. It reports gone, writing nothing, when the result was
// deleted meanwhile.
func (p *Pipeline) writeEvaluation(ctx context.Context, result *models.SearchResult, out *evaluationOutcome, updates map[string]any) (gone bool, err error) {
	ctx, end := telemetry.Start(ctx, "pipeline.Pipeline.writeEvaluation", telemetry.ID(telemetry.AttrResultID, result.ID))
	defer end(&err)
	if status, ok := updates["evaluation_status"].(string); ok {
		telemetry.SetAttributes(ctx, attribute.String(telemetry.AttrStatus, status))
	}
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		exists, err := lockSearchResult(tx, result.ID)
		if err != nil {
			return err
		}
		if !exists {
			gone = true
			return nil
		}
		if out.cv != nil && (out.resp != nil || out.matchErr != nil) {
			eval := p.evaluationRow(result, out)
			if err := saveEvaluation(tx, eval, breakdown(out.resp)); err != nil {
				return err
			}
			telemetry.SetAttributes(ctx, telemetry.ID("evaluation.id", eval.ID), telemetry.Count("criteria.count", len(breakdown(out.resp))))
			// The evaluation just written is the result's current one; its
			// score is copied only when it completed.
			updates["evaluation_id"] = eval.ID
			updates["overall_score"] = nil
			if eval.Status == models.StatusCompleted {
				updates["overall_score"] = eval.OverallScore
			}
		}
		return tx.Model(&models.SearchResult{}).Where("id = ?", result.ID).Updates(updates).Error
	})
	return gone, err
}

func breakdown(resp *matcher.Response) []matcher.CriterionScore {
	if resp == nil {
		return nil
	}
	return resp.Breakdown
}

// evaluationRow builds the evaluation to store: the parsed response, or the
// failure with whatever raw output the LLM gave.
func (p *Pipeline) evaluationRow(result *models.SearchResult, out *evaluationOutcome) *models.Evaluation {
	eval := &models.Evaluation{
		SearchResultID: result.ID,
		CVID:           out.cv.ID,
		RawResponse:    out.raw,
		Model:          p.cfg.Model,
		PromptVersion:  p.cfg.PromptVersion,
		Strengths:      models.StringList{},
		MissingSkills:  models.StringList{},
	}
	if out.resp == nil {
		eval.Status = models.StatusFailed
		if out.matchErr != nil {
			eval.Error = out.matchErr.Error()
		}
		return eval
	}
	eval.Status = models.StatusCompleted
	eval.OverallScore = out.resp.OverallScore
	eval.Summary = out.resp.Summary
	if out.resp.Strengths != nil {
		eval.Strengths = out.resp.Strengths
	}
	if out.resp.MissingSkills != nil {
		eval.MissingSkills = out.resp.MissingSkills
	}
	return eval
}

func vector(v []float32) *pgvector.Vector {
	vec := pgvector.NewVector(v)
	return &vec
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
