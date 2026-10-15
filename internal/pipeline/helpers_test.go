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
	"cv-evaluator/internal/queue"
	"cv-evaluator/internal/services/jobs"
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
	_ Embedder   = (*jobs.Service)(nil)
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
		"TRUNCATE queue_tasks, jobs, search_results, evaluations, cvs, section_embeddings").Error)
}

// --- fakes -----------------------------------------------------------------

type fakeSearches struct {
	mu       sync.Mutex
	searches map[ulid.ULID]*Search
	marks    map[ulid.ULID]int
	markErr  error
}

func newFakeSearches(searches ...*Search) *fakeSearches {
	f := &fakeSearches{searches: map[ulid.ULID]*Search{}, marks: map[ulid.ULID]int{}}
	for _, s := range searches {
		f.searches[s.ID] = s
	}
	return f
}

func (f *fakeSearches) DueSearches(_ context.Context, now time.Time, limit int) ([]Search, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var due []Search
	for _, s := range f.searches {
		if s.Enabled && (s.NextRunAt == nil || !s.NextRunAt.After(now)) {
			due = append(due, *s)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].ID.Compare(due[j].ID) < 0 })
	if len(due) > limit {
		due = due[:limit]
	}
	return due, nil
}

func (f *fakeSearches) GetSearch(_ context.Context, id ulid.ULID) (*Search, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.searches[id]
	if !ok {
		return nil, nil
	}
	cp := *s
	return &cp, nil
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
	// Return copies so handlers cannot mutate the fixture.
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

type fakeEmbeddingLLM struct {
	calls atomic.Int32
}

func (f *fakeEmbeddingLLM) CreateEmbedding(context.Context, string) ([]float32, error) {
	f.calls.Add(1)
	v := make([]float32, 768)
	v[0] = 1
	return v, nil
}

type fakeEvaluator struct {
	mu       sync.Mutex
	calls    int
	cvs      []ulid.ULID
	fail     func(call int) (string, error) // non-nil error fails the call
	delay    time.Duration
	inFlight atomic.Int32
	maxSeen  atomic.Int32
}

const goodRaw = `{"overall_score":77}`

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
	fail := f.fail
	f.mu.Unlock()

	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}

	if fail != nil {
		if raw, err := fail(call); err != nil {
			return nil, raw, err
		}
	}
	return &matcher.Response{
		OverallScore:  77,
		SkillsScore:   70,
		Strengths:     []string{"go"},
		MissingSkills: []string{"rust"},
		Summary:       "solid",
	}, goodRaw, nil
}

func (f *fakeEvaluator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// --- harness ---------------------------------------------------------------

type harness struct {
	t         *testing.T
	queue     *queue.Queue
	searches  *fakeSearches
	fetcher   *fakeFetcher
	embedLLM  *fakeEmbeddingLLM
	evaluator *fakeEvaluator
	pipeline  *Pipeline
	worker    *queue.Worker
	scheduler *Scheduler
	clock     *testClock
}

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

type harnessOption func(*Config)

func newHarness(t *testing.T, searches *fakeSearches, fetcher *fakeFetcher, opts ...harnessOption) *harness {
	t.Helper()
	resetDB(t)

	q := queue.New(testDB, queue.Options{BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	embedLLM := &fakeEmbeddingLLM{}
	evaluator := &fakeEvaluator{}

	cfg := Config{
		DB:        testDB,
		Queue:     q,
		Searches:  searches,
		Fetcher:   fetcher,
		Embedder:  jobs.New(testDB, embedLLM),
		Evaluator: evaluator,
		RateLimit: RateLimitOptions{Interval: time.Microsecond, MaxPenalty: time.Microsecond},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	p, err := New(cfg)
	require.NoError(t, err)

	clock := &testClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	return &harness{
		t:         t,
		queue:     q,
		searches:  searches,
		fetcher:   fetcher,
		embedLLM:  embedLLM,
		evaluator: evaluator,
		pipeline:  p,
		worker:    queue.NewWorker(q, p.Handlers(), queue.WorkerOptions{}),
		scheduler: NewScheduler(q, searches, fetcher, SchedulerOptions{Now: clock.Now}),
		clock:     clock,
	}
}

// drain runs tasks until the queue stays empty for a few polls.
func (h *harness) drain() {
	h.t.Helper()
	idle := 0
	for i := 0; i < 2000 && idle < 5; i++ {
		worked, err := h.worker.RunOnce(h.t.Context())
		require.NoError(h.t, err)
		if worked {
			idle = 0
			continue
		}
		idle++
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *harness) tick() int {
	h.t.Helper()
	n, err := h.scheduler.Tick(h.t.Context())
	require.NoError(h.t, err)
	return n
}

func newSearch(userID ulid.ULID, query string) *Search {
	return &Search{
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
