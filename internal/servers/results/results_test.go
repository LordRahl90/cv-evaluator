package results

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/providers"
	"cv-evaluator/internal/servers/apierror"
	jobsearchhttp "cv-evaluator/internal/servers/jobsearches"
	"cv-evaluator/internal/servers/middleware"
	"cv-evaluator/internal/services/matcher"
	resultsvc "cv-evaluator/internal/services/results"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var testJWTSecret = []byte("results-handler-test-secret")

const testAPIKey = "sk_results-handler-key"

var (
	found     = time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	evaluated = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
)

func newTestRouter(svc Service, apiKeyUser ulid.ULID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	verifier := middleware.APIKeyVerifierFunc(func(_ context.Context, rawKey string) (ulid.ULID, error) {
		if rawKey != testAPIKey {
			return ulid.ULID{}, middleware.ErrInvalidToken
		}
		return apiKeyUser, nil
	})
	New(r, svc, testJWTSecret, middleware.WithAPIKeyVerifier(verifier))
	return r
}

func bearerJWT(t *testing.T, userID ulid.ULID) string {
	t.Helper()
	now := time.Now()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(testJWTSecret)
	require.NoError(t, err)
	return "Bearer " + tok
}

func doRequest(router *gin.Engine, method, path, auth, body string) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) apierror.Response {
	t.Helper()
	var body apierror.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func ptr[T any](v T) *T { return &v }

var (
	searchID = ulid.MustParse("01JSEARCH0000000000000000A")
	jobID    = ulid.MustParse("01JJ0B00000000000000000000")
	resultID = ulid.MustParse("01JRESXT000000000000000000")
	evalID   = ulid.MustParse("01JEVA1000000000000000000A")
	cvID     = ulid.MustParse("01JCV00000000000000000000A")
)

func sampleJob() resultsvc.Job {
	return resultsvc.Job{
		ID: jobID, Title: "Senior Go Engineer", Company: "Northwind", Location: "Copenhagen",
		Provider: "linkedin", DetailsURL: "https://example.com/1", PostedAt: "2 days ago",
		Description: "Build Go services.",
	}
}

func sampleDetail() *resultsvc.Detail {
	return &resultsvc.Detail{
		ID: resultID, Status: "new", EvaluationStatus: "done", CreatedAt: found, UpdatedAt: found,
		Search: resultsvc.SearchRef{ID: searchID, Name: "Senior Go"},
		Job:    sampleJob(),
		Evaluation: &resultsvc.Evaluation{
			ID: evalID, CVID: cvID, OverallScore: 91,
			Breakdown:     []resultsvc.CriterionScore{{Criterion: "skills", Score: 94, Reason: "Go"}},
			Strengths:     []string{"Go"},
			MissingSkills: []string{"Terraform"},
			Summary:       "A close fit.",
			Requirements:  []matcher.Requirement{{Requirement: "Go", Importance: "must", Met: "yes", Evidence: "6 years"}},
			Model:         "deepseek-chat", PromptVersion: "v4", EvaluatedAt: evaluated,
		},
	}
}

const sampleDetailJSON = `{
	"object":"search_result","id":"01JRESXT000000000000000000","status":"new","evaluation_status":"done",
	"created_at":"2026-03-01T08:00:00Z","updated_at":"2026-03-01T08:00:00Z",
	"search":{"id":"01JSEARCH0000000000000000A","name":"Senior Go"},
	"job":{"id":"01JJ0B00000000000000000000","title":"Senior Go Engineer","company":"Northwind","location":"Copenhagen",
		"provider":"linkedin","details_url":"https://example.com/1","posted_at":"2 days ago","description":"Build Go services."},
	"evaluation":{"id":"01JEVA1000000000000000000A","cv_id":"01JCV00000000000000000000A","overall_score":91,
		"breakdown":[{"criterion":"skills","score":94,"reason":"Go"}],
		"strengths":["Go"],"missing_skills":["Terraform"],"summary":"A close fit.",
		"requirements":[{"requirement":"Go","importance":"must","met":"yes","evidence":"6 years"}],
		"model":"deepseek-chat","prompt_version":"v4","evaluated_at":"2026-03-01T09:00:00Z"}
}`

// ── GET /results ────────────────────────────────────────────────────────────

func TestList_Success(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	job := sampleJob()
	job.Description = ""
	page := &resultsvc.Page{
		HasMore: true, TotalCount: 31,
		Results: []resultsvc.Result{
			{
				ID: resultID, Status: "new", EvaluationStatus: "done", CreatedAt: found, UpdatedAt: found,
				Search: resultsvc.SearchRef{ID: searchID, Name: "Senior Go"}, Job: job,
				Evaluation: &resultsvc.EvaluationSummary{OverallScore: 91, TopMissingSkills: []string{"Terraform"}, EvaluatedAt: evaluated},
			},
			{
				ID: evalID, Status: "saved", EvaluationStatus: "pending", CreatedAt: found, UpdatedAt: found,
				Search: resultsvc.SearchRef{ID: searchID, Name: "Senior Go"}, Job: job,
			},
		},
	}
	svc.On("List", mock.Anything, userID, resultsvc.ListParams{}).Return(page, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/results", bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	jobJSON := `{"id":"01JJ0B00000000000000000000","title":"Senior Go Engineer","company":"Northwind","location":"Copenhagen",
		"provider":"linkedin","details_url":"https://example.com/1","posted_at":"2 days ago"}`
	assert.JSONEq(t, `{"object":"list","has_more":true,"total_count":31,"data":[
		{"object":"search_result","id":"01JRESXT000000000000000000","status":"new","evaluation_status":"done",
		 "created_at":"2026-03-01T08:00:00Z","updated_at":"2026-03-01T08:00:00Z",
		 "search":{"id":"01JSEARCH0000000000000000A","name":"Senior Go"},"job":`+jobJSON+`,
		 "evaluation":{"overall_score":91,"top_missing_skills":["Terraform"],"evaluated_at":"2026-03-01T09:00:00Z"}},
		{"object":"search_result","id":"01JEVA1000000000000000000A","status":"saved","evaluation_status":"pending",
		 "created_at":"2026-03-01T08:00:00Z","updated_at":"2026-03-01T08:00:00Z",
		 "search":{"id":"01JSEARCH0000000000000000A","name":"Senior Go"},"job":`+jobJSON+`,"evaluation":null}
	]}`, w.Body.String())
	svc.AssertExpectations(t)
}

func TestList_ParsesQuery(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	after := ulid.Make()
	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	svc.On("List", mock.Anything, userID, resultsvc.ListParams{
		MinScore:           ptr(70),
		Statuses:           []string{"new", "saved", "applied"},
		EvaluationStatuses: []string{"done"},
		Since:              &since,
		Criterion:          "skills",
		MinCriterionScore:  ptr(80),
		Sort:               "newest",
		Limit:              5,
		StartingAfter:      &after,
	}).Return(&resultsvc.Page{}, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet,
		"/results?min_score=70&status=new,saved&status=applied&evaluation_status=done&since=2026-03-01T00:00:00Z"+
			"&criterion=skills&min_criterion_score=80&sort=newest&limit=5&starting_after="+after.String(),
		bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"object":"list","data":[],"has_more":false,"total_count":0}`, w.Body.String())
	svc.AssertExpectations(t)
}

