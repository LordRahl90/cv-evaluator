// Package results reads and triages a user's search results: the jobs their
// saved searches found, each with its current evaluation against their CV.
// Results of deleted searches, and of deleted jobs, are not shown.
//
// Results are filtered and sorted by the score copied onto search_results
// (see models.SearchResult), so listing never scans evaluations.
package results

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/telemetry"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

// Sort orders for List.
const (
	// SortScore is best overall score first, unscored results last, then
	// newest first.
	SortScore = "score"
	// SortNewest is most recently found first.
	SortNewest = "newest"
)

// Page sizes for List.
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// StrongScore is the overall score from which a match counts as strong.
const StrongScore = 80

// TopMissingSkills is how many missing skills a listed result carries.
const TopMissingSkills = 3

var (
	// ErrNotFound means the result does not exist or belongs to another
	// user.
	ErrNotFound = errors.New("search result not found")
	// ErrSearchNotFound means the job search does not exist or belongs to
	// another user.
	ErrSearchNotFound = errors.New("job search not found")
	// ErrEvaluationInProgress is returned by ReEvaluate while the result is
	// already waiting for, or undergoing, evaluation.
	ErrEvaluationInProgress = errors.New("evaluation already in progress")
)

// ValidationError describes an invalid or missing parameter.
type ValidationError struct {
	// Param is the name of the offending parameter, e.g. "status".
	Param string
	// Missing is true when the parameter was required but absent, and false
	// when it was present but invalid.
	Missing bool
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(param, message string) error {
	return &ValidationError{Param: param, Message: message}
}

func missing(param, message string) error {
	return &ValidationError{Param: param, Missing: true, Message: message}
}

// ListParams filter, sort and page a user's results. Zero values mean no
// filter, SortScore and DefaultLimit.
type ListParams struct {
	// JobSearchID limits the results to one of the user's searches.
	JobSearchID *ulid.ULID
	// MinScore keeps scored results whose overall score is at least this.
	MinScore *int
	// Statuses keeps results with any of these statuses.
	Statuses []string
	// EvaluationStatuses keeps results with any of these evaluation
	// statuses.
	EvaluationStatuses []string
	// Since keeps results found at or after this time.
	Since *time.Time
	// Criterion and MinCriterionScore, given together, keep scored results
	// whose current evaluation scored the criterion at least that.
	Criterion         string
	MinCriterionScore *int
	Sort              string
	Limit             int
	// StartingAfter is the id of the last result of the previous page.
	StartingAfter *ulid.ULID
}

// SearchRef names the search that found a result.
type SearchRef struct {
	ID   ulid.ULID
	Name string
}

// Job is the posting a result is about. Description is only filled by Get.
type Job struct {
	ID          ulid.ULID
	Title       string
	Company     string
	Location    string
	Provider    string
	DetailsURL  string
	PostedAt    string
	Description string
}

// EvaluationSummary is what a listed result shows of its evaluation.
type EvaluationSummary struct {
	OverallScore int
	// TopMissingSkills are the first TopMissingSkills missing skills.
	TopMissingSkills []string
	EvaluatedAt      time.Time
}

// Result is one listed search result.
type Result struct {
	ID               ulid.ULID
	Status           string
	EvaluationStatus string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Search           SearchRef
	Job              Job
	// Evaluation is nil until the result has a completed evaluation. A
	// result being re-evaluated keeps its previous one meanwhile.
	Evaluation *EvaluationSummary
}

// Page is one page of List.
type Page struct {
	Results []Result
	// HasMore reports whether another page follows.
	HasMore bool
	// TotalCount is how many results match the filters, on all pages.
	TotalCount int64
}

// CriterionScore is one criterion's score in an evaluation.
type CriterionScore struct {
	Criterion string
	Score     int
	Reason    string
}

// Evaluation is a result's full current evaluation.
type Evaluation struct {
	ID           ulid.ULID
	CVID         ulid.ULID
	OverallScore int
	// Breakdown is in the order of entities.EvaluationCriteria; scores for
	// criteria no longer in that list follow, by name.
	Breakdown     []CriterionScore
	Strengths     []string
	MissingSkills []string
	Summary       string
	// Requirements is nil for evaluations made before the LLM was asked
	// for them.
	Requirements  []matcher.Requirement
	Model         string
	PromptVersion string
	EvaluatedAt   time.Time
}

// Detail is one search result with its full job and evaluation.
type Detail struct {
	ID               ulid.ULID
	Status           string
	EvaluationStatus string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Search           SearchRef
	Job              Job
	// Evaluation is nil until the result has a completed evaluation.
	Evaluation *Evaluation
}

// SearchCount is one search's share of a user's results.
type SearchCount struct {
	ID          ulid.ULID
	Name        string
	Total       int64
	NewThisWeek int64
}

// Summary counts a user's results for the dashboard.
type Summary struct {
	// WeekStart is the start of the rolling week the weekly counts cover.
	WeekStart time.Time
	Total     int64
	// Scored results have a completed evaluation.
	Scored int64
	// Evaluating results wait for, or are undergoing, evaluation.
	Evaluating int64
	// FailedEvaluation results could not be evaluated.
	FailedEvaluation int64
	// NewThisWeek results were found since WeekStart, by
	// NewThisWeekSearches different searches.
	NewThisWeek         int64
	NewThisWeekSearches int64
	// StrongMatches are results scored at least StrongScore that are not
	// dismissed; StrongMatchesNew of them still have status new.
	StrongMatches    int64
	StrongMatchesNew int64
	// ByStatus counts results per status; every status is present.
	ByStatus map[string]int64
	// BySearch has every search of the user, oldest first, including
	// searches with no results.
	BySearch []SearchCount
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// visible scopes a query on "search_results sr" to userID's results whose
// search and job still exist.
func (s *Service) visible(ctx context.Context, userID ulid.ULID) *gorm.DB {
	return s.db.WithContext(ctx).
		Table("search_results sr").
		Joins("JOIN job_searches js ON js.id = sr.job_search_id AND js.deleted_at IS NULL").
		Joins("JOIN jobs j ON j.id = sr.job_id AND j.deleted_at IS NULL").
		Where("sr.user_id = ?", userID)
}

// listRow is one row of the List query.
type listRow struct {
	ID               ulid.ULID
	Status           string
	EvaluationStatus string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	OverallScore     *int
	SearchID         ulid.ULID
	SearchName       string
	JobID            ulid.ULID
	Title            string
	Company          string
	Location         string
	Provider         string
	DetailsURL       string
	PostedAt         string
	MissingSkills    models.StringList
	EvaluatedAt      *time.Time
}

// List returns a page of userID's results.
func (s *Service) List(ctx context.Context, userID ulid.ULID, p ListParams) (_ *Page, err error) {
	ctx, end := telemetry.Start(ctx, "results.Service.List", telemetry.ID(telemetry.AttrUserID, userID))
	defer end(&err)
	if err := normalize(&p); err != nil {
		return nil, err
	}
	if p.JobSearchID != nil {
		if err := s.requireSearch(ctx, userID, *p.JobSearchID); err != nil {
			return nil, err
		}
	}

	filtered := func() *gorm.DB {
		q := s.visible(ctx, userID)
		if p.JobSearchID != nil {
			q = q.Where("sr.job_search_id = ?", *p.JobSearchID)
		}
		if p.MinScore != nil {
			q = q.Where("sr.overall_score >= ?", *p.MinScore)
		}
		if len(p.Statuses) > 0 {
			q = q.Where("sr.status IN ?", p.Statuses)
		}
		if len(p.EvaluationStatuses) > 0 {
			q = q.Where("sr.evaluation_status IN ?", p.EvaluationStatuses)
		}
		if p.Since != nil {
			q = q.Where("sr.created_at >= ?", *p.Since)
		}
		if p.Criterion != "" {
			q = q.Where("sr.overall_score IS NOT NULL AND EXISTS (SELECT 1 FROM evaluation_scores es "+
				"WHERE es.evaluation_id = sr.evaluation_id AND es.criterion = ? AND es.score >= ?)",
				p.Criterion, *p.MinCriterionScore)
		}
		return q
	}

	var total int64
	if err := filtered().Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count search results: %w", err)
	}

	q := filtered().
		Select(`sr.id, sr.status, sr.evaluation_status, sr.created_at, sr.updated_at, sr.overall_score,
			js.id AS search_id, js.name AS search_name,
			j.id AS job_id, j.title, j.company, j.location, j.provider, j.details_url, j.posted_at,
			e.missing_skills, e.updated_at AS evaluated_at`).
		Joins("LEFT JOIN evaluations e ON e.id = sr.evaluation_id AND sr.overall_score IS NOT NULL")
	if p.StartingAfter != nil {
		var err error
		if q, err = s.after(ctx, q, userID, &p); err != nil {
			return nil, err
		}
	}
	if p.Sort == SortNewest {
		q = q.Order("sr.created_at DESC, sr.id DESC")
	} else {
		q = q.Order("sr.overall_score DESC NULLS LAST, sr.id DESC")
	}

	var rows []listRow
	if err := q.Limit(p.Limit + 1).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list search results: %w", err)
	}

	page := &Page{TotalCount: total, HasMore: len(rows) > p.Limit}
	if page.HasMore {
		rows = rows[:p.Limit]
	}
	page.Results = make([]Result, len(rows))
	for i, r := range rows {
		page.Results[i] = r.result()
	}
	return page, nil
}

