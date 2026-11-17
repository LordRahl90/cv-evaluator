package match

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cv-evaluator/internal/integrations/extractor"
	"cv-evaluator/internal/services/matcher"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGuestMatcher records what it was asked to match.
type fakeGuestMatcher struct {
	resp   *matcher.Response
	err    error
	calls  int
	cvText string
	job    string
}

func (f *fakeGuestMatcher) MatchCVText(_ context.Context, cvText, jobDescription string) (*matcher.Response, error) {
	f.calls++
	f.cvText, f.job = cvText, jobDescription
	return f.resp, f.err
}

// inProcessPDF extracts with extractor.Text, which Isolated runs in a child.
type inProcessPDF struct{}

func (inProcessPDF) Text(_ context.Context, data []byte) (string, error) { return extractor.Text(data) }

// stubPDF returns fixed text or an error.
type stubPDF struct {
	text string
	err  error
}

func (s stubPDF) Text(context.Context, []byte) (string, error) { return s.text, s.err }

func newGuestRouter(m GuestMatcher, pdf PDFExtractor) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewGuest(r, m, pdf)
	return r
}

// postGuest posts the guest form; a nil cv leaves the file out and an empty
// job leaves the job description out.
func postGuest(t *testing.T, r *gin.Engine, cv []byte, job string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if cv != nil {
		part, err := w.CreateFormFile("cv", "resume.pdf")
		require.NoError(t, err)
		_, err = part.Write(cv)
		require.NoError(t, err)
	}
	if job != "" {
		require.NoError(t, w.WriteField("job_description", job))
	}
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, GuestPath, body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func readDummyResume(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../integrations/extractor/testdata/dummy-resume.pdf")
	require.NoError(t, err)
	return data
}

// errorBody is the apierror envelope.
type errorBody struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Param   string `json:"param"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body
}

func TestGuestMatch_SuccessWithoutAuth(t *testing.T) {
	want := &matcher.Response{
		OverallScore: 72,
		Breakdown:    []matcher.CriterionScore{{Criterion: "skills", Score: 80, Reason: "Go"}},
		Strengths:    []string{"Go"},
		Summary:      "Good fit",
	}
	m := &fakeGuestMatcher{resp: want}

	rec := postGuest(t, newGuestRouter(m, inProcessPDF{}), readDummyResume(t), "  We need a Go engineer  ")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got matcher.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, *want, got)
	assert.Equal(t, 1, m.calls)
	assert.Equal(t, "We need a Go engineer", m.job)
	assert.NotEmpty(t, strings.TrimSpace(m.cvText), "the PDF's text is matched")
}

func TestGuestMatch_MissingFields(t *testing.T) {
	m := &fakeGuestMatcher{}
	r := newGuestRouter(m, stubPDF{text: "cv"})

	rec := postGuest(t, r, nil, "a job")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "cv", decodeError(t, rec).Error.Param)

	rec = postGuest(t, r, []byte("%PDF-1.4"), "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "job_description", decodeError(t, rec).Error.Param)

	rec = postGuest(t, r, []byte("%PDF-1.4"), "   ")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a blank job description is missing")

	assert.Zero(t, m.calls)
}

func TestGuestMatch_JobDescriptionTooLong(t *testing.T) {
	m := &fakeGuestMatcher{}
	rec := postGuest(t, newGuestRouter(m, stubPDF{text: "cv"}), []byte("%PDF-1.4"),
		strings.Repeat("é", matcher.MaxJobDescriptionChars+1))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeError(t, rec)
	assert.Equal(t, "parameter_invalid", body.Error.Code)
	assert.Equal(t, "job_description", body.Error.Param)
	assert.Zero(t, m.calls)
}

func TestGuestMatch_RejectsANonPDF(t *testing.T) {
	m := &fakeGuestMatcher{}
	rec := postGuest(t, newGuestRouter(m, inProcessPDF{}), []byte("just some text"), "a job")

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := decodeError(t, rec)
	assert.Equal(t, "invalid_pdf", body.Error.Code)
	assert.Equal(t, "The CV must be a PDF file.", body.Error.Message)
	assert.Zero(t, m.calls)
}

func TestGuestMatch_RejectedPDFMessagesAreSafe(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"too many pages": {extractor.ErrTooManyPages, "The CV must have at most 30 pages."},
		"unreadable":     {errors.Join(extractor.ErrUnreadable, errors.New("xref offset 1234 broken")), "We could not read text from this PDF."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postGuest(t, newGuestRouter(&fakeGuestMatcher{}, stubPDF{err: tc.err}), []byte("%PDF-1.4"), "a job")
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Contains(t, decodeError(t, rec).Error.Message, tc.want)
			assert.NotContains(t, rec.Body.String(), "xref")
		})
	}
}

func TestGuestMatch_PDFWithoutText(t *testing.T) {
	rec := postGuest(t, newGuestRouter(&fakeGuestMatcher{err: matcher.ErrNoCV}, stubPDF{text: "  "}), []byte("%PDF-1.4"), "a job")

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "invalid_pdf", decodeError(t, rec).Error.Code)
}

func TestGuestMatch_TooLarge(t *testing.T) {
	m := &fakeGuestMatcher{}
	big := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("a"), extractor.MaxFileBytes)...)

	rec := postGuest(t, newGuestRouter(m, inProcessPDF{}), big, "a job")

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "cv", decodeError(t, rec).Error.Param)
	assert.Zero(t, m.calls)
}

func TestGuestMatch_BodyOverTheServerLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// As servers.limitBody does for every request.
	r.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<10)
	})
	NewGuest(r, &fakeGuestMatcher{}, inProcessPDF{})

	rec := postGuest(t, r, bytes.Repeat([]byte("a"), 4<<10), "a job")
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestGuestMatch_InternalErrorsAreGeneric(t *testing.T) {
	m := &fakeGuestMatcher{err: errors.New("deepseek: 401 invalid api key sk-secret")}
	rec := postGuest(t, newGuestRouter(m, stubPDF{text: "Go engineer"}), []byte("%PDF-1.4"), "a job")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "sk-secret")
	assert.NotContains(t, rec.Body.String(), "deepseek")
}

func TestGuestMatch_Timeout(t *testing.T) {
	m := &fakeGuestMatcher{err: context.DeadlineExceeded}
	rec := postGuest(t, newGuestRouter(m, stubPDF{text: "Go engineer"}), []byte("%PDF-1.4"), "a job")
	assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
}

// The authenticated match routes still require auth next to the guest one.
func TestGuestMatch_OtherMatchRoutesStillNeedAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	New(r, new(MockJobMatcher), testJWTSecret)
	NewGuest(r, &fakeGuestMatcher{resp: &matcher.Response{}}, stubPDF{text: "cv"})

	rec := performRequest(r, http.MethodPost, "/match/job-description", `{"job_description":"x"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	assert.Equal(t, http.StatusOK, postGuest(t, r, []byte("%PDF-1.4"), "a job").Code)
}
