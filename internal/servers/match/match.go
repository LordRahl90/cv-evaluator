package match

import (
	"context"
	"errors"
	"net/http"

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

// New creates a new Handler and registers its routes on the provided router.
func New(router *gin.Engine, matcher JobMatcher) *Handler {
	h := &Handler{
		router:  router,
		matcher: matcher,
	}
	h.registerRoutes(router)
	return h
}

func (h *Handler) registerRoutes(router *gin.Engine) {
	match := router.Group("match")
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
		c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
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

// handleServiceError maps well-known service errors to appropriate HTTP status codes.
func handleServiceError(c *gin.Context, err error) {
	var statusCode int
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		statusCode = http.StatusGatewayTimeout
	default:
		statusCode = http.StatusInternalServerError
	}
	c.JSON(statusCode, errorResponse(err.Error()))
}

func errorResponse(msg string) gin.H {
	return gin.H{"error": msg}
}
