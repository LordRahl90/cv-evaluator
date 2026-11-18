package match

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"

	"cv-evaluator/internal/integrations/extractor"
	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/cv"
	"cv-evaluator/internal/servers/ratelimit"
	"cv-evaluator/internal/services/matcher"

	"github.com/gin-gonic/gin"
)

// GuestPath is the public route for matching without an account.
const GuestPath = "/match/guest"

// GuestMatcher scores CV text against a job description without storing
// anything.
type GuestMatcher interface {
	MatchCVText(ctx context.Context, cvText, jobDescription string) (*matcher.Response, error)
}

// PDFExtractor returns the plain text of a PDF; extractor.Isolated runs it
// in a child process.
type PDFExtractor interface {
	Text(ctx context.Context, data []byte) (string, error)
}

// GuestHandler serves quick matches for visitors without an account. Each
// one is a paid LLM call, so the route must be rate limited (see
// cmd/api/limits.go); nothing it receives is stored.
type GuestHandler struct {
	matcher GuestMatcher
	pdf     PDFExtractor
	// total caps guest matches across all visitors; see CapTotal.
	total *ratelimit.Rule
}

// NewGuest registers POST /match/guest on router, without authentication.
func NewGuest(router *gin.Engine, m GuestMatcher, pdf PDFExtractor) *GuestHandler {
	h := &GuestHandler{matcher: m, pdf: pdf}
	router.POST(GuestPath, h.matchGuest)
	return h
}

// CapTotal limits guest matches across all visitors together with rule.
// Only requests that passed every check and are about to call the model
// count, so junk cannot use up the allowance of real visitors.
func (h *GuestHandler) CapTotal(rule ratelimit.Rule) *GuestHandler {
	h.total = &rule
	return h
}

// matchGuest handles POST /match/guest: a multipart form with a "cv" PDF
// and a "job_description". It answers like POST /match/job-description.
func (h *GuestHandler) matchGuest(c *gin.Context) {
	ctx := c.Request.Context()

	fileHeader, err := c.FormFile("cv")
	if err != nil {
		if maxBytesErr := new(http.MaxBytesError); errors.As(err, &maxBytesErr) {
			writeCVTooLarge(c)
			return
		}
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The cv file is required.", "cv")
		return
	}
	// Check the job description before the PDF, so a bad request costs no
	// extraction.
	jobDescription := strings.TrimSpace(c.PostForm("job_description"))
	if jobDescription == "" {
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The job_description parameter is required.", "job_description")
		return
	}
	if n := len([]rune(jobDescription)); n > matcher.MaxJobDescriptionChars {
		apierror.BadRequest(c, apierror.CodeParameterInvalid,
			fmt.Sprintf("The job_description parameter must be at most %d characters long.", matcher.MaxJobDescriptionChars), "job_description")
		return
	}
	if problem := jobDescriptionProblem(jobDescription); problem != "" {
		apierror.BadRequest(c, apierror.CodeParameterInvalid, problem, "job_description")
		return
	}
	if fileHeader.Size > extractor.MaxFileBytes {
		writeCVTooLarge(c)
		return
	}

	data, err := readFormFile(fileHeader)
	if err != nil {
		handleGuestError(c, err)
		return
	}
	cvText, err := h.pdf.Text(ctx, data)
	if err != nil {
		handleGuestError(c, err)
		return
	}
	if len(strings.Fields(cvText)) < minGuestCVWords {
		apierror.Write(c, http.StatusUnprocessableEntity, apierror.InvalidRequest(cv.CodeInvalidPDF,
			"We couldn't read enough text from this PDF to score it. Export your CV again as a text-based PDF and retry.", "cv"))
		return
	}

	// Last, as the request is about to cost money: requests rejected above
	// do not use up the allowance shared by all visitors.
	if h.total != nil && !h.total.AllowGlobal(c) {
		return
	}

	// Lengths only: the CV and the job description are the visitor's.
	slog.InfoContext(ctx, "guest match", "cv_bytes", len(data), "cv_chars", len([]rune(cvText)), "job_chars", len([]rune(jobDescription)))
	resp, err := h.matcher.MatchCVText(ctx, cvText, jobDescription)
	if err != nil {
		handleGuestError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// readFormFile reads an uploaded file, at most one byte over
// extractor.MaxFileBytes so an oversized file is still recognised.
func readFormFile(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()
	return io.ReadAll(io.LimitReader(f, extractor.MaxFileBytes+1))
}

// handleGuestError maps errors to responses like the CV upload does.
// Anything else is logged and answered with a generic 500.
func handleGuestError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, extractor.ErrTooLarge):
		writeCVTooLarge(c)
	case extractor.IsInvalidPDF(err):
		slog.InfoContext(c.Request.Context(), "guest match rejected the PDF", "error", err)
		apierror.Write(c, http.StatusUnprocessableEntity, apierror.InvalidRequest(cv.CodeInvalidPDF, cv.InvalidPDFMessage(err), "cv"))
	case errors.Is(err, matcher.ErrNoCV):
		// The PDF parsed but had no text, as a scanned CV does.
		apierror.Write(c, http.StatusUnprocessableEntity, apierror.InvalidRequest(cv.CodeInvalidPDF, cv.InvalidPDFMessage(extractor.ErrUnreadable), "cv"))
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c)
	default:
		slog.ErrorContext(c.Request.Context(), "guest match failed", "error", err)
		apierror.InternalServerError(c)
	}
}

func writeCVTooLarge(c *gin.Context) {
	apierror.Write(c, http.StatusRequestEntityTooLarge, apierror.InvalidRequest(apierror.CodeBodyTooLarge,
		fmt.Sprintf("The CV must be a PDF of at most %d MB.", extractor.MaxFileBytes>>20), "cv"))
}