func TestList_BadQuery(t *testing.T) {
	cases := []struct{ query, param string }{
		{"min_score=high", "min_score"},
		{"min_criterion_score=x", "min_criterion_score"},
		{"limit=ten", "limit"},
		{"limit=0", "limit"},
		{"since=yesterday", "since"},
		{"starting_after=nope", "starting_after"},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			svc := new(MockService)
			w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/results?"+tc.query, bearerJWT(t, ulid.Make()), "")

			require.Equal(t, http.StatusBadRequest, w.Code)
			body := decodeError(t, w)
			assert.Equal(t, apierror.CodeParameterInvalid, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
			svc.AssertNotCalled(t, "List")
		})
	}
}

func TestList_ValidationError(t *testing.T) {
	svc := new(MockService)
	svc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &resultsvc.ValidationError{Param: "status", Message: "bad status"}).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/results?status=archived", bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := decodeError(t, w)
	assert.Equal(t, "invalid_request_error", body.Error.Type)
	assert.Equal(t, apierror.CodeParameterInvalid, body.Error.Code)
	assert.Equal(t, "status", body.Error.Param)
}

func TestList_RequiresAuth(t *testing.T) {
	svc := new(MockService)
	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/results", "", "")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "List")
}

func TestList_AcceptsAPIKey(t *testing.T) {
	svc := new(MockService)
	keyUser := ulid.Make()
	svc.On("List", mock.Anything, keyUser, resultsvc.ListParams{}).Return(&resultsvc.Page{}, nil).Once()

	w := doRequest(newTestRouter(svc, keyUser), http.MethodGet, "/results", "Bearer "+testAPIKey, "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	svc.AssertExpectations(t)
}

func TestList_InternalError(t *testing.T) {
	svc := new(MockService)
	svc.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("pg down")).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/results", bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "pg down")
}