// after continues q past the StartingAfter result, in p.Sort order.
func (s *Service) after(ctx context.Context, q *gorm.DB, userID ulid.ULID, p *ListParams) (*gorm.DB, error) {
	var cursor []struct {
		CreatedAt    time.Time
		OverallScore *int
	}
	if err := s.db.WithContext(ctx).Model(&models.SearchResult{}).
		Select("created_at, overall_score").
		Where("id = ? AND user_id = ?", *p.StartingAfter, userID).
		Scan(&cursor).Error; err != nil {
		return nil, fmt.Errorf("load cursor: %w", err)
	}
	if len(cursor) == 0 {
		return nil, invalid("starting_after", "The starting_after parameter must be the id of one of your results.")
	}
	c, id := cursor[0], *p.StartingAfter

	switch {
	case p.Sort == SortNewest:
		return q.Where("(sr.created_at, sr.id) < (?, ?)", c.CreatedAt, id), nil
	case c.OverallScore == nil:
		return q.Where("sr.overall_score IS NULL AND sr.id < ?", id), nil
	default:
		return q.Where("(sr.overall_score < ? OR (sr.overall_score = ? AND sr.id < ?) OR sr.overall_score IS NULL)",
			*c.OverallScore, *c.OverallScore, id), nil
	}
}

