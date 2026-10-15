// Package pipeline runs saved job searches in the background: it fetches
// postings from every supported portal, stores each posting once, embeds it
// once, evaluates it against the user's CV and stores the evaluation.
//
// Work flows through the Postgres queue (internal/queue):
//
//	Scheduler ─ due search ─▶ run_search(search, provider)   one per portal
//	run_search ─ new job ───▶ embed_job(job)                 once per job
//	run_search ─ embedded ──▶ evaluate_job(search result)
//	embed_job ─ embedded ───▶ evaluate_job(each unevaluated result of the job)
//
// Every handler is idempotent: jobs are upserted on (provider, job_id),
// search results on (search, job), evaluations on (search result, CV), and
// tasks are enqueued with unique keys, so re-running any task never
// duplicates rows.
//
// The pipeline depends on consumer-side interfaces (SearchStore, Fetcher)
// rather than on the job search and provider packages directly; a thin
// adapter over those satisfies them.
package pipeline

import (
	"context"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/matcher"

	"github.com/oklog/ulid/v2"
)

// Search frequencies.
const (
	FrequencyHourly = "hourly"
	FrequencyDaily  = "daily"
	FrequencyWeekly = "weekly"
)

// Search is a user's saved job search. Every search runs against all
// portals the Fetcher supports.
type Search struct {
	ID        ulid.ULID
	UserID    ulid.ULID
	Name      string
	Query     string
	Location  string
	Frequency string
	Enabled   bool
	LastRunAt *time.Time
	NextRunAt *time.Time
}

// Criteria is what a portal is searched with.
type Criteria struct {
	Query    string
	Location string
	MaxPages int
}

// SearchStore reads saved searches and records when they ran.
type SearchStore interface {
	// DueSearches returns up to limit enabled searches whose NextRunAt is
	// nil or not after now.
	DueSearches(ctx context.Context, now time.Time, limit int) ([]Search, error)
	// GetSearch returns the search, or nil and no error if it no longer
	// exists.
	GetSearch(ctx context.Context, id ulid.ULID) (*Search, error)
	// MarkRun records that the search was run at ranAt and is next due at
	// next.
	MarkRun(ctx context.Context, id ulid.ULID, ranAt, next time.Time) error
}

// Fetcher searches job portals by name.
type Fetcher interface {
	// Providers lists the portals every search runs against.
	Providers() []string
	Search(ctx context.Context, provider string, c Criteria) ([]entities.JobPost, error)
	Details(ctx context.Context, provider string, job *entities.JobPost) (*entities.JobDetails, error)
}

// CVSelector picks the CV a user's jobs are evaluated against.
type CVSelector interface {
	// SelectCV returns the CV to evaluate with, or nil and no error when
	// the user has none.
	SelectCV(ctx context.Context, userID ulid.ULID) (*models.CV, error)
}

// Embedder embeds a job's description and stores it on the job. It does
// nothing for a job that is already embedded. services/jobs.Service
// satisfies it.
type Embedder interface {
	Embed(ctx context.Context, job *models.Job) error
}

// Evaluator matches a CV against a stored job and returns the parsed
// response and the raw LLM output. services/matcher.Service satisfies it.
type Evaluator interface {
	MatchJob(ctx context.Context, cvID ulid.ULID, job *models.Job) (*matcher.Response, string, error)
}

// NextRunAfter returns when a search with the given frequency is next due
// after from. An unknown frequency is treated as daily.
func NextRunAfter(frequency string, from time.Time) time.Time {
	switch frequency {
	case FrequencyHourly:
		return from.Add(time.Hour)
	case FrequencyWeekly:
		return from.Add(7 * 24 * time.Hour)
	default:
		return from.Add(24 * time.Hour)
	}
}
