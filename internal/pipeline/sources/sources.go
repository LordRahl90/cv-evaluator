// Package sources adapts the saved job searches (models.JobSearch) and the
// portal registry (internal/providers) to the pipeline's SearchStore and
// Fetcher interfaces.
package sources

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/pipeline"
	"cv-evaluator/internal/providers"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

var (
	_ pipeline.SearchStore = (*Store)(nil)
	_ pipeline.Fetcher     = (*Fetcher)(nil)
)

// ErrUnknownProvider is returned when a task names a portal the registry
// doesn't have, for example after a portal was removed.
var ErrUnknownProvider = errors.New("sources: unknown provider")

// Store reads saved job searches from the database.
type Store struct {
	db *gorm.DB
}

// NewStore returns a SearchStore over models.JobSearch.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// DueSearches returns enabled searches whose next run is due, oldest first.
func (s *Store) DueSearches(ctx context.Context, now time.Time, limit int) ([]pipeline.Search, error) {
	var rows []models.JobSearch
	err := s.db.WithContext(ctx).
		Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	res := make([]pipeline.Search, 0, len(rows))
	for i := range rows {
		res = append(res, toSearch(&rows[i]))
	}
	return res, nil
}

// ClaimRun takes ownership of one run of the search by pushing next_run_at
// out to until, but only while the search is still enabled and due. Of
// several workers racing for the same run, exactly one sees true.
func (s *Store) ClaimRun(ctx context.Context, id ulid.ULID, now, until time.Time) (bool, error) {
	res := s.db.WithContext(ctx).
		Model(&models.JobSearch{}).
		Where("id = ? AND enabled AND next_run_at IS NOT NULL AND next_run_at <= ?", id, now).
		Update("next_run_at", until)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// MarkRun records the run. A search disabled while it was running is
// left unscheduled rather than given a new next run.
func (s *Store) MarkRun(ctx context.Context, id ulid.ULID, ranAt, next time.Time) error {
	return s.db.WithContext(ctx).
		Model(&models.JobSearch{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_run_at": ranAt,
			"next_run_at": gorm.Expr("CASE WHEN enabled THEN ?::timestamptz ELSE NULL END", next),
		}).Error
}

func toSearch(j *models.JobSearch) pipeline.Search {
	return pipeline.Search{
		ID:        j.ID,
		UserID:    j.UserID,
		Name:      j.Name,
		Query:     j.Query,
		Location:  j.Location,
		Frequency: j.Frequency,
		Enabled:   j.Enabled,
		LastRunAt: j.LastRunAt,
		NextRunAt: j.NextRunAt,
	}
}

// Fetcher searches portals through the provider registry.
type Fetcher struct {
	registry *providers.Registry
}

// NewFetcher returns a Fetcher over registry.
func NewFetcher(registry *providers.Registry) *Fetcher {
	return &Fetcher{registry: registry}
}

// Providers lists every portal a search runs against.
func (f *Fetcher) Providers() []string {
	return f.registry.Names()
}

// Search searches one portal.
func (f *Fetcher) Search(ctx context.Context, provider string, c pipeline.Criteria) ([]entities.JobPost, error) {
	p, err := f.provider(provider)
	if err != nil {
		return nil, err
	}
	return p.Search(ctx, providers.Criteria(c))
}

// Details fetches a posting's full description from its portal.
func (f *Fetcher) Details(ctx context.Context, provider string, job *entities.JobPost) (*entities.JobDetails, error) {
	p, err := f.provider(provider)
	if err != nil {
		return nil, err
	}
	return p.Details(ctx, job)
}

func (f *Fetcher) provider(name string) (providers.Provider, error) {
	p, ok := f.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
	return p, nil
}
