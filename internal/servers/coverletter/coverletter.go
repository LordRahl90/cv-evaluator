// Package coverletter exposes POST /match/cover-letter, which writes a cover
// letter for a pasted job from the caller's newest CV. Nothing is stored.
package coverletter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"
	"cv-evaluator/internal/servers/ratelimit"
	coverlettersvc "cv-evaluator/internal/services/coverletter"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/services/users"
	"cv-evaluator/internal/telemetry"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

// Error codes of this handler.
const (
	// CodeCVMissing is returned (422) when the caller has no CV yet.
	CodeCVMissing = "cv_missing"
	// CodeCoverLetterFailed is returned (502) when no letter passing the
	// checks could be written.
	CodeCoverLetterFailed = "cover_letter_failed"
)

// Default rate limit: letters per user per window. Every letter is one or
// two LLM calls, so the limit is low.
const (
	DefaultLimit  = 5
	DefaultWindow = time.Minute
)

// Writer is the part of *coverlettersvc.Service the handler depends on.
type Writer interface {
	Generate(ctx context.Context, userID ulid.ULID, req coverlettersvc.Request) (*coverlettersvc.Letter, error)
}

type request struct {
	JobDescription string            `json:"job_description"`
	Evaluation     *matcher.Response `json:"evaluation"`
}

// Handler wires the cover letter service to HTTP.
type Handler struct {
	writer  Writer
	limiter *ratelimit.Limiter
}

// New registers POST /match/cover-letter on router, authenticated and
// limited to DefaultLimit letters per user per DefaultWindow. opts are
// passed to middleware.Authenticate.
func New(router gin.IRouter, writer Writer, jwtSecret []byte, opts ...middleware.Option) *Handler {
	return NewWithLimiter(router, writer, ratelimit.New(DefaultLimit, DefaultWindow), jwtSecret, opts...)
}

// NewWithLimiter is New with a custom rate limiter.
func NewWithLimiter(router gin.IRouter, writer Writer, limiter *ratelimit.Limiter, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{writer: writer, limiter: limiter}
	router.POST("/match/cover-letter", middleware.Authenticate(jwtSecret, opts...), h.create)
	return h
}

func (h *Handler) create(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
		return
	}

	var req request
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.BadRequest(c, apierror.CodeInvalidJSON, "The request body must be a valid JSON object.", "")
		return
	}
	req.JobDescription = strings.TrimSpace(req.JobDescription)
	switch {
	case req.JobDescription == "":
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The job_description parameter is required.", "job_description")
		return
	case len([]rune(req.JobDescription)) > coverlettersvc.MaxJobDescriptionChars:
		apierror.BadRequest(c, apierror.CodeParameterInvalid,
			fmt.Sprintf("The job_description parameter must be at most %d characters long.", coverlettersvc.MaxJobDescriptionChars), "job_description")
		return
	}

	// Only well-formed requests count toward the limit.
	if ok, retryAfter := h.limiter.Allow(userID.String()); !ok {
		apierror.TooManyRequests(c, fmt.Sprintf("Too many cover letters in a short time (at most %d per %s). Try again in %d seconds.",
			h.limiter.Limit(), windowText(h.limiter.Window()), int(math.Ceil(retryAfter.Seconds()))), retryAfter)
		return
	}

	letter, err := h.writer.Generate(c.Request.Context(), userID, coverlettersvc.Request{
		JobDescription: req.JobDescription,
		Evaluation:     req.Evaluation,
	})
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, letter)
}

func windowText(d time.Duration) string {
	if d == time.Minute {
		return "minute"
	}
	return d.String()
}

func handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, coverlettersvc.ErrNoCV):
		apierror.Write(c, http.StatusUnprocessableEntity, apierror.InvalidRequest(CodeCVMissing, "Upload a CV before writing a cover letter.", ""))
	case errors.Is(err, users.ErrUserNotFound):
		apierror.NotFound(c, "The account does not exist.", "")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c, err)
	case errors.Is(err, coverlettersvc.ErrGenerationFailed):
		// The service has logged which checks failed.
		telemetry.RecordError(c.Request.Context(), err)
		apierror.Write(c, http.StatusBadGateway, apierror.Error{
			Type:    apierror.TypeAPI,
			Code:    CodeCoverLetterFailed,
			Message: "We could not write a cover letter that meets our quality checks. Please try again.",
		})
	default:
		slog.ErrorContext(c.Request.Context(), "cover letter handler", "error", err)
		apierror.InternalServerError(c, err)
	}
}
