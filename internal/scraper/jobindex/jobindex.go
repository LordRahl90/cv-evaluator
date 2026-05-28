package jobindex

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"cv-solution/internal/entities"

	"github.com/gocolly/colly"
)

const (
	baseDomain      = "jobindex.dk"
	provider        = "jobindex"
	searchLink      = "https://www.jobindex.dk/jobsoegning?address=Else+Alfelts+Vej+89%2C+2tv%2C+2300+K%C3%B8benhavn+S&employment_type=1&radius=50&jobage=7&q=senior+software+engineer"
	defaultMaxPages = 4
)

var jobIndexJobIDPattern = regexp.MustCompile(`/(?:jobannonce|job)/([A-Za-z0-9]+)`)
var jobIndexStashPattern = regexp.MustCompile(`(?s)var\s+Stash\s*=\s*(\{.*?\});`)

type jobIndexStashData struct {
	JobSearchResultApp struct {
		StoreData struct {
			SearchResponse struct {
				Results []jobIndexSearchResult `json:"results"`
			} `json:"searchResponse"`
		} `json:"storeData"`
	} `json:"jobsearch/result_app"`
}

type jobIndexSearchResult struct {
	TID         string `json:"tid"`
	Headline    string `json:"headline"`
	CompanyText string `json:"companytext"`
	Area        string `json:"area"`
	FirstDate   string `json:"firstdate"`
	URL         string `json:"url"`
}

func extractJobIndexJobID(detailsURL string) string {
	detailsURL = strings.TrimSpace(detailsURL)
	if detailsURL == "" {
		return ""
	}

	if matches := jobIndexJobIDPattern.FindStringSubmatch(detailsURL); len(matches) == 2 {
		return matches[1]
	}

	u, err := url.Parse(detailsURL)
	if err != nil {
		return ""
	}

	for _, key := range []string{"jobid", "id", "t"} {
		if id := strings.TrimSpace(u.Query().Get(key)); id != "" {
			return id
		}
	}

	return ""
}

type Service struct {
	SearchLinkPage string
	MaxPages       int
}

