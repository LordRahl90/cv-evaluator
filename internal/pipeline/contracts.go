// Package pipeline runs saved job searches in the background: it fetches
// postings from every supported portal, stores each posting once, embeds it
// once, evaluates it against the user's CV and stores the evaluation.
//
// There is no separate task queue. Progress is kept on the domain rows and
// workers claim rows in the state they need with SELECT ... FOR UPDATE SKIP
// LOCKED, inside a transaction that also records the outcome:
//
//	job search  next_run_at due ─ ClaimRun ─▶ run every portal ─▶ MarkRun
//	                              (upsert jobs, search results)
//	jobs        new ─ fetch details ─▶ described ─ embed ─▶ embedded
//	search      evaluation_status pending (job embedded) ─ evaluate ─▶ done
//	results
//
// Failed steps increment attempts and set next_attempt_at with exponential
// backoff; after MaxAttempts the row is marked failed. A crash rolls the
// transaction back, which frees the rows for another worker, so several
// worker processes can run at once. Every step is idempotent: jobs are
// upserted on (provider, job_id), search results on (search, job) and
// evaluations on (search result, CV).
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

// SearchStore reads saved searches and records their runs.
type SearchStore interface {
	// DueSearches returns up to limit searches that are enabled and have a
	// NextRunAt that is not after now (enabled AND next_run_at <= now). A
	// nil NextRunAt means the search is not scheduled.
	DueSearches(ctx context.Context, now time.Time, limit int) ([]Search, error)
	// ClaimRun atomically takes ownership of one run of the search: if it
	// is still enabled and due (next_run_at <= now), it sets next_run_at to
	// until and reports true. Only one caller can win a given run; the
	// others get false. until is a lease: if the owner dies before MarkRun,
	// the search becomes due again then.
	ClaimRun(ctx context.Context, id ulid.ULID, now, until time.Time) (bool, error)
	// MarkRun records that the search ran at ranAt and is next due at next.
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

// Embedder embeds a job description. The Ollama client satisfies it.
type Embedder interface {
	CreateEmbedding(ctx context.Context, text string) ([]float32, error)
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