func (r *listRow) result() Result {
	out := Result{
		ID:               r.ID,
		Status:           r.Status,
		EvaluationStatus: r.EvaluationStatus,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
		Search:           SearchRef{ID: r.SearchID, Name: r.SearchName},
		Job: Job{
			ID:         r.JobID,
			Title:      r.Title,
			Company:    r.Company,
			Location:   r.Location,
			Provider:   r.Provider,
			DetailsURL: r.DetailsURL,
			PostedAt:   r.PostedAt,
		},
	}
	if r.OverallScore != nil && r.EvaluatedAt != nil {
		top := []string(r.MissingSkills)
		if len(top) > TopMissingSkills {
			top = top[:TopMissingSkills]
		}
		if top == nil {
			top = []string{}
		}
		out.Evaluation = &EvaluationSummary{OverallScore: *r.OverallScore, TopMissingSkills: top, EvaluatedAt: *r.EvaluatedAt}
	}
	return out
}

// normalize validates p and fills in its defaults.
func normalize(p *ListParams) error {
	if p.MinScore != nil && (*p.MinScore < 0 || *p.MinScore > 100) {
		return invalid("min_score", "The min_score parameter must be between 0 and 100.")
	}

	statuses, err := oneOf("status", p.Statuses, models.SearchResultStatuses)
	if err != nil {
		return err
	}
	p.Statuses = statuses
	evalStatuses, err := oneOf("evaluation_status", p.EvaluationStatuses, models.EvaluationStatuses)
	if err != nil {
		return err
	}
	p.EvaluationStatuses = evalStatuses

	p.Criterion = strings.ToLower(strings.TrimSpace(p.Criterion))
	switch {
	case p.Criterion == "" && p.MinCriterionScore != nil:
		return missing("criterion", "The criterion parameter is required with min_criterion_score.")
	case p.Criterion != "" && p.MinCriterionScore == nil:
		return missing("min_criterion_score", "The min_criterion_score parameter is required with criterion.")
	case p.Criterion != "" && !entities.IsEvaluationCriterion(p.Criterion):
		names := make([]string, len(entities.EvaluationCriteria))
		for i, c := range entities.EvaluationCriteria {
			names[i] = c.Name
		}
		return invalid("criterion", "The criterion parameter must be one of: "+strings.Join(names, ", ")+".")
	case p.MinCriterionScore != nil && (*p.MinCriterionScore < 0 || *p.MinCriterionScore > 100):
		return invalid("min_criterion_score", "The min_criterion_score parameter must be between 0 and 100.")
	}

	switch p.Sort {
	case "":
		p.Sort = SortScore
	case SortScore, SortNewest:
	default:
		return invalid("sort", "The sort parameter must be one of: score, newest.")
	}

	switch {
	case p.Limit == 0:
		p.Limit = DefaultLimit
	case p.Limit < 1 || p.Limit > MaxLimit:
		return invalid("limit", fmt.Sprintf("The limit parameter must be between 1 and %d.", MaxLimit))
	}
	return nil
}

