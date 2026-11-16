package match

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"
	"cv-evaluator/internal/services/matcher"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

// JobMatcher is the domain service interface consumed by this handler.
type JobMatcher interface {
	MatchByJobID(ctx context.Context, userID ulid.ULID, jobID string) (*matcher.Response, error)
	MatchByJobDescription(ctx context.Context, userID ulid.ULID, jobDescription string) (*matcher.Response, error)
}

// matchByDescriptionRequest is the JSON payload for POST /match/job-description.
type matchByDescriptionRequest struct {
	JobDescription string `json:"job_description" binding:"required"`
}

// Handler wires the JobMatcher service to HTTP routes.
type Handler struct {
	router  *gin.Engine
	matcher JobMatcher
}

// New creates a new Handler and registers its routes on the provided router,
// protected by the auth middleware. opts are passed to middleware.Authenticate,
// for example middleware.WithAPIKeyVerifier to accept API keys as well as JWTs.
func New(router *gin.Engine, matcher JobMatcher, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{
		router:  router,
		matcher: matcher,
	}
	h.registerRoutes(router, jwtSecret, opts)
	return h
}

func (h *Handler) registerRoutes(router *gin.Engine, jwtSecret []byte, opts []middleware.Option) {
	match := router.Group("match")
	match.Use(middleware.Authenticate(jwtSecret, opts...))
	{
		match.POST("job-description", h.matchByJobDescription)
		match.POST("job/:id", h.matchByJobId)
		match.POST("job-link", h.matchByJobLink) // this involves a provider and webscraping flow, so pending for now
	}
}

// matchByJobDescription handles POST /match/job-description.
func (h *Handler) matchByJobDescription(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("unauthorized"))
		return
	}

	var req matchByDescriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.BadRequest(c, apierror.CodeInvalidJSON, "The request body must be JSON with a job_description.", "job_description")
		return
	}
	if n := len([]rune(req.JobDescription)); n > matcher.MaxJobDescriptionChars {
		apierror.BadRequest(c, apierror.CodeParameterInvalid,
			fmt.Sprintf("The job_description parameter must be at most %d characters long.", matcher.MaxJobDescriptionChars), "job_description")
		return
	}

	resp, err := h.matcher.MatchByJobDescription(c.Request.Context(), userID, req.JobDescription)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// matchByJobId handles POST /match/job/:id.
func (h *Handler) matchByJobId(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("unauthorized"))
		return
	}

	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("job id is required"))
		return
	}

	resp, err := h.matcher.MatchByJobID(c.Request.Context(), userID, jobID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// matchByJobLink is pending implementation (scraping flow).
func (h *Handler) matchByJobLink(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, errorResponse("not implemented"))
}

// getUserID retrieves the authenticated user ID from the gin context.
// It expects the auth middleware to have stored it under middleware.UserIDKey.
func getUserID(c *gin.Context) (ulid.ULID, bool) {
	raw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		return ulid.ULID{}, false
	}
	switch v := raw.(type) {
	case ulid.ULID:
		return v, v != (ulid.ULID{})
	case string:
		parsed, err := ulid.ParseStrict(v)
		if err != nil || parsed == (ulid.ULID{}) {
			return ulid.ULID{}, false
		}
		return parsed, true
	}
	return ulid.ULID{}, false
}

// handleServiceError maps well-known service errors to appropriate HTTP
// status codes. Anything else is logged and answered with a generic 500, so
// internal details never reach the client.
func handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, matcher.ErrNoCV):
		apierror.Write(c, http.StatusUnprocessableEntity,
			apierror.InvalidRequest(CodeCVRequired, "Upload a CV before matching.", ""))
	case errors.Is(err, matcher.ErrJobNotFound):
		apierror.NotFound(c, "No job has that id.", "id")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c)
	default:
		slog.ErrorContext(c.Request.Context(), "match failed", "error", err)
		apierror.InternalServerError(c)
	}
}

// CodeCVRequired is the error code when the user has no CV to match against.
const CodeCVRequired = "cv_required"

func errorResponse(msg string) gin.H {
	return gin.H{"error": msg}
}
