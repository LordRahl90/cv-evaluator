package pipeline

import (
	"context"
	"errors"
	"time"

	"cv-evaluator/internal/entities"

	"github.com/oklog/ulid/v2"
)

// ErrNoSources is returned by NoSources' fetch methods.
var ErrNoSources = errors.New("pipeline: no job search sources configured")

// NoSources is a placeholder SearchStore and Fetcher with no searches and
// no portals. With it the scheduler finds nothing to run.
type NoSources struct{}

var (
	_ SearchStore = NoSources{}
	_ Fetcher     = NoSources{}
)

func (NoSources) DueSearches(context.Context, time.Time, int) ([]Search, error) { return nil, nil }

func (NoSources) ClaimRun(context.Context, ulid.ULID, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (NoSources) MarkRun(context.Context, ulid.ULID, time.Time, time.Time) error { return nil }

func (NoSources) Providers() []string { return nil }

func (NoSources) Search(context.Context, string, Criteria) ([]entities.JobPost, error) {
	return nil, ErrNoSources
}

func (NoSources) Details(context.Context, string, *entities.JobPost) (*entities.JobDetails, error) {
	return nil, ErrNoSources
}