// ── GET /job-searches/:id/results ───────────────────────────────────────────

func TestListForSearch(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	svc.On("List", mock.Anything, userID, resultsvc.ListParams{JobSearchID: &searchID, MinScore: ptr(80)}).
		Return(&resultsvc.Page{}, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet,
		"/job-searches/"+searchID.String()+"/results?min_score=80", bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	svc.AssertExpectations(t)
}

func TestListForSearch_NotFoundAndBadID(t *testing.T) {
	svc := new(MockService)
	svc.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil, resultsvc.ErrSearchNotFound).Once()
	router := newTestRouter(svc, ulid.Make())

	w := doRequest(router, http.MethodGet, "/job-searches/"+ulid.Make().String()+"/results", bearerJWT(t, ulid.Make()), "")
	require.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, apierror.CodeResourceMissing, decodeError(t, w).Error.Code)

	w = doRequest(router, http.MethodGet, "/job-searches/nope/results", bearerJWT(t, ulid.Make()), "")
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "id", decodeError(t, w).Error.Param)
}

func TestRoutesCoexistWithJobSearchRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	assert.NotPanics(t, func() {
		jobsearchhttp.New(r, nil, providers.Default(), testJWTSecret, nil)
		New(r, new(MockService), testJWTSecret)
	})
}

// ── GET /results/:id ────────────────────────────────────────────────────────

func TestGet_Success(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	svc.On("Get", mock.Anything, userID, resultID).Return(sampleDetail(), nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/results/"+resultID.String(), bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, sampleDetailJSON, w.Body.String())
	svc.AssertExpectations(t)
}

func TestGet_NoEvaluationOrRequirements(t *testing.T) {
	svc := new(MockService)
	d := sampleDetail()
	d.Evaluation.Requirements = nil
	svc.On("Get", mock.Anything, mock.Anything, resultID).Return(d, nil).Once()
	pending := sampleDetail()
	pending.Evaluation = nil
	other := ulid.Make()
	pending.ID = other
	svc.On("Get", mock.Anything, mock.Anything, other).Return(pending, nil).Once()
	router := newTestRouter(svc, ulid.Make())

	w := doRequest(router, http.MethodGet, "/results/"+resultID.String(), bearerJWT(t, ulid.Make()), "")
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Evaluation map[string]any `json:"evaluation"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body.Evaluation, "requirements")
	assert.Nil(t, body.Evaluation["requirements"], "older evaluations have null requirements")

	w = doRequest(router, http.MethodGet, "/results/"+other.String(), bearerJWT(t, ulid.Make()), "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"evaluation":null`)
}

