package jobsearches

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

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/providers"
	"cv-evaluator/internal/servers/middleware"
	jobsearchsvc "cv-evaluator/internal/services/jobsearches"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var testJWTSecret = []byte("jobsearches-handler-test-secret")

const testAPIKey = "sk_live_jobsearchhandlertest"

// bareProvider does not implement providers.Describer.
type bareProvider struct{}

func (bareProvider) Name() string { return "bare" }
func (bareProvider) Search(context.Context, providers.Criteria) ([]entities.JobPost, error) {
	return nil, nil
}
func (bareProvider) Details(context.Context, *entities.JobPost) (*entities.JobDetails, error) {
	return nil, nil
}

func newTestRouter(svc Service, apiKeyUser ulid.ULID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	verifier := middleware.APIKeyVerifierFunc(func(_ context.Context, rawKey string) (ulid.ULID, error) {
		if rawKey != testAPIKey {
			return ulid.ULID{}, middleware.ErrInvalidToken
		}
		return apiKeyUser, nil
	})
	New(r, svc, providers.Default(), testJWTSecret, verifier)
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

type apiErrorBody struct {
	Error struct {
		Type  string `json:"type"`
		Code  string `json:"code"`
		Param string `json:"param"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body
}

func sampleSearch(userID ulid.ULID) *models.JobSearch {
	next := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	return &models.JobSearch{
		ID:        ulid.Make(),
		CreatedAt: time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC),
		UserID:    userID,
		Name:      "Go in Copenhagen",
		Query:     "golang",
		Location:  "Copenhagen",
		Frequency: models.FrequencyDaily,
		Enabled:   true,
		NextRunAt: &next,
	}
}

func ptr[T any](v T) *T { return &v }

// ── POST /job-searches ──────────────────────────────────────────────────────

func TestCreate_Success(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	js := sampleSearch(userID)
	svc.On("Create", mock.Anything, userID, jobsearchsvc.CreateParams{
		Name: "Go in Copenhagen", Query: "golang", Location: "Copenhagen",
		Frequency: "daily", Enabled: ptr(true),
	}).Return(js, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/job-searches", bearerJWT(t, userID),
		`{"name":"Go in Copenhagen","query":"golang","location":"Copenhagen","frequency":"daily","enabled":true}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.JSONEq(t, `{
		"object":"job_search","id":"`+js.ID.String()+`","name":"Go in Copenhagen",
		"query":"golang","location":"Copenhagen","frequency":"daily","enabled":true,
		"last_run_at":null,"next_run_at":"2026-03-01T09:00:00Z",
		"created_at":"2026-03-01T08:00:00Z","updated_at":"2026-03-01T08:00:00Z"
	}`, w.Body.String())
	svc.AssertExpectations(t)
}

func TestCreate_EnabledOmitted(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	svc.On("Create", mock.Anything, userID, mock.MatchedBy(func(p jobsearchsvc.CreateParams) bool {
		return p.Enabled == nil && p.Query == "go" && p.Frequency == "" && p.Name == ""
	})).Return(sampleSearch(userID), nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/job-searches", bearerJWT(t, userID),
		`{"query":"go"}`)

	require.Equal(t, http.StatusCreated, w.Code)
	svc.AssertExpectations(t)
}

func TestCreate_InvalidJSON(t *testing.T) {
	svc := new(MockService)

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/job-searches", bearerJWT(t, ulid.Make()), `{`)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_json", decodeError(t, w).Error.Code)
	svc.AssertNotCalled(t, "Create")
}

func TestCreate_ValidationErrors(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		code  string
		param string
	}{
		{"missing", &jobsearchsvc.ValidationError{Param: "query", Missing: true, Message: "m"}, "parameter_missing", "query"},
		{"invalid", &jobsearchsvc.ValidationError{Param: "frequency", Message: "m"}, "parameter_invalid", "frequency"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(MockService)
			svc.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(nil, tc.err).Once()

			w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/job-searches", bearerJWT(t, ulid.Make()), `{}`)

			require.Equal(t, http.StatusBadRequest, w.Code)
			body := decodeError(t, w)
			assert.Equal(t, "invalid_request_error", body.Error.Type)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.param, body.Error.Param)
		})
	}
}

