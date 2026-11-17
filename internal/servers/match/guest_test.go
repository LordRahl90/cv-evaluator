package match

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/integrations/extractor"
	"cv-evaluator/internal/servers/ratelimit"
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

	rec := postGuest(t, newGuestRouter(m, inProcessPDF{}), readDummyResume(t), "  "+validJob+"  ")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got matcher.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, *want, got)
	assert.Equal(t, 1, m.calls)
	assert.Equal(t, validJob, m.job)
	assert.NotEmpty(t, strings.TrimSpace(m.cvText), "the PDF's text is matched")
}

func TestGuestMatch_MissingFields(t *testing.T) {
	m := &fakeGuestMatcher{}
	r := newGuestRouter(m, stubPDF{text: validCV})

	rec := postGuest(t, r, nil, validJob)
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
	rec := postGuest(t, newGuestRouter(m, stubPDF{text: validCV}), []byte("%PDF-1.4"),
		strings.Repeat("é", matcher.MaxJobDescriptionChars+1))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeError(t, rec)
	assert.Equal(t, "parameter_invalid", body.Error.Code)
	assert.Equal(t, "job_description", body.Error.Param)
	assert.Zero(t, m.calls)
}

func TestGuestMatch_RejectsANonPDF(t *testing.T) {
	m := &fakeGuestMatcher{}
	rec := postGuest(t, newGuestRouter(m, inProcessPDF{}), []byte("just some text"), validJob)

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
			rec := postGuest(t, newGuestRouter(&fakeGuestMatcher{}, stubPDF{err: tc.err}), []byte("%PDF-1.4"), validJob)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Contains(t, decodeError(t, rec).Error.Message, tc.want)
			assert.NotContains(t, rec.Body.String(), "xref")
		})
	}
}

func TestGuestMatch_PDFWithoutText(t *testing.T) {
	rec := postGuest(t, newGuestRouter(&fakeGuestMatcher{err: matcher.ErrNoCV}, stubPDF{text: "  "}), []byte("%PDF-1.4"), validJob)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "invalid_pdf", decodeError(t, rec).Error.Code)
}

func TestGuestMatch_TooLarge(t *testing.T) {
	m := &fakeGuestMatcher{}
	big := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("a"), extractor.MaxFileBytes)...)

	rec := postGuest(t, newGuestRouter(m, inProcessPDF{}), big, validJob)

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

	rec := postGuest(t, r, bytes.Repeat([]byte("a"), 4<<10), validJob)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestGuestMatch_InternalErrorsAreGeneric(t *testing.T) {
	m := &fakeGuestMatcher{err: errors.New("deepseek: 401 invalid api key sk-secret")}
	rec := postGuest(t, newGuestRouter(m, stubPDF{text: validCV}), []byte("%PDF-1.4"), validJob)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "sk-secret")
	assert.NotContains(t, rec.Body.String(), "deepseek")
}

func TestGuestMatch_Timeout(t *testing.T) {
	m := &fakeGuestMatcher{err: context.DeadlineExceeded}
	rec := postGuest(t, newGuestRouter(m, stubPDF{text: validCV}), []byte("%PDF-1.4"), validJob)
	assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
}

