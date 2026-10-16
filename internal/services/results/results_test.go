package results

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var fixedNow = time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	svc := New(db)
	svc.now = func() time.Time { return fixedNow }
	return svc, db
}

func ptr[T any](v T) *T { return &v }

type seeder struct {
	t  *testing.T
	db *gorm.DB
	n  int
}

func (s *seeder) search(userID ulid.ULID, name string) *models.JobSearch {
	s.t.Helper()
	js := &models.JobSearch{UserID: userID, Name: name, Query: name, Frequency: models.FrequencyDaily, Enabled: true}
	require.NoError(s.t, s.db.Create(js).Error)
	return js
}

// resultOpts describe a seeded result. A nil score leaves it unevaluated.
type resultOpts struct {
	score      *int
	status     string
	evalStatus string
	found      time.Time
	missing    []string
	raw        string
	scores     map[string]int
}

func (s *seeder) result(js *models.JobSearch, o resultOpts) *models.SearchResult {
	s.t.Helper()
	s.n++
	job := &models.Job{
		Provider: "linkedin", JobID: fmt.Sprint(s.n), Title: fmt.Sprintf("Job %d", s.n),
		Company: "Acme", Location: "Copenhagen", PostedAt: "2 days ago",
		DetailsURL: fmt.Sprintf("https://example.com/jobs/%d", s.n), Detail: "We need Go.",
	}
	require.NoError(s.t, s.db.Create(job).Error)

	if o.found.IsZero() {
		o.found = fixedNow.Add(-time.Duration(s.n) * time.Hour)
	}
	if o.evalStatus == "" {
		o.evalStatus = models.EvaluationPending
		if o.score != nil {
			o.evalStatus = models.EvaluationDone
		}
	}
	r := &models.SearchResult{
		JobSearchID: js.ID, UserID: js.UserID, JobID: job.ID,
		Status: o.status, EvaluationStatus: o.evalStatus, CreatedAt: o.found,
	}
	require.NoError(s.t, s.db.Create(r).Error)

	if o.score != nil {
		eval := &models.Evaluation{
			SearchResultID: r.ID, CVID: ulid.Make(), OverallScore: *o.score,
			Strengths: models.StringList{"Go"}, MissingSkills: o.missing, Summary: "A fit.",
			RawResponse: o.raw, Model: "deepseek-chat", PromptVersion: "v4", Status: models.StatusCompleted,
		}
		require.NoError(s.t, s.db.Create(eval).Error)
		for criterion, score := range o.scores {
			require.NoError(s.t, s.db.Create(&models.EvaluationScore{
				EvaluationID: eval.ID, Criterion: criterion, Score: score, Reason: criterion + " reason",
			}).Error)
		}
		require.NoError(s.t, s.db.Model(r).Updates(map[string]any{"evaluation_id": eval.ID, "overall_score": *o.score}).Error)
		r.EvaluationID, r.OverallScore = &eval.ID, o.score
	}
	return r
}

func ids(page *Page) []ulid.ULID {
	out := make([]ulid.ULID, len(page.Results))
	for i, r := range page.Results {
		out[i] = r.ID
	}
	return out
}

