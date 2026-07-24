package match

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"cv-evaluator/internal/services/matcher"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMatchByJobDescription_Success(t *testing.T) {
	svc := new(MockJobMatcher)
	userID := ulid.Make()
	expected := &matcher.Response{
		OverallScore:  85,
		SkillsScore:   90,
		Strengths:     []string{"Go", "Distributed systems"},
		MissingSkills: []string{"Kubernetes"},
		Summary:       "Strong match",
	}
	svc.On("MatchByJobDescription", mock.Anything, userID, "We are looking for a Go engineer").
		Return(expected, nil)

	w := performRequest(newTestRouter(svc, userID), http.MethodPost, "/match/job-description",
		`{"job_description":"We are looking for a Go engineer"}`)

	require.Equal(t, http.StatusOK, w.Code)
	var got matcher.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, expected.OverallScore, got.OverallScore)
	assert.Equal(t, expected.SkillsScore, got.SkillsScore)
	assert.Equal(t, expected.Strengths, got.Strengths)
	assert.Equal(t, expected.MissingSkills, got.MissingSkills)
	assert.Equal(t, expected.Summary, got.Summary)
	svc.AssertExpectations(t)
}

func TestMatchByJobDescription_MissingBody(t *testing.T) {
	svc := new(MockJobMatcher)
	userID := ulid.Make()

	w := performRequest(newTestRouter(svc, userID), http.MethodPost, "/match/job-description", `{}`)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	svc.AssertNotCalled(t, "MatchByJobDescription")
}

func TestMatchByJobDescription_Unauthorized(t *testing.T) {
	svc := new(MockJobMatcher)

	w := performRequest(noAuthRouter(svc), http.MethodPost, "/match/job-description",
		`{"job_description":"We are looking for a Go engineer"}`)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "MatchByJobDescription")
}

func TestMatchByJobDescription_ServiceError(t *testing.T) {
	svc := new(MockJobMatcher)
	userID := ulid.Make()
	svc.On("MatchByJobDescription", mock.Anything, userID, mock.AnythingOfType("string")).
		Return(nil, errors.New("database unavailable"))

	w := performRequest(newTestRouter(svc, userID), http.MethodPost, "/match/job-description",
		`{"job_description":"We are looking for a Go engineer"}`)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	svc.AssertExpectations(t)
}

func TestMatchByJobDescription_ServiceTimeout(t *testing.T) {
	svc := new(MockJobMatcher)
	userID := ulid.Make()
	svc.On("MatchByJobDescription", mock.Anything, userID, mock.AnythingOfType("string")).
		Return(nil, context.DeadlineExceeded)

	w := performRequest(newTestRouter(svc, userID), http.MethodPost, "/match/job-description",
		`{"job_description":"We are looking for a Go engineer"}`)

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	svc.AssertExpectations(t)
}

func TestMatchByJobId_Success(t *testing.T) {
	svc := new(MockJobMatcher)
	userID := ulid.Make()
	expected := &matcher.Response{OverallScore: 78, Summary: "Good match"}
	svc.On("MatchByJobID", mock.Anything, userID, "job-abc-123").Return(expected, nil)

	w := performRequest(newTestRouter(svc, userID), http.MethodPost, "/match/job/job-abc-123", "")

	require.Equal(t, http.StatusOK, w.Code)
	var got matcher.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, expected.OverallScore, got.OverallScore)
	assert.Equal(t, expected.Summary, got.Summary)
	svc.AssertExpectations(t)
}

func TestMatchByJobId_Unauthorized(t *testing.T) {
	svc := new(MockJobMatcher)

	w := performRequest(noAuthRouter(svc), http.MethodPost, "/match/job/job-abc-123", "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "MatchByJobID")
}

func TestMatchByJobId_ServiceError(t *testing.T) {
	svc := new(MockJobMatcher)
	userID := ulid.Make()
	svc.On("MatchByJobID", mock.Anything, userID, "job-abc-123").
		Return(nil, errors.New("job not found"))

	w := performRequest(newTestRouter(svc, userID), http.MethodPost, "/match/job/job-abc-123", "")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	svc.AssertExpectations(t)
}

func TestMatchByJobLink_NotImplemented(t *testing.T) {
	svc := new(MockJobMatcher)
	userID := ulid.Make()

	w := performRequest(newTestRouter(svc, userID), http.MethodPost, "/match/job-link", "")

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	svc.AssertNotCalled(t, "MatchByJobID")
	svc.AssertNotCalled(t, "MatchByJobDescription")
}