func TestCreate_InternalError(t *testing.T) {
	svc := new(MockService)
	svc.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("pg down")).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/job-searches", bearerJWT(t, ulid.Make()), `{}`)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "api_error", decodeError(t, w).Error.Type)
	assert.NotContains(t, w.Body.String(), "pg down")
}

func TestCreate_Timeout(t *testing.T) {
	svc := new(MockService)
	svc.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(nil, context.DeadlineExceeded).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/job-searches", bearerJWT(t, ulid.Make()), `{}`)

	require.Equal(t, http.StatusGatewayTimeout, w.Code)
	assert.Equal(t, "timeout", decodeError(t, w).Error.Code)
}

// ── GET /job-searches ───────────────────────────────────────────────────────

func TestList_WithAPIKey(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	searches := []models.JobSearch{*sampleSearch(userID), *sampleSearch(userID)}
	svc.On("List", mock.Anything, userID).Return(searches, nil).Once()

	w := doRequest(newTestRouter(svc, userID), http.MethodGet, "/job-searches", "Bearer "+testAPIKey, "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, searches[1].ID.String(), resp.Data[1].ID)
	assert.Equal(t, "job_search", resp.Data[0].Object)
	svc.AssertExpectations(t)
}

func TestList_EmptyIsArray(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	svc.On("List", mock.Anything, userID).Return([]models.JobSearch{}, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/job-searches", bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"object":"list","data":[]}`, w.Body.String())
}

func TestList_Error(t *testing.T) {
	svc := new(MockService)
	svc.On("List", mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/job-searches", bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── GET /job-searches/:id ───────────────────────────────────────────────────

func TestGet_Success(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	js := sampleSearch(userID)
	svc.On("Get", mock.Anything, userID, js.ID).Return(js, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/job-searches/"+js.ID.String(), bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	var resp JobSearchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, js.ID.String(), resp.ID)
	assert.Equal(t, "golang", resp.Query)
}

func TestGet_NotFound(t *testing.T) {
	svc := new(MockService)
	svc.On("Get", mock.Anything, mock.Anything, mock.Anything).Return(nil, jobsearchsvc.ErrNotFound).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodGet, "/job-searches/"+ulid.Make().String(), bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusNotFound, w.Code)
	body := decodeError(t, w)
	assert.Equal(t, "resource_missing", body.Error.Code)
	assert.Equal(t, "id", body.Error.Param)
}

func TestByID_InvalidID(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/job-searches/nope"},
		{http.MethodPatch, "/job-searches/nope"},
		{http.MethodDelete, "/job-searches/nope"},
		{http.MethodPost, "/job-searches/nope/run"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			svc := new(MockService)
			w := doRequest(newTestRouter(svc, ulid.Make()), tc.method, tc.path, bearerJWT(t, ulid.Make()), `{}`)

			require.Equal(t, http.StatusBadRequest, w.Code)
			body := decodeError(t, w)
			assert.Equal(t, "parameter_invalid", body.Error.Code)
			assert.Equal(t, "id", body.Error.Param)
		})
	}
}

// ── PATCH /job-searches/:id ─────────────────────────────────────────────────

func TestUpdate_Success(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	js := sampleSearch(userID)
	js.Enabled = false
	js.NextRunAt = nil
	svc.On("Update", mock.Anything, userID, js.ID, jobsearchsvc.UpdateParams{
		Enabled:  ptr(false),
		Location: ptr(""),
	}).Return(js, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPatch, "/job-searches/"+js.ID.String(), bearerJWT(t, userID),
		`{"enabled":false,"location":""}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["enabled"])
	assert.Nil(t, resp["next_run_at"])
	svc.AssertExpectations(t)
}

func TestUpdate_InvalidJSON(t *testing.T) {
	svc := new(MockService)

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPatch, "/job-searches/"+ulid.Make().String(), bearerJWT(t, ulid.Make()), `[`)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_json", decodeError(t, w).Error.Code)
	svc.AssertNotCalled(t, "Update")
}

func TestUpdate_ValidationError(t *testing.T) {
	svc := new(MockService)
	svc.On("Update", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &jobsearchsvc.ValidationError{Param: "frequency", Message: "bad frequency"}).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPatch, "/job-searches/"+ulid.Make().String(), bearerJWT(t, ulid.Make()), `{"frequency":"monthly"}`)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "frequency", decodeError(t, w).Error.Param)
}

// ── DELETE /job-searches/:id ────────────────────────────────────────────────

func TestDelete_Success(t *testing.T) {
	svc := new(MockService)
	userID, id := ulid.Make(), ulid.Make()
	svc.On("Delete", mock.Anything, userID, id).Return(nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodDelete, "/job-searches/"+id.String(), bearerJWT(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"object":"job_search","id":"`+id.String()+`","deleted":true}`, w.Body.String())
	svc.AssertExpectations(t)
}

func TestDelete_NotFound(t *testing.T) {
	svc := new(MockService)
	svc.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(jobsearchsvc.ErrNotFound).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodDelete, "/job-searches/"+ulid.Make().String(), bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusNotFound, w.Code)
}

// ── POST /job-searches/:id/run ──────────────────────────────────────────────

func TestRun_Accepted(t *testing.T) {
	svc := new(MockService)
	userID := ulid.Make()
	js := sampleSearch(userID)
	svc.On("RunNow", mock.Anything, userID, js.ID).Return(js, nil).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/job-searches/"+js.ID.String()+"/run", bearerJWT(t, userID), "")

	require.Equal(t, http.StatusAccepted, w.Code)
	var resp JobSearchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, js.ID.String(), resp.ID)
	svc.AssertExpectations(t)
}

func TestRun_Disabled(t *testing.T) {
	svc := new(MockService)
	svc.On("RunNow", mock.Anything, mock.Anything, mock.Anything).Return(nil, jobsearchsvc.ErrDisabled).Once()

	w := doRequest(newTestRouter(svc, ulid.Make()), http.MethodPost, "/job-searches/"+ulid.Make().String()+"/run", bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, CodeJobSearchDisabled, decodeError(t, w).Error.Code)
}

// ── GET /providers ──────────────────────────────────────────────────────────

func TestListProviders(t *testing.T) {
	w := doRequest(newTestRouter(new(MockService), ulid.Make()), http.MethodGet, "/providers", bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusOK, w.Code)
	var resp ProviderListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "jobindex", resp.Data[0].Name)
	assert.Equal(t, "Jobindex", resp.Data[0].DisplayName)
	assert.Equal(t, "Denmark", resp.Data[0].Coverage)
	assert.Equal(t, "https://www.jobindex.dk", resp.Data[0].Website)
	assert.Equal(t, "linkedin", resp.Data[1].Name)
	assert.Equal(t, "provider", resp.Data[1].Object)
	assert.Equal(t, "Worldwide", resp.Data[1].Coverage)
}