func TestList_SortsFiltersAndPages(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	s := &seeder{t: t, db: db}
	user, other := ulid.Make(), ulid.Make()

	a := s.search(user, "Senior Go")
	b := s.search(user, "Backend")
	r90 := s.result(a, resultOpts{score: ptr(90), missing: []string{"Terraform", "Kafka", "Rust", "Java"},
		scores: map[string]int{"skills": 95}})
	r70 := s.result(a, resultOpts{score: ptr(70), status: models.SearchResultSaved, scores: map[string]int{"skills": 60}})
	r85 := s.result(b, resultOpts{score: ptr(85), status: models.SearchResultApplied, scores: map[string]int{"skills": 85}})
	rPending := s.result(b, resultOpts{})
	r60 := s.result(b, resultOpts{score: ptr(60), status: models.SearchResultDismissed, found: fixedNow.Add(-10 * 24 * time.Hour)})

	// Not the caller's, or no longer visible.
	s.result(s.search(other, "theirs"), resultOpts{score: ptr(99)})
	deleted := s.search(user, "deleted")
	s.result(deleted, resultOpts{score: ptr(98)})
	require.NoError(t, db.Delete(deleted).Error)

	page, err := svc.List(ctx, user, ListParams{})
	require.NoError(t, err)
	assert.Equal(t, []ulid.ULID{r90.ID, r85.ID, r70.ID, r60.ID, rPending.ID}, ids(page), "best score first, unscored last")
	assert.EqualValues(t, 5, page.TotalCount)
	assert.False(t, page.HasMore)

	first := page.Results[0]
	assert.Equal(t, models.SearchResultNew, first.Status)
	assert.Equal(t, models.EvaluationDone, first.EvaluationStatus)
	assert.Equal(t, SearchRef{ID: a.ID, Name: "Senior Go"}, first.Search)
	assert.Equal(t, "Acme", first.Job.Company)
	assert.Equal(t, "linkedin", first.Job.Provider)
	assert.NotEmpty(t, first.Job.DetailsURL)
	assert.Empty(t, first.Job.Description, "the list leaves descriptions out")
	require.NotNil(t, first.Evaluation)
	assert.Equal(t, 90, first.Evaluation.OverallScore)
	assert.Equal(t, []string{"Terraform", "Kafka", "Rust"}, first.Evaluation.TopMissingSkills)
	assert.Nil(t, page.Results[4].Evaluation)
	assert.Equal(t, []string{}, page.Results[1].Evaluation.TopMissingSkills)

	cases := []struct {
		name string
		p    ListParams
		want []ulid.ULID
	}{
		{"min score", ListParams{MinScore: ptr(80)}, []ulid.ULID{r90.ID, r85.ID}},
		{"statuses", ListParams{Statuses: []string{"saved", " Applied ", "saved"}}, []ulid.ULID{r85.ID, r70.ID}},
		{"evaluation status", ListParams{EvaluationStatuses: []string{"pending"}}, []ulid.ULID{rPending.ID}},
		{"since", ListParams{Since: ptr(fixedNow.Add(-7 * 24 * time.Hour)), MinScore: ptr(0)}, []ulid.ULID{r90.ID, r85.ID, r70.ID}},
		{"newest", ListParams{Sort: SortNewest}, []ulid.ULID{r90.ID, r70.ID, r85.ID, rPending.ID, r60.ID}},
		{"one search", ListParams{JobSearchID: &a.ID}, []ulid.ULID{r90.ID, r70.ID}},
		{"criterion", ListParams{Criterion: "Skills", MinCriterionScore: ptr(80)}, []ulid.ULID{r90.ID, r85.ID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := svc.List(ctx, user, tc.p)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ids(page))
			assert.EqualValues(t, len(tc.want), page.TotalCount)
		})
	}

	_, err = svc.List(ctx, user, ListParams{JobSearchID: ptr(s.search(other, "x").ID)})
	require.ErrorIs(t, err, ErrSearchNotFound)
	_, err = svc.List(ctx, user, ListParams{JobSearchID: &deleted.ID})
	require.ErrorIs(t, err, ErrSearchNotFound)
}

func TestList_CursorPagination(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	s := &seeder{t: t, db: db}
	user := ulid.Make()
	js := s.search(user, "Go")

	// Ties on score and unscored results must page without gaps or repeats.
	for _, score := range []*int{ptr(80), nil, ptr(90), ptr(80), nil, ptr(70), ptr(80)} {
		s.result(js, resultOpts{score: score})
	}

	for _, sort := range []string{SortScore, SortNewest} {
		t.Run(sort, func(t *testing.T) {
			all, err := svc.List(ctx, user, ListParams{Sort: sort})
			require.NoError(t, err)
			require.Len(t, all.Results, 7)

			var paged []ulid.ULID
			p := ListParams{Sort: sort, Limit: 3}
			for {
				page, err := svc.List(ctx, user, p)
				require.NoError(t, err)
				assert.EqualValues(t, 7, page.TotalCount)
				paged = append(paged, ids(page)...)
				if !page.HasMore {
					break
				}
				require.Len(t, page.Results, 3)
				p.StartingAfter = &page.Results[len(page.Results)-1].ID
			}
			assert.Equal(t, ids(all), paged)
		})
	}

	_, err := svc.List(ctx, user, ListParams{StartingAfter: ptr(ulid.Make())})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "starting_after", verr.Param)
}

