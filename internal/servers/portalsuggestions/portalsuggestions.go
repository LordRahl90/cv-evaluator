// Package portalsuggestions exposes routes for suggesting new job portals.
package portalsuggestions

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"
	portalsvc "cv-evaluator/internal/services/portalsuggestions"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

// Error codes specific to portal suggestions.
const (
	CodePortalAlreadySuggested = "portal_already_suggested"
	CodePortalAlreadySupported = "portal_already_supported"
)

// Service is the domain interface the handler depends on.
// *portalsvc.Service satisfies it.
type Service interface {
	Create(ctx context.Context, userID ulid.ULID, p portalsvc.CreateParams) (*models.PortalSuggestion, error)
	List(ctx context.Context, userID ulid.ULID) ([]models.PortalSuggestion, error)
}

type createRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Note string `json:"note"`
}

// SuggestionResponse is the public representation of a portal suggestion.
type SuggestionResponse struct {
	Object    string    `json:"object"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Note      string    `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListResponse wraps a list of suggestions.
type ListResponse struct {
	Object string               `json:"object"`
	Data   []SuggestionResponse `json:"data"`
}

// Handler wires the Service to HTTP routes.
type Handler struct {
	svc Service
}

// New registers the portal suggestion routes on router, authenticated and
// scoped to the caller:
//
//	POST /portal-suggestions
//	GET  /portal-suggestions
//
// opts are passed to middleware.Authenticate, for example
// middleware.WithAPIKeyVerifier to accept API keys as well as JWTs and
// middleware.WithSessionValidator to reject revoked sessions.
func New(router gin.IRouter, svc Service, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{svc: svc}

	group := router.Group("/portal-suggestions", middleware.Authenticate(jwtSecret, opts...))
	group.POST("", h.create)
	group.GET("", h.list)

	return h
}

func (h *Handler) create(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
		return
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.BadRequest(c, apierror.CodeInvalidJSON, "The request body must be a valid JSON object.", "")
		return
	}

	rec, err := h.svc.Create(c.Request.Context(), userID, portalsvc.CreateParams(req))
	if err != nil {
		handleServiceError(c, rec, err)
		return
	}
	c.JSON(http.StatusCreated, toResponse(rec))
}

func (h *Handler) list(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
		return
	}

	recs, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		handleServiceError(c, nil, err)
		return
	}

	data := make([]SuggestionResponse, 0, len(recs))
	for i := range recs {
		data = append(data, toResponse(&recs[i]))
	}
	c.JSON(http.StatusOK, ListResponse{Object: "list", Data: data})
}

func handleServiceError(c *gin.Context, existing *models.PortalSuggestion, err error) {
	var verr *portalsvc.ValidationError
	switch {
	case errors.As(err, &verr):
		code := apierror.CodeParameterInvalid
		if verr.Missing {
			code = apierror.CodeParameterMissing
		}
		apierror.BadRequest(c, code, verr.Message, verr.Param)
	case errors.Is(err, portalsvc.ErrAlreadySuggested):
		msg := "You have already suggested this portal."
		if existing != nil {
			msg = "You have already suggested this portal as " + existing.ID.String() + "."
		}
		apierror.Conflict(c, CodePortalAlreadySuggested, msg, "url")
	case errors.Is(err, portalsvc.ErrAlreadySupported):
		apierror.Conflict(c, CodePortalAlreadySupported, "This portal is already searched. See GET /providers.", "url")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c)
	default:
		slog.ErrorContext(c.Request.Context(), "portal suggestions handler", "error", err)
		apierror.InternalServerError(c)
	}
}

func toResponse(p *models.PortalSuggestion) SuggestionResponse {
	return SuggestionResponse{
		Object:    "portal_suggestion",
		ID:        p.ID.String(),
		Name:      p.Name,
		URL:       p.URL,
		Note:      p.Note,
		Status:    p.Status,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}
