package cv

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/users"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ── POST /cv/ ────────────────────────────────────────────────────────────────

func TestUploadCV_Success(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()
	svc.On("UploadCV", mock.Anything, userID, mock.AnythingOfType("*multipart.FileHeader")).
		Return(nil).Once()

	w := doMultipartUpload(newTestRouter(svc), authHeader(t, userID), "my cv content")

	assert.Equal(t, http.StatusAccepted, w.Code)
	svc.AssertExpectations(t)
}

func TestUploadCV_NoFile(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()

	w := doRequest(newTestRouter(svc), http.MethodPost, "/cv/", authHeader(t, userID), "")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	svc.AssertNotCalled(t, "UploadCV")
}

func TestUploadCV_Unauthorized(t *testing.T) {
	svc := new(MockCVService)

	w := doMultipartUpload(newTestRouter(svc), "", "my cv content")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "UploadCV")
}

func TestUploadCV_ServiceError(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()
	svc.On("UploadCV", mock.Anything, userID, mock.AnythingOfType("*multipart.FileHeader")).
		Return(errors.New("extraction failed")).Once()

	w := doMultipartUpload(newTestRouter(svc), authHeader(t, userID), "bad content")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	svc.AssertExpectations(t)
}

// ── GET /cv/ ─────────────────────────────────────────────────────────────────

func TestListCVs_Success(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()
	now := time.Now().Truncate(time.Second)
	cvID := ulid.Make()
	svc.On("ListUserCVs", mock.Anything, userID).
		Return([]models.CV{{ID: cvID, UserID: userID, CreatedAt: now, UpdatedAt: now}}, nil).
		Once()

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/", authHeader(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), cvID.String())
	svc.AssertExpectations(t)
}

func TestListCVs_EmptyList(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()
	svc.On("ListUserCVs", mock.Anything, userID).Return([]models.CV{}, nil).Once()

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/", authHeader(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, "[]", w.Body.String())
	svc.AssertExpectations(t)
}

func TestListCVs_Unauthorized(t *testing.T) {
	svc := new(MockCVService)

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/", "", "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "ListUserCVs")
}

func TestListCVs_ServiceError(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()
	svc.On("ListUserCVs", mock.Anything, userID).
		Return(nil, errors.New("db down")).Once()

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/", authHeader(t, userID), "")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	svc.AssertExpectations(t)
}

// ── GET /cv/download/:id ─────────────────────────────────────────────────────

func TestDownloadCV_Success(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()
	cvID := ulid.Make()
	svc.On("GetCV", mock.Anything, userID, cvID).
		Return(&models.CV{ID: cvID, UserID: userID, ExtractedContent: "my extracted cv text"}, nil).
		Once()

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/download/"+cvID.String(), authHeader(t, userID), "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "my extracted cv text", w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	svc.AssertExpectations(t)
}

func TestDownloadCV_OwnershipViolation(t *testing.T) {
	svc := new(MockCVService)
	ownerID := ulid.Make()
	attackerID := ulid.Make()
	cvID := ulid.Make()

	// The service returns ErrCVNotFound when the userID doesn't match the owner.
	svc.On("GetCV", mock.Anything, attackerID, cvID).
		Return(nil, users.ErrCVNotFound).
		Once()

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/download/"+cvID.String(), authHeader(t, attackerID), "")

	assert.Equal(t, http.StatusNotFound, w.Code)
	_ = ownerID // referenced above for clarity
	svc.AssertExpectations(t)
}

func TestDownloadCV_Unauthorized(t *testing.T) {
	svc := new(MockCVService)
	cvID := ulid.Make()

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/download/"+cvID.String(), "", "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "GetCV")
}

func TestDownloadCV_InvalidID(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()

	w := doRequest(newTestRouter(svc), http.MethodGet, "/cv/download/not-a-ulid", authHeader(t, userID), "")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	svc.AssertNotCalled(t, "GetCV")
}

// ── DELETE /cv/:id ────────────────────────────────────────────────────────────

func TestDeleteCV_Success(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()
	cvID := ulid.Make()
	svc.On("DeleteCV", mock.Anything, userID, cvID).Return(nil).Once()

	w := doRequest(newTestRouter(svc), http.MethodDelete, "/cv/"+cvID.String(), authHeader(t, userID), "")

	assert.Equal(t, http.StatusNoContent, w.Code)
	svc.AssertExpectations(t)
}

func TestDeleteCV_OwnershipViolation(t *testing.T) {
	svc := new(MockCVService)
	attackerID := ulid.Make()
	cvID := ulid.Make()
	svc.On("DeleteCV", mock.Anything, attackerID, cvID).
		Return(users.ErrCVNotFound).
		Once()

	w := doRequest(newTestRouter(svc), http.MethodDelete, "/cv/"+cvID.String(), authHeader(t, attackerID), "")

	assert.Equal(t, http.StatusNotFound, w.Code)
	svc.AssertExpectations(t)
}

func TestDeleteCV_Unauthorized(t *testing.T) {
	svc := new(MockCVService)
	cvID := ulid.Make()

	w := doRequest(newTestRouter(svc), http.MethodDelete, "/cv/"+cvID.String(), "", "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	svc.AssertNotCalled(t, "DeleteCV")
}

func TestDeleteCV_InvalidID(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()

	w := doRequest(newTestRouter(svc), http.MethodDelete, "/cv/bad-id", authHeader(t, userID), "")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	svc.AssertNotCalled(t, "DeleteCV")
}

// ── PATCH /cv/ ───────────────────────────────────────────────────────────────

func TestUpdateCV_NotImplemented(t *testing.T) {
	svc := new(MockCVService)
	userID := ulid.Make()

	w := doRequest(newTestRouter(svc), http.MethodPatch, "/cv/", authHeader(t, userID), "")

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}
