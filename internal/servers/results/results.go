// Package results exposes routes for reading and triaging the caller's
// search results: the jobs their saved searches found, scored against their
// CV.
package results

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"
	resultsvc "cv-evaluator/internal/services/results"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

// CodeEvaluationInProgress is returned when re-evaluating a result that is
// already waiting for, or undergoing, evaluation.
const CodeEvaluationInProgress = "evaluation_in_progress"

// Service is the domain interface the handler depends on.
// *resultsvc.Service satisfies it.
type Service interface {
	List(ctx context.Context, userID ulid.ULID, p resultsvc.ListParams) (*resultsvc.Page, error)
	Get(ctx context.Context, userID, id ulid.ULID) (*resultsvc.Detail, error)
	UpdateStatus(ctx context.Context, userID, id ulid.ULID, status string) (*resultsvc.Detail, error)
	ReEvaluate(ctx context.Context, userID, id ulid.ULID) (*resultsvc.Detail, error)
	Summary(ctx context.Context, userID ulid.ULID) (*resultsvc.Summary, error)
}

type updateRequest struct {
	Status *string `json:"status"`
}

// SearchRefResponse names the search that found a result.
type SearchRefResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// JobResponse is the posting a result is about. Description is only set on
// a single result.
type JobResponse struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Company     string  `json:"company"`
	Location    string  `json:"location"`
	Provider    string  `json:"provider"`
	DetailsURL  string  `json:"details_url"`
	PostedAt    string  `json:"posted_at"`
	Description *string `json:"description,omitempty"`
}

// EvaluationSummaryResponse is what a listed result shows of its
// evaluation.
type EvaluationSummaryResponse struct {
	OverallScore     int       `json:"overall_score"`
	TopMissingSkills []string  `json:"top_missing_skills"`
	EvaluatedAt      time.Time `json:"evaluated_at"`
}

// ResultResponse is one listed search result.
type ResultResponse struct {
	Object           string                     `json:"object"`
	ID               string                     `json:"id"`
	Status           string                     `json:"status"`
	EvaluationStatus string                     `json:"evaluation_status"`
	CreatedAt        time.Time                  `json:"created_at"`
	UpdatedAt        time.Time                  `json:"updated_at"`
	Search           SearchRefResponse          `json:"search"`
	Job              JobResponse                `json:"job"`
	Evaluation       *EvaluationSummaryResponse `json:"evaluation"`
}

// ListResponse is one page of results.
type ListResponse struct {
	Object     string           `json:"object"`
	Data       []ResultResponse `json:"data"`
	HasMore    bool             `json:"has_more"`
	TotalCount int64            `json:"total_count"`
}

// CriterionScoreResponse is one criterion's score.
type CriterionScoreResponse struct {
	Criterion string `json:"criterion"`
	Score     int    `json:"score"`
	Reason    string `json:"reason"`
}

// RequirementResponse is one job requirement and whether the CV meets it.
type RequirementResponse struct {
	Requirement string `json:"requirement"`
	Importance  string `json:"importance"`
	Met         string `json:"met"`
	Evidence    string `json:"evidence"`
}

// EvaluationResponse is a result's full current evaluation.
type EvaluationResponse struct {
	ID            string                   `json:"id"`
	CVID          string                   `json:"cv_id"`
	OverallScore  int                      `json:"overall_score"`
	Breakdown     []CriterionScoreResponse `json:"breakdown"`
	Strengths     []string                 `json:"strengths"`
	MissingSkills []string                 `json:"missing_skills"`
	Summary       string                   `json:"summary"`
	Requirements  []RequirementResponse    `json:"requirements"`
	Model         string                   `json:"model"`
	PromptVersion string                   `json:"prompt_version"`
	EvaluatedAt   time.Time                `json:"evaluated_at"`
}

