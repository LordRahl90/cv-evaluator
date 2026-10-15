// Package jobsearches manages saved job searches: what to look for and how
// often. Every search runs against all supported job portals. This package
// only schedules runs (via NextRunAt); running them is the scheduler's job.
package jobsearches

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Length limits for user-supplied fields, in characters.
const (
	MaxNameLength     = 100
	MaxQueryLength    = 200
	MaxLocationLength = 200
)

// DefaultFrequency is used when a search is created without a frequency.
const DefaultFrequency = models.FrequencyDaily

var (
	// ErrNotFound means the search does not exist or belongs to another user.
	ErrNotFound = errors.New("job search not found")
	// ErrDisabled is returned by RunNow for a disabled search.
	ErrDisabled = errors.New("job search is disabled")
)

// ValidationError describes an invalid or missing parameter.
type ValidationError struct {
	// Param is the JSON name of the offending field, e.g. "frequency".
	Param string
	// Missing is true when the parameter was required but absent, and false
	// when it was present but invalid.
	Missing bool
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func missing(param, message string) error {
	return &ValidationError{Param: param, Missing: true, Message: message}
}

func invalid(param, message string) error {
	return &ValidationError{Param: param, Message: message}
}

// CreateParams are the fields of a new search. Name defaults to Query,
// Frequency to DefaultFrequency and Enabled to true.
type CreateParams struct {
	Name      string
	Query     string
	Location  string
	Frequency string
	Enabled   *bool
}

// UpdateParams changes the non-nil fields of a search.
type UpdateParams struct {
	Name      *string
	Query     *string
	Location  *string
	Frequency *string
	Enabled   *bool
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// Create saves a new search for userID. An enabled search is due
// immediately, so its first run happens on the scheduler's next tick.
func (s *Service) Create(ctx context.Context, userID ulid.ULID, p CreateParams) (*models.JobSearch, error) {
	rec := &models.JobSearch{
		UserID:    userID,
		Name:      strings.TrimSpace(p.Name),
		Query:     strings.TrimSpace(p.Query),
		Location:  strings.TrimSpace(p.Location),
		Frequency: strings.TrimSpace(p.Frequency),
		Enabled:   p.Enabled == nil || *p.Enabled,
	}
	if rec.Frequency == "" {
		rec.Frequency = DefaultFrequency
	}
	if err := validate(rec); err != nil {
		return nil, err
	}
	if rec.Enabled {
		now := s.now()
		rec.NextRunAt = &now
	}

	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		return nil, fmt.Errorf("create job search: %w", err)
	}
	return rec, nil
}

// List returns the caller's searches, newest first.
func (s *Service) List(ctx context.Context, userID ulid.ULID) ([]models.JobSearch, error) {
	var recs []models.JobSearch
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&recs).Error; err != nil {
		return nil, fmt.Errorf("list job searches: %w", err)
	}
	return recs, nil
}

// Get returns one of the caller's searches.
func (s *Service) Get(ctx context.Context, userID, id ulid.ULID) (*models.JobSearch, error) {
	var rec models.JobSearch
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get job search: %w", err)
	}
	return &rec, nil
}

// Update applies the non-nil fields of p. Enabling a search makes it due
// immediately, disabling it clears NextRunAt, and changing the frequency of
// a search that has run before reschedules it from its last run.
func (s *Service) Update(ctx context.Context, userID, id ulid.ULID, p UpdateParams) (*models.JobSearch, error) {
	rec, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}

	set := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	oldFrequency, wasEnabled := rec.Frequency, rec.Enabled
	set(&rec.Name, p.Name)
	set(&rec.Query, p.Query)
	set(&rec.Location, p.Location)
	set(&rec.Frequency, p.Frequency)
	if p.Enabled != nil {
		rec.Enabled = *p.Enabled
	}

	if err := validate(rec); err != nil {
		return nil, err
	}

	switch {
	case !rec.Enabled:
		rec.NextRunAt = nil
	case !wasEnabled:
		now := s.now()
		rec.NextRunAt = &now
	case rec.Frequency != oldFrequency && rec.LastRunAt != nil:
		next := rec.NextRun(*rec.LastRunAt)
		rec.NextRunAt = &next
	}

	if err := s.db.WithContext(ctx).Save(rec).Error; err != nil {
		return nil, fmt.Errorf("update job search: %w", err)
	}
	return rec, nil
}

// Delete soft-deletes one of the caller's searches.
func (s *Service) Delete(ctx context.Context, userID, id ulid.ULID) error {
	res := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&models.JobSearch{})
	if res.Error != nil {
		return fmt.Errorf("delete job search: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// RunNow makes an enabled search due now, so the scheduler runs it on its
// next tick. It returns ErrDisabled for a disabled search.
func (s *Service) RunNow(ctx context.Context, userID, id ulid.ULID) (*models.JobSearch, error) {
	rec, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !rec.Enabled {
		return nil, ErrDisabled
	}

	now := s.now()
	if err := s.db.WithContext(ctx).
		Model(rec).
		Update("next_run_at", now).Error; err != nil {
		return nil, fmt.Errorf("run job search: %w", err)
	}
	rec.NextRunAt = &now
	return rec, nil
}

// validate checks rec and fills in a default name.
func validate(rec *models.JobSearch) error {
	if rec.Query == "" {
		return missing("query", "The query parameter is required.")
	}
	if utf8.RuneCountInString(rec.Query) > MaxQueryLength {
		return invalid("query", fmt.Sprintf("The query parameter must be at most %d characters long.", MaxQueryLength))
	}
	if utf8.RuneCountInString(rec.Location) > MaxLocationLength {
		return invalid("location", fmt.Sprintf("The location parameter must be at most %d characters long.", MaxLocationLength))
	}

	if rec.Name == "" {
		rec.Name = rec.Query
	}
	if utf8.RuneCountInString(rec.Name) > MaxNameLength {
		return invalid("name", fmt.Sprintf("The name parameter must be at most %d characters long.", MaxNameLength))
	}

	if _, ok := models.FrequencyInterval(rec.Frequency); !ok {
		return invalid("frequency", "The frequency parameter must be one of: "+strings.Join(models.Frequencies, ", ")+".")
	}

	return nil
}