func TestGet_NotFoundAndBadID(t *testing.T) {
	svc := new(MockService)
	svc.On("Get", mock.Anything, mock.Anything, mock.Anything).Return(nil, resultsvc.ErrNotFound).Once()
	router := newTestRouter(svc, ulid.Make())

	w := doRequest(router, http.MethodGet, "/results/"+ulid.Make().String(), bearerJWT(t, ulid.Make()), "")
	require.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "No such result.", decodeError(t, w).Error.Message)

	w = doRequest(router, http.MethodGet, "/results/nope", bearerJWT(t, ulid.Make()), "")
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "id", decodeError(t, w).Error.Param)
}

// ── PATCH /results/:id ──────────────────────────────────────────────────────

func TestUpdate_Success(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	d := sampleDetail()
	d.Status = "saved"
	svc.On("UpdateStatus", mock.Anything, userID, resultID, "saved").Return(d, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPatch, "/results/"+resultID.String(),
		bearerJWT(t, userID), `{"status":"saved"}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"status":"saved"`)
	svc.AssertExpectations(t)
}

func TestUpdate_BadRequests(t *testing.T) {
	svc := new(MockService)
	svc.On("UpdateStatus", mock.Anything, mock.Anything, mock.Anything, "archived").
		Return(nil, &resultsvc.ValidationError{Param: "status", Message: "bad"}).Once()
	router := newTestRouter(svc, ulid.Make())
	path := "/results/" + resultID.String()

	w := doRequest(router, http.MethodPatch, path, bearerJWT(t, ulid.Make()), `{`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, apierror.CodeInvalidJSON, decodeError(t, w).Error.Code)

	w = doRequest(router, http.MethodPatch, path, bearerJWT(t, ulid.Make()), `{}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, apierror.CodeParameterMissing, decodeError(t, w).Error.Code)

	w = doRequest(router, http.MethodPatch, path, bearerJWT(t, ulid.Make()), `{"status":"archived"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "status", decodeError(t, w).Error.Param)
	svc.AssertExpectations(t)
}

// ── POST /results/:id/re-evaluate ───────────────────────────────────────────

func TestReEvaluate(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	d := sampleDetail()
	d.EvaluationStatus = "pending"
	busy := ulid.Make()
	svc.On("ReEvaluate", mock.Anything, userID, resultID).Return(d, nil).Once()
	svc.On("ReEvaluate", mock.Anything, userID, busy).Return(nil, resultsvc.ErrEvaluationInProgress).Once()
	router := newTestRouter(svc, ulid.Make())

	w := doRequest(router, http.MethodPost, "/results/"+resultID.String()+"/re-evaluate", bearerJWT(t, userID), "")
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"evaluation_status":"pending"`)

	w = doRequest(router, http.MethodPost, "/results/"+busy.String()+"/re-evaluate", bearerJWT(t, userID), "")
	require.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, CodeEvaluationInProgress, decodeError(t, w).Error.Code)
	svc.AssertExpectations(t)
}

// ── GET /results/summary ────────────────────────────────────────────────────

func TestSummary(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	svc.On("Summary", mock.Anything, userID).Return(&resultsvc.Summary{
		WeekStart: found, Total: 84, Scored: 80, Evaluating: 3, FailedEvaluation: 1,
		NewThisWeek: 14, NewThisWeekSearches: 3, StrongMatches: 5, StrongMatchesNew: 2,
		ByStatus: map[string]int64{"new": 70, "saved": 8, "applied": 4, "dismissed": 2},
		BySearch: []resultsvc.SearchCount{{ID: searchID, Name: "Senior Go", Total: 23, NewThisWeek: 9}},
	}, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/results/summary", bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{
		"object":"results_summary","week_start":"2026-03-01T08:00:00Z","total":84,"scored":80,"evaluating":3,
		"failed_evaluation":1,"new_this_week":14,"new_this_week_searches":3,"strong_score":80,
		"strong_matches":5,"strong_matches_new":2,
		"by_status":{"new":70,"saved":8,"applied":4,"dismissed":2},
		"by_search":[{"id":"01JSEARCH0000000000000000A","name":"Senior Go","total":23,"new_this_week":9}]
	}`, w.Body.String())
	svc.AssertExpectations(t)
}
