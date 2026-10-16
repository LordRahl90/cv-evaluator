// Package portalsuggestions records job portals users would like searched.
// The team reviews the suggestions and builds scrapers for accepted ones.
package portalsuggestions

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/telemetry"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Length limits for user-supplied fields, in characters.
const (
	MaxNameLength = 100
	MaxURLLength  = 2048
	MaxNoteLength = 1000
)

const pgUniqueViolation = "23505"

var (
	// ErrAlreadySuggested is returned when the caller already suggested the
	// same URL. Create returns the existing suggestion alongside it.
	ErrAlreadySuggested = errors.New("portal already suggested")
	// ErrAlreadySupported is returned when the URL belongs to a portal that
	// is already searched.
	ErrAlreadySupported = errors.New("portal already supported")
)

// ValidationError describes an invalid or missing parameter.
type ValidationError struct {
	// Param is the JSON name of the offending field, e.g. "url".
	Param string
	// Missing is true when the parameter was required but absent.
	Missing bool
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// CreateParams are the fields of a new suggestion. Name defaults to the
// URL's host.
type CreateParams struct {
	Name string
	URL  string
	Note string
}

type Service struct {
	db             *gorm.DB
	supportedHosts []string
}

// New returns the service. supportedWebsites are the websites of portals
// that are already searched (e.g. "https://www.jobindex.dk"); suggestions
// for them are rejected with ErrAlreadySupported.
func New(db *gorm.DB, supportedWebsites ...string) *Service {
	s := &Service{db: db}
	for _, w := range supportedWebsites {
		if u, err := parseURL(w); err == nil {
			s.supportedHosts = append(s.supportedHosts, siteHost(u.Hostname()))
		}
	}
	return s
}

// Create records a suggestion from userID. If the caller already suggested
// the same URL, it returns the existing suggestion and ErrAlreadySuggested.
func (s *Service) Create(ctx context.Context, userID ulid.ULID, p CreateParams) (*models.PortalSuggestion, error) {
	ctx, span := telemetry.Start(ctx, "portalsuggestions.Service.Create", telemetry.ID(telemetry.AttrUserID, userID))
	defer span.End()
	rec, err := s.build(userID, p)
	if err != nil {
		return nil, span.RecordError(err)
	}

	if existing, err := s.find(ctx, userID, rec.URL); err != nil || existing != nil {
		if err != nil {
			return nil, err
		}
		return existing, span.RecordError(ErrAlreadySuggested)
	}

	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			// Lost a race with a concurrent identical request.
			existing, findErr := s.find(ctx, userID, rec.URL)
			if findErr == nil && existing != nil {
				return existing, span.RecordError(ErrAlreadySuggested)
			}
		}
		return nil, fmt.Errorf("create portal suggestion: %w", err)
	}
	return rec, nil
}

// List returns the caller's suggestions, newest first.
func (s *Service) List(ctx context.Context, userID ulid.ULID) ([]models.PortalSuggestion, error) {
	ctx, span := telemetry.Start(ctx, "portalsuggestions.Service.List", telemetry.ID(telemetry.AttrUserID, userID))
	defer span.End()
	var recs []models.PortalSuggestion
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&recs).Error; err != nil {
		return nil, fmt.Errorf("list portal suggestions: %w", err)
	}
	return recs, nil
}

func (s *Service) find(ctx context.Context, userID ulid.ULID, normalizedURL string) (*models.PortalSuggestion, error) {
	var rec models.PortalSuggestion
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND url = ?", userID, normalizedURL).
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find portal suggestion: %w", err)
	}
	return &rec, nil
}

func (s *Service) build(userID ulid.ULID, p CreateParams) (*models.PortalSuggestion, error) {
	rawURL := strings.TrimSpace(p.URL)
	if rawURL == "" {
		return nil, &ValidationError{Param: "url", Missing: true, Message: "The url parameter is required."}
	}
	if len(rawURL) > MaxURLLength {
		return nil, &ValidationError{Param: "url", Message: fmt.Sprintf("The url parameter must be at most %d characters long.", MaxURLLength)}
	}
	u, err := parseURL(rawURL)
	if err != nil {
		return nil, &ValidationError{Param: "url", Message: "The url parameter must be a valid http or https URL, for example https://www.stepstone.de."}
	}

	host := siteHost(u.Hostname())
	for _, supported := range s.supportedHosts {
		if host == supported || strings.HasSuffix(host, "."+supported) {
			return nil, ErrAlreadySupported
		}
	}

	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = host
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return nil, &ValidationError{Param: "name", Message: fmt.Sprintf("The name parameter must be at most %d characters long.", MaxNameLength)}
	}

	note := strings.TrimSpace(p.Note)
	if utf8.RuneCountInString(note) > MaxNoteLength {
		return nil, &ValidationError{Param: "note", Message: fmt.Sprintf("The note parameter must be at most %d characters long.", MaxNoteLength)}
	}

	return &models.PortalSuggestion{
		UserID: userID,
		Name:   name,
		URL:    normalize(u),
		Note:   note,
		Status: models.PortalSuggestionSubmitted,
	}, nil
}

// parseURL parses raw as an http(s) URL, assuming https when no scheme is
// given ("stepstone.de").
func parseURL(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("not an http(s) URL")
	}
	if !strings.Contains(u.Hostname(), ".") {
		return nil, errors.New("host has no domain")
	}
	return u, nil
}

// siteHost lower-cases host and drops a leading "www.".
func siteHost(host string) string {
	return strings.TrimPrefix(strings.ToLower(host), "www.")
}

// normalize renders u in a canonical form so that trivially different
// spellings of the same portal compare equal: https scheme, lower-case host
// without "www.", no default port, no fragment and no trailing slash.
func normalize(u *url.URL) string {
	host := siteHost(u.Hostname())
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		host += ":" + port
	}
	out := "https://" + host + strings.TrimRight(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}
