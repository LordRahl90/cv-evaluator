package users

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/stretchr/testify/require"
)

type uploadTestEmbeddingLLM struct{}

func (uploadTestEmbeddingLLM) CreateEmbedding(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, 768), nil
}

type uploadTestChatLLM struct{}

func (uploadTestChatLLM) CleanupCV(_ context.Context, _ string) (string, error) {
	return `{"summary":"summary","experience":["exp"],"skills":["go"],"education":["uni"],"title":"engineer"}`, nil
}

func newUploadTestService(t *testing.T) (*Service, *models.User) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	user := &models.User{
		FirstName: "Upload",
		LastName:  "Tester",
		Email:     "upload.test@example.com",
		Password:  "password",
	}
	require.NoError(t, db.Create(user).Error)

	svc := NewWithAuth(db, uploadTestChatLLM{}, uploadTestEmbeddingLLM{}, "test-secret", 0)
	return svc, user
}

func makeFileHeaderFromPath(t *testing.T, path string) *multipart.FileHeader {
	t.Helper()

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() {
		_ = f.Close()
	}()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("cv", filepath.Base(path))
	require.NoError(t, err)
	_, err = io.Copy(part, f)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, req.ParseMultipartForm(20<<20))

	mf, fh, err := req.FormFile("cv")
	require.NoError(t, err)
	require.NotNil(t, fh)
	require.NoError(t, mf.Close())

	return fh
}

func TestService_UploadCV(t *testing.T) {
	svc, user := newUploadTestService(t)
	cvHeader := makeFileHeaderFromPath(t, "./testdata/alugbin-abiodun-resume.pdf")

	err := svc.UploadCV(t.Context(), user.ID, cvHeader)
	require.NoError(t, err)

	cvs, err := svc.ListUserCVs(t.Context(), user.ID)
	require.NoError(t, err)
	require.Len(t, cvs, 1)

	var sectionCount int64
	err = svc.db.WithContext(t.Context()).
		Model(&models.SectionEmbedding{}).
		Where("user_id = ? AND cv_id = ?", user.ID, cvs[0].ID).
		Count(&sectionCount).Error
	require.NoError(t, err)
	require.Greater(t, sectionCount, int64(0))
}

func TestService_UploadCV_RequiresFile(t *testing.T) {
	svc, user := newUploadTestService(t)

	err := svc.UploadCV(t.Context(), user.ID, nil)
	require.Error(t, err)
}