func TestList_Validation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	cases := []struct {
		p       ListParams
		param   string
		missing bool
	}{
		{ListParams{MinScore: ptr(101)}, "min_score", false},
		{ListParams{Statuses: []string{"archived"}}, "status", false},
		{ListParams{EvaluationStatuses: []string{"queued"}}, "evaluation_status", false},
		{ListParams{Sort: "oldest"}, "sort", false},
		{ListParams{Limit: MaxLimit + 1}, "limit", false},
		{ListParams{Criterion: "location", MinCriterionScore: ptr(50)}, "criterion", false},
		{ListParams{Criterion: "skills"}, "min_criterion_score", true},
		{ListParams{MinCriterionScore: ptr(50)}, "criterion", true},
		{ListParams{Criterion: "skills", MinCriterionScore: ptr(-1)}, "min_criterion_score", false},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			_, err := svc.List(ctx, ulid.Make(), tc.p)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tc.param, verr.Param)
			assert.Equal(t, tc.missing, verr.Missing)
		})
	}
}

func TestGet(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	s := &seeder{t: t, db: db}
	user := ulid.Make()
	js := s.search(user, "Go")

	raw := `{"requirements":[{"requirement":"Go","importance":"must","met":"yes","evidence":"6 years"}],"overall_score":91}`
	r := s.result(js, resultOpts{score: ptr(91), missing: []string{"Terraform"}, raw: raw,
		scores: map[string]int{"education": 50, "skills": 94, "domain": 76, "legacy": 10, "experience": 90}})

	d, err := svc.Get(ctx, user, r.ID)
	require.NoError(t, err)
	assert.Equal(t, r.ID, d.ID)
	assert.Equal(t, "We need Go.", d.Job.Description)
	assert.Equal(t, SearchRef{ID: js.ID, Name: "Go"}, d.Search)
	require.NotNil(t, d.Evaluation)
	e := d.Evaluation
	assert.Equal(t, 91, e.OverallScore)
	assert.Equal(t, []string{"Go"}, e.Strengths)
	assert.Equal(t, []string{"Terraform"}, e.MissingSkills)
	assert.Equal(t, "A fit.", e.Summary)
	assert.Equal(t, "deepseek-chat", e.Model)
	assert.Equal(t, "v4", e.PromptVersion)
	assert.NotEqual(t, ulid.ULID{}, e.CVID)
	assert.False(t, e.EvaluatedAt.IsZero())
	assert.Equal(t, []matcher.Requirement{{Requirement: "Go", Importance: "must", Met: "yes", Evidence: "6 years"}}, e.Requirements)

	criteria := make([]string, len(e.Breakdown))
	for i, b := range e.Breakdown {
		criteria[i] = b.Criterion
	}
	assert.Equal(t, []string{"skills", "experience", "domain", "education", "legacy"}, criteria,
		"criteria order, unknown criteria last")
	assert.Equal(t, 94, e.Breakdown[0].Score)
	assert.Equal(t, "skills reason", e.Breakdown[0].Reason)

	// An older evaluation without requirements, and an unevaluated result.
	old := s.result(js, resultOpts{score: ptr(50), raw: `{"overall_score":50}`})
	d, err = svc.Get(ctx, user, old.ID)
	require.NoError(t, err)
	require.NotNil(t, d.Evaluation)
	assert.Nil(t, d.Evaluation.Requirements)
	assert.Empty(t, d.Evaluation.Breakdown)
	assert.Equal(t, []string{}, d.Evaluation.MissingSkills)

	pending := s.result(js, resultOpts{})
	d, err = svc.Get(ctx, user, pending.ID)
	require.NoError(t, err)
	assert.Nil(t, d.Evaluation)
	assert.Equal(t, models.EvaluationPending, d.EvaluationStatus)

	_, err = svc.Get(ctx, ulid.Make(), r.ID)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.Get(ctx, user, ulid.Make())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUpdateStatus(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	s := &seeder{t: t, db: db}
	user := ulid.Make()
	r := s.result(s.search(user, "Go"), resultOpts{score: ptr(80)})

	d, err := svc.UpdateStatus(ctx, user, r.ID, " Saved ")
	require.NoError(t, err)
	assert.Equal(t, models.SearchResultSaved, d.Status)

	var got models.SearchResult
	require.NoError(t, db.Take(&got, "id = ?", r.ID).Error)
	assert.Equal(t, models.SearchResultSaved, got.Status)

	_, err = svc.UpdateStatus(ctx, user, r.ID, "archived")
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "status", verr.Param)
	_, err = svc.UpdateStatus(ctx, user, r.ID, "")
	require.ErrorAs(t, err, &verr)
	assert.True(t, verr.Missing)

	_, err = svc.UpdateStatus(ctx, ulid.Make(), r.ID, "applied")
	require.ErrorIs(t, err, ErrNotFound)
	var unchanged models.SearchResult
	require.NoError(t, db.Take(&unchanged, "id = ?", r.ID).Error)
	assert.Equal(t, models.SearchResultSaved, unchanged.Status, "another user cannot change it")
}

