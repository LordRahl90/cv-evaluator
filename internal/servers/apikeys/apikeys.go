// Package apikeys exposes routes for managing personal API keys and an
// adapter that lets the auth middleware authenticate requests with them.
package apikeys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"
	apikeysvc "cv-evaluator/internal/services/apikeys"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

// Service is the domain interface the handler depends on.
// *apikeysvc.Service satisfies it.
type Service interface {
	Create(ctx context.Context, userID ulid.ULID, name string) (*apikeysvc.CreatedKey, error)
	List(ctx context.Context, userID ulid.ULID) ([]apikeysvc.Key, error)
	Delete(ctx context.Context, userID, keyID ulid.ULID) error
}

// KeyVerifier is the subset of *apikeysvc.Service used to authenticate requests.
type KeyVerifier interface {
	Verify(ctx context.Context, rawKey string) (ulid.ULID, error)
}

// NewVerifier adapts the API key service to middleware.APIKeyVerifier,
// translating apikeysvc.ErrInvalidKey into middleware.ErrInvalidToken so the
// middleware answers with 401 (and with 500 for anything else).
func NewVerifier(svc KeyVerifier) middleware.APIKeyVerifier {
	return middleware.APIKeyVerifierFunc(func(ctx context.Context, rawKey string) (ulid.ULID, error) {
		userID, err := svc.Verify(ctx, rawKey)
		if errors.Is(err, apikeysvc.ErrInvalidKey) {
			return ulid.ULID{}, fmt.Errorf("%w: %w", middleware.ErrInvalidToken, err)
		}
		return userID, err
	})
}

type createRequest struct {
	Name string `json:"name"`
}

// KeyResponse is the public representation of an API key. Secret is only
// populated in the response to POST /api-keys.
type KeyResponse struct {
	Object     string     `json:"object"`
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Secret     string     `json:"secret,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// ListResponse wraps a page of keys.
type ListResponse struct {
	Object string        `json:"object"`
	Data   []KeyResponse `json:"data"`
}

// DeletedResponse confirms a revocation.
type DeletedResponse struct {
	Object  string `json:"object"`
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// Handler wires the Service to HTTP routes.
type Handler struct {
	svc Service
}

// New registers the API key routes on router, all authenticated and scoped
// to the caller:
//
//	POST   /api-keys
//	GET    /api-keys
//	DELETE /api-keys/:id
//
// opts are passed to middleware.Authenticate, for example
// middleware.WithAPIKeyVerifier to accept API keys as well as JWTs and
// middleware.WithSessionValidator to reject revoked sessions.
func New(router gin.IRouter, svc Service, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{svc: svc}

	group := router.Group("/api-keys")
	group.Use(middleware.Authenticate(jwtSecret, opts...))
	group.POST("", h.create)
	group.GET("", h.list)
	group.DELETE("/:id", h.delete)

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

	key, err := h.svc.Create(c.Request.Context(), userID, req.Name)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	resp := toResponse(key.Key)
	resp.Secret = key.Secret
	c.JSON(http.StatusCreated, resp)
}

func (h *Handler) list(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
		return
	}

	keys, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	data := make([]KeyResponse, 0, len(keys))
	for _, k := range keys {
		data = append(data, toResponse(k))
	}
	c.JSON(http.StatusOK, ListResponse{Object: "list", Data: data})
}

func (h *Handler) delete(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
		return
	}

	keyID, err := ulid.ParseStrict(c.Param("id"))
	if err != nil {
		apierror.BadRequest(c, apierror.CodeParameterInvalid, "The id parameter must be a valid API key id.", "id")
		return
	}

	if err := h.svc.Delete(c.Request.Context(), userID, keyID); err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, DeletedResponse{Object: "api_key", ID: keyID.String(), Deleted: true})
}

func handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, apikeysvc.ErrNameRequired):
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The name parameter is required.", "name")
	case errors.Is(err, apikeysvc.ErrNameTooLong):
		apierror.BadRequest(c, apierror.CodeParameterInvalid,
			fmt.Sprintf("The name parameter must be at most %d characters long.", apikeysvc.MaxNameLength), "name")
	case errors.Is(err, apikeysvc.ErrNotFound):
		apierror.NotFound(c, "No such API key.", "id")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c, err)
	default:
		slog.ErrorContext(c.Request.Context(), "api keys handler", "error", err)
		apierror.InternalServerError(c, err)
	}
}

func toResponse(k apikeysvc.Key) KeyResponse {
	return KeyResponse{
		Object:     "api_key",
		ID:         k.ID.String(),
		Name:       k.Name,
		Prefix:     k.Prefix,
		CreatedAt:  k.CreatedAt,
		LastUsedAt: k.LastUsedAt,
	}
}