// The authenticated match routes still require auth next to the guest one.
func TestGuestMatch_OtherMatchRoutesStillNeedAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	New(r, new(MockJobMatcher), testJWTSecret)
	NewGuest(r, &fakeGuestMatcher{resp: &matcher.Response{}}, stubPDF{text: validCV})

	rec := performRequest(r, http.MethodPost, "/match/job-description", `{"job_description":"x"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	assert.Equal(t, http.StatusOK, postGuest(t, r, []byte("%PDF-1.4"), validJob).Code)
}

// validJob and validCV pass the guest content checks.
const validJob = `Senior Go Engineer

We are hiring a backend engineer to build and run the services behind our hiring platform.
You will design APIs in Go, model data in PostgreSQL, and work with product and design on new features.

Requirements: four or more years of Go in production, solid SQL, experience with Kubernetes and observability.
Nice to have: TypeScript, Vue, and event streaming with Kafka.`

const validCV = `Jane Doe, backend engineer. Five years building Go services at Routewise and Parcelly:
payment APIs, a PostgreSQL schema serving two million users, and the move from a monolith to services on Kubernetes.
Led on-call for the platform team, added tracing with OpenTelemetry and cut p99 latency by forty percent.
Skills: Go, SQL, PostgreSQL, Kubernetes, Terraform, TypeScript. Degree in computer science, University of Lagos.`

// countingStore is a ratelimit.Store that counts every request it sees.
type countingStore struct{ calls int }

func (s *countingStore) Incr(_ context.Context, _ string, window time.Duration) (int64, time.Duration, error) {
	s.calls++
	return int64(s.calls), window, nil
}

// newCappedGuestRouter is newGuestRouter with a total cap of limit guest
// matches, counted in the returned store.
func newCappedGuestRouter(m GuestMatcher, pdf PDFExtractor, limit int) (*gin.Engine, *countingStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := &countingStore{}
	NewGuest(r, m, pdf).CapTotal(ratelimit.Rule{
		Method: http.MethodPost, Path: GuestPath, What: "guest matches", Message: "Guest matching is busy right now.",
		Limiter: ratelimit.NewShared(store, "guest-matches", limit, 24*time.Hour),
	})
	return r, store
}

// words returns n different words, so only the count is too low.
func words(n int) string {
	w := make([]string, n)
	for i := range w {
		w[i] = fmt.Sprintf("requirement%d", i)
	}
	return strings.Join(w, " ")
}

// aWords is 50 different words made almost only of the letter a.
func aWords() string {
	w := make([]string, 50)
	for i := range w {
		w[i] = strings.Repeat("a", 3+i%10) + string(rune('b'+i%20))
	}
	return strings.Join(w, " ")
}

func TestGuestMatch_RejectionsDoNotUseTheTotalCapOrTheModel(t *testing.T) {
	pdf := []byte("%PDF-1.4")
	ok := stubPDF{text: validCV}
	cases := map[string]struct {
		pdf    PDFExtractor
		cv     []byte
		job    string
		status int
		param  string
		msg    string
	}{
		"no cv":  {pdf: ok, job: validJob, status: http.StatusBadRequest, param: "cv"},
		"no job": {pdf: ok, cv: pdf, status: http.StatusBadRequest, param: "job_description"},
		"job under 40 words": {pdf: ok, cv: pdf, job: words(39),
			status: http.StatusBadRequest, param: "job_description", msg: "too short"},
		"job under 200 chars": {pdf: ok, cv: pdf, job: strings.Repeat("go be ", 25),
			status: http.StatusBadRequest, param: "job_description", msg: "too short"},
		"job without letters": {pdf: ok, cv: pdf, job: strings.Repeat("12345 !!! 678 ", 20),
			status: http.StatusBadRequest, param: "job_description", msg: "does not look like a job posting"},
		"one word repeated": {pdf: ok, cv: pdf, job: strings.Repeat("engineer ", 60),
			status: http.StatusBadRequest, param: "job_description", msg: "does not look like a job posting"},
		"a few words repeated": {pdf: ok, cv: pdf, job: strings.Repeat("we need a great go engineer now ", 10),
			status: http.StatusBadRequest, param: "job_description", msg: "does not look like a job posting"},
		"one character repeated": {pdf: ok, cv: pdf, job: aWords(),
			status: http.StatusBadRequest, param: "job_description", msg: "does not look like a job posting"},
		"job too long": {pdf: ok, cv: pdf, job: strings.Repeat("é", matcher.MaxJobDescriptionChars+1),
			status: http.StatusBadRequest, param: "job_description"},
		"not a pdf": {pdf: inProcessPDF{}, cv: []byte("just some text"), job: validJob,
			status: http.StatusUnprocessableEntity, param: "cv"},
		"unreadable pdf": {pdf: stubPDF{err: extractor.ErrUnreadable}, cv: pdf, job: validJob,
			status: http.StatusUnprocessableEntity, param: "cv"},
		"cv too large": {pdf: inProcessPDF{}, cv: append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("a"), extractor.MaxFileBytes)...), job: validJob,
			status: http.StatusRequestEntityTooLarge, param: "cv"},
		"cv without text": {pdf: stubPDF{text: "  "}, cv: pdf, job: validJob,
			status: http.StatusUnprocessableEntity, param: "cv", msg: "We couldn't read enough text from this PDF"},
		"cv under 50 words": {pdf: stubPDF{text: words(49)}, cv: pdf, job: validJob,
			status: http.StatusUnprocessableEntity, param: "cv", msg: "We couldn't read enough text from this PDF"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := &fakeGuestMatcher{resp: &matcher.Response{}}
			r, store := newCappedGuestRouter(m, tc.pdf, 300)

			rec := postGuest(t, r, tc.cv, tc.job)

			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			body := decodeError(t, rec)
			assert.Equal(t, tc.param, body.Error.Param)
			assert.Contains(t, body.Error.Message, tc.msg)
			if tc.status == http.StatusUnprocessableEntity {
				assert.Equal(t, "invalid_pdf", body.Error.Code)
			}
			assert.Zero(t, store.calls, "the total cap is not used")
			assert.Zero(t, m.calls, "the model is not called")
		})
	}
}

func TestGuestMatch_ValidRequestUsesTheTotalCap(t *testing.T) {
	m := &fakeGuestMatcher{resp: &matcher.Response{OverallScore: 70}}
	r, store := newCappedGuestRouter(m, stubPDF{text: validCV}, 1)

	require.Equal(t, http.StatusOK, postGuest(t, r, []byte("%PDF-1.4"), validJob).Code)
	assert.Equal(t, 1, store.calls)
	assert.Equal(t, 1, m.calls)

	rec := postGuest(t, r, []byte("%PDF-1.4"), validJob)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "Guest matching is busy right now.", decodeError(t, rec).Error.Message)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.Equal(t, 2, store.calls)
	assert.Equal(t, 1, m.calls, "over the cap, the model is not called")
}

func TestJobDescriptionProblem_AcceptsRealPostings(t *testing.T) {
	postings := map[string]string{
		"english": validJob,
		"french": `Ingénieur logiciel Go (H/F), Paris. Nous recherchons une personne pour concevoir nos services
de paiement, écrire des API robustes et améliorer la fiabilité de notre plateforme. Vous travaillerez avec
les équipes produit et données. Profil : trois ans d'expérience en Go, bonne maîtrise de PostgreSQL et de
Kubernetes, goût pour la qualité du code et les revues. Télétravail partiel possible, salaire selon profil.`,
		"bullets": `Data Analyst – Remote
- Build dashboards in Looker and keep them accurate
- Write SQL against BigQuery to answer product questions
- Work with marketing on campaign measurement
- Present findings to leadership every month
Requirements:
- 2+ years in analytics, ideally in a consumer product company
- Strong SQL and spreadsheets
- Python is a plus`,
	}
	for name, text := range postings {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, jobDescriptionProblem(text))
		})
	}
}