func TestReEvaluate(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	s := &seeder{t: t, db: db}
	user := ulid.Make()
	js := s.search(user, "Go")

	done := s.result(js, resultOpts{score: ptr(80)})
	require.NoError(t, db.Model(done).Updates(map[string]any{"attempts": 2, "last_error": "boom"}).Error)

	d, err := svc.ReEvaluate(ctx, user, done.ID)
	require.NoError(t, err)
	assert.Equal(t, models.EvaluationPending, d.EvaluationStatus)
	require.NotNil(t, d.Evaluation, "the previous evaluation stays until replaced")
	assert.Equal(t, 80, d.Evaluation.OverallScore)

	var got models.SearchResult
	require.NoError(t, db.Take(&got, "id = ?", done.ID).Error)
	assert.Equal(t, 0, got.Attempts)
	assert.Nil(t, got.NextAttemptAt)
	assert.Empty(t, got.LastError)

	_, err = svc.ReEvaluate(ctx, user, done.ID)
	require.ErrorIs(t, err, ErrEvaluationInProgress, "already due")

	processing := s.result(js, resultOpts{evalStatus: models.EvaluationProcessing})
	_, err = svc.ReEvaluate(ctx, user, processing.ID)
	require.ErrorIs(t, err, ErrEvaluationInProgress)

	failed := s.result(js, resultOpts{evalStatus: models.EvaluationFailed})
	_, err = svc.ReEvaluate(ctx, user, failed.ID)
	require.NoError(t, err)

	// Waiting for a later attempt (e.g. the user had no CV): made due now.
	waiting := s.result(js, resultOpts{})
	require.NoError(t, db.Model(waiting).Update("next_attempt_at", fixedNow.Add(time.Hour)).Error)
	_, err = svc.ReEvaluate(ctx, user, waiting.ID)
	require.NoError(t, err)
	var due models.SearchResult
	require.NoError(t, db.Take(&due, "id = ?", waiting.ID).Error)
	assert.Nil(t, due.NextAttemptAt)

	_, err = svc.ReEvaluate(ctx, ulid.Make(), failed.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSummary(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	s := &seeder{t: t, db: db}
	user := ulid.Make()

	a := s.search(user, "Senior Go")
	b := s.search(user, "Backend")
	empty := s.search(user, "Platform")
	old := fixedNow.Add(-8 * 24 * time.Hour)

	s.result(a, resultOpts{score: ptr(91)})
	s.result(a, resultOpts{score: ptr(85), status: models.SearchResultSaved})
	s.result(a, resultOpts{score: ptr(82), status: models.SearchResultDismissed})
	s.result(b, resultOpts{score: ptr(60), found: old})
	s.result(b, resultOpts{evalStatus: models.EvaluationProcessing})
	s.result(b, resultOpts{evalStatus: models.EvaluationFailed, found: old, status: models.SearchResultApplied})
	s.result(s.search(ulid.Make(), "theirs"), resultOpts{score: ptr(95)})

	sum, err := svc.Summary(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, fixedNow.Add(-7*24*time.Hour), sum.WeekStart)
	assert.EqualValues(t, 6, sum.Total)
	assert.EqualValues(t, 4, sum.Scored)
	assert.EqualValues(t, 1, sum.Evaluating)
	assert.EqualValues(t, 1, sum.FailedEvaluation)
	assert.EqualValues(t, 4, sum.NewThisWeek)
	assert.EqualValues(t, 2, sum.NewThisWeekSearches)
	assert.EqualValues(t, 2, sum.StrongMatches, "dismissed ones do not count")
	assert.EqualValues(t, 1, sum.StrongMatchesNew)
	assert.Equal(t, map[string]int64{"new": 3, "saved": 1, "applied": 1, "dismissed": 1}, sum.ByStatus)
	assert.Equal(t, []SearchCount{
		{ID: a.ID, Name: "Senior Go", Total: 3, NewThisWeek: 3},
		{ID: b.ID, Name: "Backend", Total: 3, NewThisWeek: 1},
		{ID: empty.ID, Name: "Platform", Total: 0, NewThisWeek: 0},
	}, sum.BySearch)

	sum, err = svc.Summary(ctx, ulid.Make())
	require.NoError(t, err)
	assert.Zero(t, sum.Total)
	assert.Equal(t, []SearchCount{}, sum.BySearch)
}
