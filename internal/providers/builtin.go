package providers

import (
	"context"
	"net/http"
	"strings"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/scraper/jobindex"
	"cv-evaluator/internal/scraper/linkedin"
)

// Names of the built-in providers.
const (
	LinkedInName = "linkedin"
	JobindexName = "jobindex"
)

// Option configures a built-in provider.
type Option func(*options)

type options struct {
	transport http.RoundTripper
}

// WithTransport makes the provider send its HTTP requests through rt, for
// example an instrumented transport that records latency. A nil rt, or not
// passing the option, means http.DefaultTransport.
func WithTransport(rt http.RoundTripper) Option {
	return func(o *options) { o.transport = rt }
}

func buildOptions(opts []Option) options {
	o := options{transport: http.DefaultTransport}
	for _, opt := range opts {
		opt(&o)
	}
	if o.transport == nil {
		o.transport = http.DefaultTransport
	}
	return o
}

// searchURL builds the URL a search starts from.
func searchURL(c Criteria, build func(base, query, location string) (string, error), base string) (string, error) {
	if strings.TrimSpace(c.Query) == "" {
		return "", ErrEmptyCriteria
	}
	return build(base, c.Query, c.Location)
}

// LinkedIn searches public LinkedIn job listings.
type LinkedIn struct {
	scraper *linkedin.Service
	// searchBase is where built search URLs point; tests override it.
	searchBase string
}

// NewLinkedIn returns the LinkedIn provider.
func NewLinkedIn(opts ...Option) *LinkedIn {
	o := buildOptions(opts)
	scraper := linkedin.New()
	scraper.Transport = o.transport
	return &LinkedIn{scraper: scraper, searchBase: linkedin.DefaultSearchURL}
}

func (p *LinkedIn) Name() string { return LinkedInName }

func (p *LinkedIn) Info() Info {
	return Info{DisplayName: "LinkedIn", Website: "https://www.linkedin.com/jobs", Coverage: "Worldwide"}
}

func (p *LinkedIn) Search(ctx context.Context, c Criteria) ([]entities.JobPost, error) {
	startURL, err := searchURL(c, linkedin.SearchURL, p.searchBase)
	if err != nil {
		return nil, err
	}
	return p.scraper.SearchJobs(ctx, startURL, c.MaxPages)
}

func (p *LinkedIn) Details(ctx context.Context, job *entities.JobPost) (*entities.JobDetails, error) {
	return p.scraper.GetJobDetails(ctx, job)
}

// Jobindex searches jobindex.dk.
type Jobindex struct {
	transport http.RoundTripper
	// searchBase is where built search URLs point; tests override it.
	searchBase string
}

// NewJobindex returns the Jobindex provider.
func NewJobindex(opts ...Option) *Jobindex {
	o := buildOptions(opts)
	return &Jobindex{transport: o.transport, searchBase: jobindex.DefaultSearchURL}
}

func (p *Jobindex) Name() string { return JobindexName }

func (p *Jobindex) Info() Info {
	return Info{DisplayName: "Jobindex", Website: "https://www.jobindex.dk", Coverage: "Denmark"}
}

func (p *Jobindex) Search(ctx context.Context, c Criteria) ([]entities.JobPost, error) {
	startURL, err := searchURL(c, jobindex.SearchURL, p.searchBase)
	if err != nil {
		return nil, err
	}
	s := &jobindex.Service{SearchLinkPage: startURL, MaxPages: c.MaxPages, Transport: p.transport}
	return s.SearchJobs(ctx)
}

func (p *Jobindex) Details(ctx context.Context, job *entities.JobPost) (*entities.JobDetails, error) {
	return (&jobindex.Service{Transport: p.transport}).GetJobDetails(ctx, job)
}
