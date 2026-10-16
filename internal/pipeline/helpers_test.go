package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The real services satisfy the pipeline's interfaces.
var (
	_ Evaluator  = (*matcher.Service)(nil)
	_ CVSelector = (*LatestCVSelector)(nil)
)

// Tests share one Postgres container and reset it before each test, so they
// must not run in parallel.
var testDB *gorm.DB

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()
	container, err := postgres.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		return 1
	}
	defer func() { _ = container.Terminate(context.Background()) }()

	testDB, err = container.OpenGorm(&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "open gorm: %v\n", err)
		return 1
	}
	return m.Run()
}

func resetDB(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		"TRUNCATE jobs, search_results, evaluations, evaluation_scores, cvs, section_embeddings").Error)
}

// --- fakes -----------------------------------------------------------------

// fakeSearches is an in-memory SearchStore with the same semantics as the
// real one: due means enabled and next_run_at <= now; ClaimRun is atomic.
type fakeSearches struct {
	mu       sync.Mutex
	searches map[ulid.ULID]*Search
	claims   map[ulid.ULID]int
	marks    map[ulid.ULID]int
	markErr  error
}

func newFakeSearches(searches ...*Search) *fakeSearches {
	f := &fakeSearches{searches: map[ulid.ULID]*Search{}, claims: map[ulid.ULID]int{}, marks: map[ulid.ULID]int{}}
	for _, s := range searches {
		f.searches[s.ID] = s
	}
	return f
}

func isDue(s *Search, now time.Time) bool {
	return s.Enabled && s.NextRunAt != nil && !s.NextRunAt.After(now)
}

func (f *fakeSearches) DueSearches(_ context.Context, now time.Time, limit int) ([]Search, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var due []Search
	for _, s := range f.searches {
		if isDue(s, now) {
			due = append(due, *s)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].ID.Compare(due[j].ID) < 0 })
	if len(due) > limit {
		due = due[:limit]
	}
	return due, nil
}

func (f *fakeSearches) ClaimRun(_ context.Context, id ulid.ULID, now, until time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.searches[id]
	if !ok || !isDue(s, now) {
		return false, nil
	}
	s.NextRunAt = &until
	f.claims[id]++
	return true, nil
}

func (f *fakeSearches) MarkRun(_ context.Context, id ulid.ULID, ranAt, next time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return f.markErr
	}
	s := f.searches[id]
	s.LastRunAt = &ranAt
	s.NextRunAt = &next
	f.marks[id]++
	return nil
}

func (f *fakeSearches) get(id ulid.ULID) Search {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.searches[id]
}

func (f *fakeSearches) claimCount(id ulid.ULID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims[id]
}

func (f *fakeSearches) markCount(id ulid.ULID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.marks[id]
}

type fakeFetcher struct {
	mu          sync.Mutex
	providers   []string
	posts       map[string][]entities.JobPost
	details     map[string]string // provider/id -> description
	searchErr   map[string]error
	searchCalls map[string]int
	detailCalls map[string]int
	criteria    []Criteria
}

func newFakeFetcher(providers ...string) *fakeFetcher {
	return &fakeFetcher{
		providers:   providers,
		posts:       map[string][]entities.JobPost{},
		details:     map[string]string{},
		searchErr:   map[string]error{},
		searchCalls: map[string]int{},
		detailCalls: map[string]int{},
	}
}

func (f *fakeFetcher) Providers() []string { return f.providers }

func (f *fakeFetcher) Search(_ context.Context, provider string, c Criteria) ([]entities.JobPost, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searchCalls[provider]++
	f.criteria = append(f.criteria, c)
	if err := f.searchErr[provider]; err != nil {
		return nil, err
	}
	// Return copies so the pipeline cannot mutate the fixture.
	return append([]entities.JobPost(nil), f.posts[provider]...), nil
}

func (f *fakeFetcher) Details(_ context.Context, provider string, job *entities.JobPost) (*entities.JobDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detailCalls[provider+"/"+job.ID]++
	d, ok := f.details[provider+"/"+job.ID]
	if !ok {
		return nil, errors.New("details not found")
	}
	return &entities.JobDetails{ID: job.ID, Details: d}, nil
}

func (f *fakeFetcher) setPosts(provider string, posts ...entities.JobPost) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts[provider] = posts
}

func (f *fakeFetcher) totalDetailCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.detailCalls {
		n += c
	}
	return n
}

func (f *fakeFetcher) searchCount(provider string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.searchCalls[provider]
}

type fakeEmbedder struct {
	calls atomic.Int32
	delay time.Duration
	err   error

	mu    sync.Mutex
	texts map[string]int
}