func TestListProviders_WithoutDescriber(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	New(r, new(MockService), providers.NewRegistry(bareProvider{}), testJWTSecret, nil)

	w := doRequest(r, http.MethodGet, "/providers", bearerJWT(t, ulid.Make()), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"object":"list","data":[{"object":"provider","name":"bare","display_name":"bare","website":"","coverage":""}]}`, w.Body.String())
}

// ── Authentication ──────────────────────────────────────────────────────────

func TestRoutes_RequireAuth(t *testing.T) {
	id := ulid.Make().String()
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/job-searches"},
		{http.MethodGet, "/job-searches"},
		{http.MethodGet, "/job-searches/" + id},
		{http.MethodPatch, "/job-searches/" + id},
		{http.MethodDelete, "/job-searches/" + id},
		{http.MethodPost, "/job-searches/" + id + "/run"},
		{http.MethodGet, "/providers"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			svc := new(MockService)
			router := newTestRouter(svc, ulid.Make())

			w := doRequest(router, tc.method, tc.path, "", "")
			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, "authentication_error", decodeError(t, w).Error.Type)

			w = doRequest(router, tc.method, tc.path, "Bearer sk_live_wrong", "")
			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, "invalid_api_key", decodeError(t, w).Error.Code)

			svc.AssertExpectations(t)
		})
	}
}

func TestRequireUser_NoMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{svc: new(MockService)}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/job-searches", nil)

	h.list(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