// DetailResponse is one search result with its full job and evaluation.
type DetailResponse struct {
	Object           string              `json:"object"`
	ID               string              `json:"id"`
	Status           string              `json:"status"`
	EvaluationStatus string              `json:"evaluation_status"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	Search           SearchRefResponse   `json:"search"`
	Job              JobResponse         `json:"job"`
	Evaluation       *EvaluationResponse `json:"evaluation"`
}

// SearchCountResponse is one search's share of the caller's results.
type SearchCountResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Total       int64  `json:"total"`
	NewThisWeek int64  `json:"new_this_week"`
}

// SummaryResponse counts the caller's results.
type SummaryResponse struct {
	Object              string                `json:"object"`
	WeekStart           time.Time             `json:"week_start"`
	Total               int64                 `json:"total"`
	Scored              int64                 `json:"scored"`
	Evaluating          int64                 `json:"evaluating"`
	FailedEvaluation    int64                 `json:"failed_evaluation"`
	NewThisWeek         int64                 `json:"new_this_week"`
	NewThisWeekSearches int64                 `json:"new_this_week_searches"`
	StrongScore         int                   `json:"strong_score"`
	StrongMatches       int64                 `json:"strong_matches"`
	StrongMatchesNew    int64                 `json:"strong_matches_new"`
	ByStatus            map[string]int64      `json:"by_status"`
	BySearch            []SearchCountResponse `json:"by_search"`
}

// Handler wires the Service to HTTP routes.
type Handler struct {
	svc Service
}

// New registers the result routes on router, all authenticated and scoped
// to the caller:
//
//	GET   /results
//	GET   /results/summary
//	GET   /results/:id
//	PATCH /results/:id
//	POST  /results/:id/re-evaluate
//	GET   /job-searches/:id/results
//
// Requests authenticate with a JWT; opts configure the auth middleware the
// same way as for the other handlers (for example accepting API keys).
func New(router gin.IRouter, svc Service, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{svc: svc}
	auth := middleware.Authenticate(jwtSecret, opts...)

	group := router.Group("/results", auth)
	group.GET("", h.list)
	group.GET("/summary", h.summary)
	group.GET("/:id", h.get)
	group.PATCH("/:id", h.update)
	group.POST("/:id/re-evaluate", h.reEvaluate)

	router.GET("/job-searches/:id/results", auth, h.listForSearch)

	return h
}

func (h *Handler) list(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	h.respondList(c, userID, nil)
}

func (h *Handler) listForSearch(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	searchID, err := ulid.ParseStrict(c.Param("id"))
	if err != nil {
		apierror.BadRequest(c, apierror.CodeParameterInvalid, "The id parameter must be a valid job search id.", "id")
		return
	}
	h.respondList(c, userID, &searchID)
}

func (h *Handler) respondList(c *gin.Context, userID ulid.ULID, searchID *ulid.ULID) {
	p, ok := parseListParams(c)
	if !ok {
		return
	}
	p.JobSearchID = searchID

	page, err := h.svc.List(c.Request.Context(), userID, p)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	data := make([]ResultResponse, 0, len(page.Results))
	for i := range page.Results {
		data = append(data, toResultResponse(&page.Results[i]))
	}
	c.JSON(http.StatusOK, ListResponse{Object: "list", Data: data, HasMore: page.HasMore, TotalCount: page.TotalCount})
}

func (h *Handler) summary(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}

	s, err := h.svc.Summary(c.Request.Context(), userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	bySearch := make([]SearchCountResponse, 0, len(s.BySearch))
	for _, sc := range s.BySearch {
		bySearch = append(bySearch, SearchCountResponse{ID: sc.ID.String(), Name: sc.Name, Total: sc.Total, NewThisWeek: sc.NewThisWeek})
	}
	c.JSON(http.StatusOK, SummaryResponse{
		Object:              "results_summary",
		WeekStart:           s.WeekStart,
		Total:               s.Total,
		Scored:              s.Scored,
		Evaluating:          s.Evaluating,
		FailedEvaluation:    s.FailedEvaluation,
		NewThisWeek:         s.NewThisWeek,
		NewThisWeekSearches: s.NewThisWeekSearches,
		StrongScore:         resultsvc.StrongScore,
		StrongMatches:       s.StrongMatches,
		StrongMatchesNew:    s.StrongMatchesNew,
		ByStatus:            s.ByStatus,
		BySearch:            bySearch,
	})
}

func (h *Handler) get(c *gin.Context) {
	userID, id, ok := requireUserAndID(c)
	if !ok {
		return
	}

	d, err := h.svc.Get(c.Request.Context(), userID, id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, toDetailResponse(d))
}

func (h *Handler) update(c *gin.Context) {
	userID, id, ok := requireUserAndID(c)
	if !ok {
		return
	}

	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.BadRequest(c, apierror.CodeInvalidJSON, "The request body must be a valid JSON object.", "")
		return
	}
	if req.Status == nil {
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The status parameter is required.", "status")
		return
	}

	d, err := h.svc.UpdateStatus(c.Request.Context(), userID, id, *req.Status)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, toDetailResponse(d))
}

func (h *Handler) reEvaluate(c *gin.Context) {
	userID, id, ok := requireUserAndID(c)
	if !ok {
		return
	}

	d, err := h.svc.ReEvaluate(c.Request.Context(), userID, id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, toDetailResponse(d))
}

// parseListParams reads the list query parameters, writing a 400 and
// returning false when one has the wrong type. The service checks values.
func parseListParams(c *gin.Context) (resultsvc.ListParams, bool) {
	var p resultsvc.ListParams
	var ok bool

	if p.MinScore, ok = queryInt(c, "min_score"); !ok {
		return p, false
	}
	if p.MinCriterionScore, ok = queryInt(c, "min_criterion_score"); !ok {
		return p, false
	}
	limit, ok := queryInt(c, "limit")
	if !ok {
		return p, false
	}
	if limit != nil {
		if *limit < 1 {
			apierror.BadRequest(c, apierror.CodeParameterInvalid, "The limit parameter must be between 1 and 100.", "limit")
			return p, false
		}
		p.Limit = *limit
	}

	p.Statuses = queryList(c, "status")
	p.EvaluationStatuses = queryList(c, "evaluation_status")
	p.Criterion = c.Query("criterion")
	p.Sort = strings.TrimSpace(c.Query("sort"))

	if v := strings.TrimSpace(c.Query("since")); v != "" {
		since, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierror.BadRequest(c, apierror.CodeParameterInvalid, "The since parameter must be an RFC 3339 timestamp.", "since")
			return p, false
		}
		p.Since = &since
	}
	if v := strings.TrimSpace(c.Query("starting_after")); v != "" {
		id, err := ulid.ParseStrict(v)
		if err != nil {
			apierror.BadRequest(c, apierror.CodeParameterInvalid, "The starting_after parameter must be a result id.", "starting_after")
			return p, false
		}
		p.StartingAfter = &id
	}
	return p, true
}

// queryInt reads an optional integer query parameter.
func queryInt(c *gin.Context, name string) (*int, bool) {
	v := strings.TrimSpace(c.Query(name))
	if v == "" {
		return nil, true
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		apierror.BadRequest(c, apierror.CodeParameterInvalid, "The "+name+" parameter must be an integer.", name)
		return nil, false
	}
	return &n, true
}

// queryList reads a parameter given repeatedly, as a comma separated list,
// or both.
func queryList(c *gin.Context, name string) []string {
	var out []string
	for _, v := range c.QueryArray(name) {
		out = append(out, strings.Split(v, ",")...)
	}
	return out
}

func requireUser(c *gin.Context) (ulid.ULID, bool) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
	}
	return userID, ok
}

func requireUserAndID(c *gin.Context) (ulid.ULID, ulid.ULID, bool) {
	userID, ok := requireUser(c)
	if !ok {
		return ulid.ULID{}, ulid.ULID{}, false
	}
	id, err := ulid.ParseStrict(c.Param("id"))
	if err != nil {
		apierror.BadRequest(c, apierror.CodeParameterInvalid, "The id parameter must be a valid result id.", "id")
		return ulid.ULID{}, ulid.ULID{}, false
	}
	return userID, id, true
}

func handleServiceError(c *gin.Context, err error) {
	var verr *resultsvc.ValidationError
	switch {
	case errors.As(err, &verr):
		code := apierror.CodeParameterInvalid
		if verr.Missing {
			code = apierror.CodeParameterMissing
		}
		apierror.BadRequest(c, code, verr.Message, verr.Param)
	case errors.Is(err, resultsvc.ErrNotFound):
		apierror.NotFound(c, "No such result.", "id")
	case errors.Is(err, resultsvc.ErrSearchNotFound):
		apierror.NotFound(c, "No such job search.", "id")
	case errors.Is(err, resultsvc.ErrEvaluationInProgress):
		apierror.Conflict(c, CodeEvaluationInProgress, "The result is already queued for or undergoing evaluation.", "")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c, err)
	default:
		slog.ErrorContext(c.Request.Context(), "results handler", "error", err)
		apierror.InternalServerError(c, err)
	}
}

func toSearchRef(s resultsvc.SearchRef) SearchRefResponse {
	return SearchRefResponse{ID: s.ID.String(), Name: s.Name}
}

func toJobResponse(j *resultsvc.Job, withDescription bool) JobResponse {
	resp := JobResponse{
		ID:         j.ID.String(),
		Title:      j.Title,
		Company:    j.Company,
		Location:   j.Location,
		Provider:   j.Provider,
		DetailsURL: j.DetailsURL,
		PostedAt:   j.PostedAt,
	}
	if withDescription {
		resp.Description = &j.Description
	}
	return resp
}

func toResultResponse(r *resultsvc.Result) ResultResponse {
	resp := ResultResponse{
		Object:           "search_result",
		ID:               r.ID.String(),
		Status:           r.Status,
		EvaluationStatus: r.EvaluationStatus,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
		Search:           toSearchRef(r.Search),
		Job:              toJobResponse(&r.Job, false),
	}
	if e := r.Evaluation; e != nil {
		resp.Evaluation = &EvaluationSummaryResponse{
			OverallScore:     e.OverallScore,
			TopMissingSkills: e.TopMissingSkills,
			EvaluatedAt:      e.EvaluatedAt,
		}
	}
	return resp
}

func toDetailResponse(d *resultsvc.Detail) DetailResponse {
	resp := DetailResponse{
		Object:           "search_result",
		ID:               d.ID.String(),
		Status:           d.Status,
		EvaluationStatus: d.EvaluationStatus,
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
		Search:           toSearchRef(d.Search),
		Job:              toJobResponse(&d.Job, true),
	}
	if e := d.Evaluation; e != nil {
		breakdown := make([]CriterionScoreResponse, 0, len(e.Breakdown))
		for _, s := range e.Breakdown {
			breakdown = append(breakdown, CriterionScoreResponse(s))
		}
		var requirements []RequirementResponse
		if e.Requirements != nil {
			requirements = make([]RequirementResponse, 0, len(e.Requirements))
			for _, r := range e.Requirements {
				requirements = append(requirements, RequirementResponse(r))
			}
		}
		resp.Evaluation = &EvaluationResponse{
			ID:            e.ID.String(),
			CVID:          e.CVID.String(),
			OverallScore:  e.OverallScore,
			Breakdown:     breakdown,
			Strengths:     e.Strengths,
			MissingSkills: e.MissingSkills,
			Summary:       e.Summary,
			Requirements:  requirements,
			Model:         e.Model,
			PromptVersion: e.PromptVersion,
			EvaluatedAt:   e.EvaluatedAt,
		}
	}
	return resp
}