func (f *fakeEmbedder) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	f.calls.Add(1)
	f.mu.Lock()
	if f.texts == nil {
		f.texts = map[string]int{}
	}
	f.texts[text]++
	f.mu.Unlock()
	if f.delay > 0 {
		if err := sleepCtx(ctx, f.delay); err != nil {
			return nil, err
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	v := make([]float32, 768)
	v[0] = 1
	return v, nil
}

type fakeEvaluator struct {
	mu       sync.Mutex
	calls    int
	cvs      []ulid.ULID
	fail     func(call int) (string, error) // non-nil error fails the call
	resp     *matcher.Response
	delay    time.Duration
	inFlight atomic.Int32
	maxSeen  atomic.Int32
}

const goodRaw = `{"overall_score":77}`

func defaultResponse() *matcher.Response {
	return &matcher.Response{
		OverallScore: 77,
		Breakdown: []matcher.CriterionScore{
			{Criterion: "skills", Score: 70, Reason: "Go"},
			{Criterion: "location", Score: 90, Reason: "same city"},
		},
		Strengths:     []string{"go"},
		MissingSkills: []string{"rust"},
		Summary:       "solid",
	}
}

func (f *fakeEvaluator) MatchJob(ctx context.Context, cvID ulid.ULID, _ *models.Job) (*matcher.Response, string, error) {
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		seen := f.maxSeen.Load()
		if n <= seen || f.maxSeen.CompareAndSwap(seen, n) {
			break
		}
	}

	f.mu.Lock()
	f.calls++
	call := f.calls
	f.cvs = append(f.cvs, cvID)
	fail, resp := f.fail, f.resp
	f.mu.Unlock()

	if f.delay > 0 {
		if err := sleepCtx(ctx, f.delay); err != nil {
			return nil, "", err
		}
	}
	if fail != nil {
		if raw, err := fail(call); err != nil {
			return nil, raw, err
		}
	}
	if resp == nil {
		resp = defaultResponse()
	}
	return resp, goodRaw, nil
}

func (f *fakeEvaluator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// --- harness ---------------------------------------------------------------

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type harness struct {
	t         *testing.T
	searches  *fakeSearches
	fetcher   *fakeFetcher
	embedder  *fakeEmbedder
	evaluator *fakeEvaluator
	pipeline  *Pipeline
	clock     *testClock
}

type harnessOption func(*Config)

func newHarness(t *testing.T, searches *fakeSearches, fetcher *fakeFetcher, opts ...harnessOption) *harness {
	t.Helper()
	resetDB(t)

	clock := &testClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	embedder := &fakeEmbedder{}
	evaluator := &fakeEvaluator{}
	cfg := Config{
		DB:            testDB,
		Searches:      searches,
		Fetcher:       fetcher,
		Embedder:      embedder,
		Evaluator:     evaluator,
		RateLimit:     RateLimitOptions{Interval: time.Microsecond, MaxPenalty: time.Microsecond},
		PortalBackoff: time.Microsecond,
		BaseBackoff:   time.Minute,
		MaxBackoff:    10 * time.Minute,
		PollInterval:  5 * time.Millisecond,
		Now:           clock.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	p, err := New(cfg)
	require.NoError(t, err)
	return &harness{
		t: t, searches: searches, fetcher: fetcher, embedder: embedder,
		evaluator: evaluator, pipeline: p, clock: clock,
	}
}

// drain runs embed and evaluate steps until neither finds work at the
// current clock.
func (h *harness) drain() {
	h.t.Helper()
	for range 1000 {
		embedded, err := h.pipeline.EmbedOne(h.t.Context())
		require.NoError(h.t, err)
		evaluated, err := h.pipeline.EvaluateOne(h.t.Context())
		require.NoError(h.t, err)
		if !embedded && !evaluated {
			return
		}
	}
	h.t.Fatal("drain did not settle")
}

// drainWithRetries drains, then moves the clock past the longest backoff
// and drains again, until nothing is left to retry.
func (h *harness) drainWithRetries() {
	h.t.Helper()
	for range DefaultMaxAttempts + 1 {
		h.drain()
		h.clock.Advance(h.pipeline.cfg.MaxBackoff + time.Second)
	}
	h.drain()
}

func (h *harness) tick() int {
	h.t.Helper()
	n, err := h.pipeline.Tick(h.t.Context())
	require.NoError(h.t, err)
	return n
}

// newSearch returns an enabled search that is due (NextRunAt in the past),
// as a newly created search is.
func newSearch(userID ulid.ULID, query string) *Search {
	due := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	return &Search{
		NextRunAt: &due,
		ID:        ulid.Make(),
		UserID:    userID,
		Name:      query,
		Query:     query,
		Location:  "Copenhagen",
		Frequency: FrequencyDaily,
		Enabled:   true,
	}
}

func seedCV(t *testing.T, userID ulid.ULID, createdAt time.Time) *models.CV {
	t.Helper()
	cv := &models.CV{
		UserID:           userID,
		CreatedAt:        createdAt,
		ExtractedContent: "cv",
		FullEmbedding:    pgvector.NewVector(make([]float32, 768)),
	}
	require.NoError(t, testDB.Create(cv).Error)
	return cv
}

func post(provider, id string, details string) entities.JobPost {
	p := entities.JobPost{
		ID:         id,
		Provider:   provider,
		Title:      "Engineer " + id,
		Company:    "ACME",
		DetailsURL: "https://example.com/" + provider + "/" + id,
	}
	if details != "" {
		p.Details = &entities.JobDetails{ID: id, Details: details}
	}
	return p
}

func count(t *testing.T, model any, where ...any) int64 {
	t.Helper()
	var n int64
	q := testDB.Model(model)
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	require.NoError(t, q.Count(&n).Error)
	return n
}

// seedEmbeddedJob stores an embedded job and a pending result for search.
func seedEmbeddedJob(t *testing.T, search *Search, provider, id string) (*models.Job, *models.SearchResult) {
	t.Helper()
	job := &models.Job{Provider: provider, JobID: id, Detail: "desc " + id, Status: models.JobStatusEmbedded, Embedding: vector(make([]float32, 768))}
	require.NoError(t, testDB.Create(job).Error)
	require.NoError(t, upsertSearchResult(testDB, search, job.ID))
	var r models.SearchResult
	require.NoError(t, testDB.Where("job_id = ?", job.ID).Take(&r).Error)
	return job, &r
}
