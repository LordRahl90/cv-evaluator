package linkedin

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"cv-evaluator/internal/entities"

	"github.com/gocolly/colly"
)

const (
	baseDomain = "www.linkedin.com"
	provider   = "linkedin"

	defaultMaxPages = 3
)

var linkedInJobIDPattern = regexp.MustCompile(`/jobs/view/(?:[^/?]*-)?(\d+)(?:[/?]|$)`)
var linkedInNumericIDPattern = regexp.MustCompile(`^\d+$`)

type Service struct {
	SearchLinkPage string
	MaxPages       int
}

func New() *Service {
	return &Service{
		//SearchLinkPage: searchLink,
		//MaxPages:       defaultMaxPages,
	}
}

func (s *Service) SearchJobs(ctx context.Context, searchLink string, maxPages int) ([]entities.JobPost, error) {
	startURL := strings.TrimSpace(searchLink)
	if startURL == "" {
		startURL = searchLink
	}

	parsedURL, err := url.Parse(startURL)
	if err != nil {
		return nil, err
	}

	allowedDomains := []string{baseDomain}
	hostname := parsedURL.Hostname()
	host := parsedURL.Host
	if hostname != "" {
		allowedDomains = append(allowedDomains, hostname)
	}
	if host != "" && host != hostname {
		allowedDomains = append(allowedDomains, host)
	}

	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}

	c := colly.NewCollector(
		colly.AllowedDomains(allowedDomains...),
		colly.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"),
	)

	var jobPosts []entities.JobPost
	seenPages := make(map[string]struct{})

	normalizeURL := func(rawURL string) string {
		u, parseErr := url.Parse(rawURL)
		if parseErr != nil {
			return rawURL
		}
		u.Fragment = ""
		return u.String()
	}

	c.OnRequest(func(r *colly.Request) {
		normalized := normalizeURL(r.URL.String())
		if _, exists := seenPages[normalized]; exists {
			r.Abort()
			return
		}
		if len(seenPages) >= maxPages {
			r.Abort()
			return
		}
		seenPages[normalized] = struct{}{}
	})

	c.OnHTML("li div.base-search-card", func(e *colly.HTMLElement) {
		job := entities.JobPost{
			Provider:   provider,
			Title:      strings.TrimSpace(e.ChildText("h3.base-search-card__title")),
			Company:    strings.TrimSpace(e.ChildText("h4.base-search-card__subtitle")),
			Location:   strings.TrimSpace(e.ChildText("span.job-search-card__location")),
			PostedAt:   strings.TrimSpace(e.ChildAttr("time", "datetime")),
			DetailsURL: strings.TrimSpace(e.ChildAttr("a.base-card__full-link", "href")),
		}
		job.ID = extractLinkedInJobID(job.DetailsURL)

		if job.Title == "" {
			return
		}

		if job.PostedAt == "" {
			job.PostedAt = strings.TrimSpace(e.ChildText("time"))
		}

		jobPosts = append(jobPosts, job)
	})

	c.OnHTML("a[aria-label='Next'][href], a[rel='next'][href]", func(e *colly.HTMLElement) {
		nextURL := strings.TrimSpace(e.Request.AbsoluteURL(e.Attr("href")))
		if nextURL == "" || len(seenPages) >= maxPages {
			return
		}

		if err := e.Request.Visit(nextURL); err != nil && !errors.Is(err, colly.ErrAlreadyVisited) {
			slog.WarnContext(
				ctx,
				"failed to queue next LinkedIn results page",
				slog.String("provider", provider),
				slog.String("url", nextURL),
				slog.Any("error", err),
			)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		statusCode := 0
		requestURL := ""
		if r != nil {
			statusCode = r.StatusCode
			requestURL = r.Request.URL.String()
		}

		slog.ErrorContext(
			ctx,
			"LinkedIn search request failed",
			slog.String("provider", provider),
			slog.Int("status_code", statusCode),
			slog.String("url", requestURL),
			slog.Any("error", err),
		)
	})

	if err := c.Visit(startURL); err != nil {
		return nil, err
	}

	return jobPosts, nil
}

func (s *Service) GetJobDetails(ctx context.Context, job *entities.JobPost) (*entities.JobDetails, error) {
	if job == nil {
		return nil, errors.New("job is required")
	}

	detailsURL := strings.TrimSpace(job.DetailsURL)
	if detailsURL == "" {
		return nil, errors.New("job details URL is required")
	}

	parsedURL, err := url.Parse(detailsURL)
	if err != nil {
		return nil, err
	}

	allowedDomains := []string{baseDomain}
	if hostname := parsedURL.Hostname(); hostname != "" {
		allowedDomains = append(allowedDomains, hostname)
	}
	if host := parsedURL.Host; host != "" {
		allowedDomains = append(allowedDomains, host)
	}

	c := colly.NewCollector(
		colly.AllowedDomains(allowedDomains...),
		colly.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"),
	)

	details := &entities.JobDetails{
		ID:       job.ID,
		Provider: provider,
	}
	if details.ID == "" {
		details.ID = extractLinkedInJobID(detailsURL)
	}

	normalizePlainText := func(input string) string {
		return strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	}

	selectors := "div.show-more-less-html__markup, div.jobs-description-content__text, section.show-more-less-html, div.description__text"
	c.OnHTML(selectors, func(e *colly.HTMLElement) {
		if details.Details != "" {
			return
		}

		text := normalizePlainText(e.DOM.Text())
		if text != "" {
			details.Details = text
			return
		}

		fallbackText := normalizePlainText(e.Text)
		if fallbackText != "" {
			details.Details = fallbackText
		}
	})

	c.OnError(func(r *colly.Response, crawlErr error) {
		statusCode := 0
		requestURL := ""
		if r != nil {
			statusCode = r.StatusCode
			requestURL = r.Request.URL.String()
		}

		slog.ErrorContext(
			ctx,
			"LinkedIn details request failed",
			slog.String("provider", provider),
			slog.Int("status_code", statusCode),
			slog.String("url", requestURL),
			slog.Any("error", crawlErr),
		)
	})

	if err := c.Visit(detailsURL); err != nil {
		return nil, err
	}

	if details.Details == "" {
		return nil, errors.New("job details not found")
	}

	details.Location = job.Location
	details.PostedAt = job.PostedAt

	return details, nil
}

func extractLinkedInJobID(detailsURL string) string {
	detailsURL = strings.TrimSpace(detailsURL)
	if detailsURL == "" {
		return ""
	}

	if matches := linkedInJobIDPattern.FindStringSubmatch(detailsURL); len(matches) == 2 {
		return matches[1]
	}

	u, parseErr := url.Parse(detailsURL)
	if parseErr != nil {
		return ""
	}

	for _, key := range []string{"currentJobId", "jobId"} {
		if id := strings.TrimSpace(u.Query().Get(key)); id != "" {
			if linkedInNumericIDPattern.MatchString(id) {
				return id
			}
		}
	}

	return ""
}