func (s *Service) SearchJobs(ctx context.Context) ([]entities.JobPost, error) {
	startURL := strings.TrimSpace(s.SearchLinkPage)
	if startURL == "" {
		startURL = searchLink
	}

	parsedURL, err := url.Parse(startURL)
	if err != nil {
		return nil, err
	}

	allowedDomains := []string{baseDomain}
	if hostname := parsedURL.Hostname(); hostname != "" {
		allowedDomains = append(allowedDomains, hostname)
	}
	if host := parsedURL.Host; host != "" && host != parsedURL.Hostname() {
		allowedDomains = append(allowedDomains, host)
	}

	maxPages := s.MaxPages
	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}

	c := colly.NewCollector(
		colly.AllowedDomains(allowedDomains...),
		colly.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"),
	)

	seenPages := map[string]struct{}{}
	seenJobs := map[string]struct{}{}
	var jobPosts []entities.JobPost

	appendUniqueJob := func(job entities.JobPost) {
		key := strings.TrimSpace(job.ID)
		if key == "" {
			key = strings.TrimSpace(job.DetailsURL)
		}
		if key == "" {
			key = strings.TrimSpace(job.Title + "|" + job.Company + "|" + job.Location)
		}
		if key == "" {
			return
		}
		if _, exists := seenJobs[key]; exists {
			return
		}
		seenJobs[key] = struct{}{}
		jobPosts = append(jobPosts, job)
	}

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
		if _, seen := seenPages[normalized]; seen {
			r.Abort()
			return
		}
		if len(seenPages) >= maxPages {
			r.Abort()
			return
		}
		seenPages[normalized] = struct{}{}
	})

	parseStashJobs := func(body []byte, responseURL *url.URL) {
		matches := jobIndexStashPattern.FindSubmatch(body)
		if len(matches) < 2 {
			return
		}

		var stash jobIndexStashData
		if err := json.Unmarshal(matches[1], &stash); err != nil {
			slog.WarnContext(
				ctx,
				"failed to parse Jobindex Stash payload",
				slog.String("provider", provider),
				slog.Any("error", err),
			)
			return
		}

		for _, result := range stash.JobSearchResultApp.StoreData.SearchResponse.Results {
			detailsURL := strings.TrimSpace(result.URL)
			if responseURL != nil {
				detailsURL = strings.TrimSpace(responseURL.ResolveReference(&url.URL{Path: detailsURL}).String())
			}
			job := entities.JobPost{
				ID:         strings.TrimSpace(result.TID),
				Provider:   provider,
				Title:      strings.TrimSpace(html.UnescapeString(result.Headline)),
				Company:    strings.TrimSpace(html.UnescapeString(result.CompanyText)),
				Location:   strings.TrimSpace(html.UnescapeString(result.Area)),
				PostedAt:   strings.TrimSpace(result.FirstDate),
				DetailsURL: detailsURL,
			}
			if job.Title == "" {
				continue
			}
			if job.ID == "" {
				job.ID = extractJobIndexJobID(job.DetailsURL)
			}
			appendUniqueJob(job)
		}
	}

	c.OnResponse(func(r *colly.Response) {
		if r == nil || len(r.Body) == 0 {
			return
		}
		parseStashJobs(r.Body, r.Request.URL)
	})

	c.OnHTML("article.jobsearch-result, div.jobsearch-result", func(e *colly.HTMLElement) {
		detailsURL := strings.TrimSpace(e.Request.AbsoluteURL(e.ChildAttr("a.jobsearch-result__joblink[href]", "href")))
		if detailsURL == "" {
			detailsURL = strings.TrimSpace(e.Request.AbsoluteURL(e.ChildAttr("h4 a[href]", "href")))
		}
		job := entities.JobPost{
			Provider:   provider,
			Title:      strings.TrimSpace(e.ChildText("a.jobsearch-result__joblink")),
			Company:    strings.TrimSpace(e.ChildText(".jobsearch-result__company, .jix-toolbar-top__company")),
			Location:   strings.TrimSpace(e.ChildText(".jobsearch-result__location, .jix_robotjob--area")),
			PostedAt:   strings.TrimSpace(e.ChildText("time")),
			DetailsURL: detailsURL,
		}
		if job.Title == "" {
			job.Title = strings.TrimSpace(e.ChildText("h4 a"))
		}

		if job.Title == "" {
			return
		}

		job.ID = extractJobIndexJobID(job.DetailsURL)
		if job.ID == "" {
			job.ID = strings.TrimSpace(e.Attr("data-job-id"))
		}
		if job.ID == "" {
			job.ID = strings.TrimSpace(e.Attr("data-beacon-tid"))
		}

		appendUniqueJob(job)
	})

	c.OnHTML("a[aria-label='Next'][href], a[rel='next'][href], a.pagination-next[href], a.next[href]", func(e *colly.HTMLElement) {
		nextURL := strings.TrimSpace(e.Request.AbsoluteURL(e.Attr("href")))
		if nextURL == "" || len(seenPages) >= maxPages {
			return
		}

		if visitErr := e.Request.Visit(nextURL); visitErr != nil && visitErr != colly.ErrAlreadyVisited {
			slog.WarnContext(
				ctx,
				"failed to queue next Jobindex results page",
				slog.String("provider", provider),
				slog.String("url", nextURL),
				slog.Any("error", visitErr),
			)
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
			"Jobindex search request failed",
			slog.String("provider", provider),
			slog.Int("status_code", statusCode),
			slog.String("url", requestURL),
			slog.Any("error", crawlErr),
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
		ID:       strings.TrimSpace(job.ID),
		Provider: provider,
	}
	if details.ID == "" {
		details.ID = extractJobIndexJobID(detailsURL)
	}

	normalizePlainText := func(input string) string {
		return strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	}

	selectors := "div.jobad-description, div.jobad-description-text, section.job-description, article.jobad-content, div.job-ad"
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
			"Jobindex details request failed",
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

	return details, nil
}