// oneOf trims and lowercases values, drops empty and repeated ones, and
// checks each is in allowed.
func oneOf(param string, values, allowed []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || seen[v] {
			continue
		}
		if !slices.Contains(allowed, v) {
			return nil, invalid(param, fmt.Sprintf("The %s parameter must be one of: %s.", param, strings.Join(allowed, ", ")))
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) requireSearch(ctx context.Context, userID, searchID ulid.ULID) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&models.JobSearch{}).
		Where("id = ? AND user_id = ?", searchID, userID).
		Count(&n).Error; err != nil {
		return fmt.Errorf("load job search: %w", err)
	}
	if n == 0 {
		return ErrSearchNotFound
	}
	return nil
}

// detailRow is the row of the Get query.
type detailRow struct {
	ID               ulid.ULID
	Status           string
	EvaluationStatus string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	EvaluationID     *ulid.ULID
	OverallScore     *int
	SearchID         ulid.ULID
	SearchName       string
	JobID            ulid.ULID
	Title            string
	Company          string
	Location         string
	Provider         string
	DetailsURL       string
	PostedAt         string
	Detail           string
}

// Get returns one of userID's results with its full job and evaluation.
func (s *Service) Get(ctx context.Context, userID, id ulid.ULID) (_ *Detail, err error) {
	ctx, end := telemetry.Start(ctx, "results.Service.Get", telemetry.ID(telemetry.AttrUserID, userID), telemetry.ID(telemetry.AttrResultID, id))
	defer end(&err)
	var rows []detailRow
	if err := s.visible(ctx, userID).
		Select(`sr.id, sr.status, sr.evaluation_status, sr.created_at, sr.updated_at, sr.evaluation_id, sr.overall_score,
			js.id AS search_id, js.name AS search_name,
			j.id AS job_id, j.title, j.company, j.location, j.provider, j.details_url, j.posted_at, j.detail`).
		Where("sr.id = ?", id).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("get search result: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	r := rows[0]

	d := &Detail{
		ID:               r.ID,
		Status:           r.Status,
		EvaluationStatus: r.EvaluationStatus,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
		Search:           SearchRef{ID: r.SearchID, Name: r.SearchName},
		Job: Job{
			ID:          r.JobID,
			Title:       r.Title,
			Company:     r.Company,
			Location:    r.Location,
			Provider:    r.Provider,
			DetailsURL:  r.DetailsURL,
			PostedAt:    r.PostedAt,
			Description: r.Detail,
		},
	}
	if r.EvaluationID != nil && r.OverallScore != nil {
		eval, err := s.evaluation(ctx, *r.EvaluationID)
		if err != nil {
			return nil, err
		}
		d.Evaluation = eval
	}
	return d, nil
}

// evaluation loads a completed evaluation with its scores, or nil when it
// is gone or did not complete.
func (s *Service) evaluation(ctx context.Context, id ulid.ULID) (*Evaluation, error) {
	var evals []models.Evaluation
	if err := s.db.WithContext(ctx).
		Where("id = ? AND status = ?", id, models.StatusCompleted).
		Limit(1).Find(&evals).Error; err != nil {
		return nil, fmt.Errorf("load evaluation: %w", err)
	}
	if len(evals) == 0 {
		return nil, nil
	}
	e := evals[0]

	var scores []models.EvaluationScore
	if err := s.db.WithContext(ctx).
		Where("evaluation_id = ?", id).
		Order("criterion").
		Find(&scores).Error; err != nil {
		return nil, fmt.Errorf("load evaluation scores: %w", err)
	}

	return &Evaluation{
		ID:            e.ID,
		CVID:          e.CVID,
		OverallScore:  e.OverallScore,
		Breakdown:     orderBreakdown(scores),
		Strengths:     nonNil(e.Strengths),
		MissingSkills: nonNil(e.MissingSkills),
		Summary:       e.Summary,
		Requirements:  matcher.DecodeRequirements(e.RawResponse),
		Model:         e.Model,
		PromptVersion: e.PromptVersion,
		EvaluatedAt:   e.UpdatedAt,
	}, nil
}

// orderBreakdown puts scores, sorted by criterion, in the order of
// entities.EvaluationCriteria, followed by any others.
func orderBreakdown(scores []models.EvaluationScore) []CriterionScore {
	out := make([]CriterionScore, 0, len(scores))
	used := make([]bool, len(scores))
	for _, c := range entities.EvaluationCriteria {
		for i, sc := range scores {
			if !used[i] && sc.Criterion == c.Name {
				used[i] = true
				out = append(out, CriterionScore{Criterion: sc.Criterion, Score: sc.Score, Reason: sc.Reason})
			}
		}
	}
	for i, sc := range scores {
		if !used[i] {
			out = append(out, CriterionScore{Criterion: sc.Criterion, Score: sc.Score, Reason: sc.Reason})
		}
	}
	return out
}

func nonNil(l models.StringList) []string {
	if l == nil {
		return []string{}
	}
	return l
}

// UpdateStatus sets one of userID's results' status and returns the result.
func (s *Service) UpdateStatus(ctx context.Context, userID, id ulid.ULID, status string) (_ *Detail, err error) {
	ctx, end := telemetry.Start(ctx, "results.Service.UpdateStatus", telemetry.ID(telemetry.AttrUserID, userID), telemetry.ID(telemetry.AttrResultID, id), attribute.String(telemetry.AttrStatus, status))
	defer end(&err)
	statuses, err := oneOf("status", []string{status}, models.SearchResultStatuses)
	if err != nil {
		return nil, err
	}
	if len(statuses) == 0 {
		return nil, missing("status", "The status parameter is required.")
	}
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}

	if err := s.db.WithContext(ctx).Model(&models.SearchResult{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{"status": statuses[0], "updated_at": s.now()}).Error; err != nil {
		return nil, fmt.Errorf("update search result: %w", err)
	}
	return s.Get(ctx, userID, id)
}

// ReEvaluate queues one of userID's results for evaluation again, against
// the CV the pipeline picks then. The current evaluation stays until the new
// one replaces it. A result already waiting for its next attempt is made due
// now. It returns ErrEvaluationInProgress while the result is being
// evaluated or is already due.
func (s *Service) ReEvaluate(ctx context.Context, userID, id ulid.ULID) (_ *Detail, err error) {
	ctx, end := telemetry.Start(ctx, "results.Service.ReEvaluate", telemetry.ID(telemetry.AttrUserID, userID), telemetry.ID(telemetry.AttrResultID, id))
	defer end(&err)
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}

	now := s.now()
	res := s.db.WithContext(ctx).Model(&models.SearchResult{}).
		Where("id = ? AND user_id = ?", id, userID).
		Where("(evaluation_status IN ? OR (evaluation_status = ? AND next_attempt_at > ?))",
			[]string{models.EvaluationDone, models.EvaluationFailed}, models.EvaluationPending, now).
		Updates(map[string]any{
			"evaluation_status": models.EvaluationPending,
			"attempts":          0,
			"next_attempt_at":   nil,
			"claimed_at":        nil,
			"last_error":        "",
			"updated_at":        now,
		})
	if res.Error != nil {
		return nil, fmt.Errorf("re-evaluate search result: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrEvaluationInProgress
	}
	return s.Get(ctx, userID, id)
}

// Summary counts userID's results.
func (s *Service) Summary(ctx context.Context, userID ulid.ULID) (_ *Summary, err error) {
	ctx, end := telemetry.Start(ctx, "results.Service.Summary", telemetry.ID(telemetry.AttrUserID, userID))
	defer end(&err)
	weekStart := s.now().Add(-7 * 24 * time.Hour)

	var totals struct {
		Total               int64
		Scored              int64
		Evaluating          int64
		FailedEvaluation    int64
		NewThisWeek         int64
		NewThisWeekSearches int64
		StrongMatches       int64
		StrongMatchesNew    int64
		New                 int64
		Saved               int64
		Applied             int64
		Dismissed           int64
	}
	if err := s.visible(ctx, userID).
		Select(`count(*) AS total,
			count(*) FILTER (WHERE sr.overall_score IS NOT NULL) AS scored,
			count(*) FILTER (WHERE sr.evaluation_status IN @evaluating) AS evaluating,
			count(*) FILTER (WHERE sr.evaluation_status = @failed) AS failed_evaluation,
			count(*) FILTER (WHERE sr.created_at >= @week) AS new_this_week,
			count(DISTINCT sr.job_search_id) FILTER (WHERE sr.created_at >= @week) AS new_this_week_searches,
			count(*) FILTER (WHERE sr.overall_score >= @strong AND sr.status <> @dismissed) AS strong_matches,
			count(*) FILTER (WHERE sr.overall_score >= @strong AND sr.status = @new) AS strong_matches_new,
			count(*) FILTER (WHERE sr.status = @new) AS new,
			count(*) FILTER (WHERE sr.status = @saved) AS saved,
			count(*) FILTER (WHERE sr.status = @applied) AS applied,
			count(*) FILTER (WHERE sr.status = @dismissed) AS dismissed`,
			map[string]any{
				"evaluating": []string{models.EvaluationPending, models.EvaluationProcessing},
				"failed":     models.EvaluationFailed,
				"week":       weekStart,
				"strong":     StrongScore,
				"new":        models.SearchResultNew,
				"saved":      models.SearchResultSaved,
				"applied":    models.SearchResultApplied,
				"dismissed":  models.SearchResultDismissed,
			}).
		Scan(&totals).Error; err != nil {
		return nil, fmt.Errorf("summarise search results: %w", err)
	}

	var bySearch []SearchCount
	if err := s.db.WithContext(ctx).
		Table("job_searches js").
		Select(`js.id, js.name, count(sr.id) AS total, count(sr.id) FILTER (WHERE sr.created_at >= ?) AS new_this_week`, weekStart).
		Joins("LEFT JOIN (search_results sr JOIN jobs j ON j.id = sr.job_id AND j.deleted_at IS NULL) ON sr.job_search_id = js.id").
		Where("js.user_id = ? AND js.deleted_at IS NULL", userID).
		Group("js.id, js.name, js.created_at").
		Order("js.created_at, js.id").
		Scan(&bySearch).Error; err != nil {
		return nil, fmt.Errorf("count search results by search: %w", err)
	}
	if bySearch == nil {
		bySearch = []SearchCount{}
	}

	return &Summary{
		WeekStart:           weekStart,
		Total:               totals.Total,
		Scored:              totals.Scored,
		Evaluating:          totals.Evaluating,
		FailedEvaluation:    totals.FailedEvaluation,
		NewThisWeek:         totals.NewThisWeek,
		NewThisWeekSearches: totals.NewThisWeekSearches,
		StrongMatches:       totals.StrongMatches,
		StrongMatchesNew:    totals.StrongMatchesNew,
		ByStatus: map[string]int64{
			models.SearchResultNew:       totals.New,
			models.SearchResultSaved:     totals.Saved,
			models.SearchResultApplied:   totals.Applied,
			models.SearchResultDismissed: totals.Dismissed,
		},
		BySearch: bySearch,
	}, nil
}
