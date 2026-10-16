// Package jobsearches exposes routes for managing saved job searches and
// listing the job portals every search runs against.
package jobsearches

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/providers"
	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"
	jobsearchsvc "cv-evaluator/internal/services/jobsearches"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

// CodeJobSearchDisabled is returned when running a disabled search.
const CodeJobSearchDisabled = "job_search_disabled"

// Service is the domain interface the handler depends on.
// *jobsearchsvc.Service satisfies it.
type Service interface {
	Create(ctx context.Context, userID ulid.ULID, p jobsearchsvc.CreateParams) (*models.JobSearch, error)
	List(ctx context.Context, userID ulid.ULID) ([]models.JobSearch, error)
	Get(ctx context.Context, userID, id ulid.ULID) (*models.JobSearch, error)
	Update(ctx context.Context, userID, id ulid.ULID, p jobsearchsvc.UpdateParams) (*models.JobSearch, error)
	Delete(ctx context.Context, userID, id ulid.ULID) error
	RunNow(ctx context.Context, userID, id ulid.ULID) (*models.JobSearch, error)
}

// ProviderLister lists the supported job portals. *providers.Registry
// satisfies it.
type ProviderLister interface {
	Names() []string
	Get(name string) (providers.Provider, bool)
}

type createRequest struct {
	Name      string `json:"name"`
	Query     string `json:"query"`
	Location  string `json:"location"`
	Frequency string `json:"frequency"`
	Enabled   *bool  `json:"enabled"`
}

type updateRequest struct {
	Name      *string `json:"name"`
	Query     *string `json:"query"`
	Location  *string `json:"location"`
	Frequency *string `json:"frequency"`
	Enabled   *bool   `json:"enabled"`
}

// JobSearchResponse is the public representation of a job search.
type JobSearchResponse struct {
	Object    string     `json:"object"`
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Query     string     `json:"query"`
	Location  string     `json:"location"`
	Frequency string     `json:"frequency"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ListResponse wraps a list of job searches.
type ListResponse struct {
	Object string              `json:"object"`
	Data   []JobSearchResponse `json:"data"`
}

// DeletedResponse confirms a deletion.
type DeletedResponse struct {
	Object  string `json:"object"`
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// ProviderResponse is the public representation of a supported job portal.
type ProviderResponse struct {
	Object      string `json:"object"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Website     string `json:"website"`
	Coverage    string `json:"coverage"`
}

// ProviderListResponse wraps the list of providers.
type ProviderListResponse struct {
	Object string             `json:"object"`
	Data   []ProviderResponse `json:"data"`
}

// Handler wires the Service to HTTP routes.
type Handler struct {
	svc       Service
	providers ProviderLister
}

// New registers the job search routes on router, all authenticated and
// scoped to the caller:
//
//	POST   /job-searches
//	GET    /job-searches
//	GET    /job-searches/:id
//	PATCH  /job-searches/:id
//	DELETE /job-searches/:id
//	POST   /job-searches/:id/run
//	GET    /providers  (read-only list of supported job portals)
//
// opts are passed to middleware.Authenticate, for example
// middleware.WithAPIKeyVerifier to accept API keys as well as JWTs and
// middleware.WithSessionValidator to reject revoked sessions.
func New(router gin.IRouter, svc Service, providerList ProviderLister, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{svc: svc, providers: providerList}
	auth := middleware.Authenticate(jwtSecret, opts...)

	group := router.Group("/job-searches", auth)
	group.POST("", h.create)
	group.GET("", h.list)
	group.GET("/:id", h.get)
	group.PATCH("/:id", h.update)
	group.DELETE("/:id", h.delete)
	group.POST("/:id/run", h.run)

	router.GET("/providers", auth, h.listProviders)

	return h
}

func (h *Handler) create(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		invalidJSON(c)
		return
	}

	js, err := h.svc.Create(c.Request.Context(), userID, jobsearchsvc.CreateParams(req))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toResponse(js))
}

func (h *Handler) list(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}

	searches, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	data := make([]JobSearchResponse, 0, len(searches))
	for i := range searches {
		data = append(data, toResponse(&searches[i]))
	}
	c.JSON(http.StatusOK, ListResponse{Object: "list", Data: data})
}

func (h *Handler) get(c *gin.Context) {
	userID, id, ok := requireUserAndID(c)
	if !ok {
		return
	}

	js, err := h.svc.Get(c.Request.Context(), userID, id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, toResponse(js))
}

func (h *Handler) update(c *gin.Context) {
	userID, id, ok := requireUserAndID(c)
	if !ok {
		return
	}

	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		invalidJSON(c)
		return
	}

	js, err := h.svc.Update(c.Request.Context(), userID, id, jobsearchsvc.UpdateParams(req))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, toResponse(js))
}

func (h *Handler) delete(c *gin.Context) {
	userID, id, ok := requireUserAndID(c)
	if !ok {
		return
	}

	if err := h.svc.Delete(c.Request.Context(), userID, id); err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, DeletedResponse{Object: "job_search", ID: id.String(), Deleted: true})
}

func (h *Handler) run(c *gin.Context) {
	userID, id, ok := requireUserAndID(c)
	if !ok {
		return
	}

	js, err := h.svc.RunNow(c.Request.Context(), userID, id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, toResponse(js))
}

func (h *Handler) listProviders(c *gin.Context) {
	names := h.providers.Names()
	data := make([]ProviderResponse, 0, len(names))
	for _, name := range names {
		p, ok := h.providers.Get(name)
		if !ok {
			continue
		}
		resp := ProviderResponse{Object: "provider", Name: name, DisplayName: name}
		if d, ok := p.(providers.Describer); ok {
			info := d.Info()
			resp.DisplayName = info.DisplayName
			resp.Website = info.Website
			resp.Coverage = info.Coverage
		}
		data = append(data, resp)
	}
	c.JSON(http.StatusOK, ProviderListResponse{Object: "list", Data: data})
}

func requireUser(c *gin.Context) (ulid.ULID, bool) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
	}
	return userID, ok
}

func requireUserAndID(c *gin.Context) (ulid.ULID, ulid.ULID, bool) {
	userID, ok := requireUser(c)
	if !ok {
		return ulid.ULID{}, ulid.ULID{}, false
	}
	id, err := ulid.ParseStrict(c.Param("id"))
	if err != nil {
		apierror.BadRequest(c, apierror.CodeParameterInvalid, "The id parameter must be a valid job search id.", "id")
		return ulid.ULID{}, ulid.ULID{}, false
	}
	return userID, id, true
}

func invalidJSON(c *gin.Context) {
	apierror.BadRequest(c, apierror.CodeInvalidJSON, "The request body must be a valid JSON object.", "")
}

func handleServiceError(c *gin.Context, err error) {
	var verr *jobsearchsvc.ValidationError
	switch {
	case errors.As(err, &verr):
		code := apierror.CodeParameterInvalid
		if verr.Missing {
			code = apierror.CodeParameterMissing
		}
		apierror.BadRequest(c, code, verr.Message, verr.Param)
	case errors.Is(err, jobsearchsvc.ErrNotFound):
		apierror.NotFound(c, "No such job search.", "id")
	case errors.Is(err, jobsearchsvc.ErrDisabled):
		apierror.Conflict(c, CodeJobSearchDisabled, "The job search is disabled. Enable it before running it.", "")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c)
	default:
		slog.ErrorContext(c.Request.Context(), "job searches handler", "error", err)
		apierror.InternalServerError(c)
	}
}

func toResponse(js *models.JobSearch) JobSearchResponse {
	return JobSearchResponse{
		Object:    "job_search",
		ID:        js.ID.String(),
		Name:      js.Name,
		Query:     js.Query,
		Location:  js.Location,
		Frequency: js.Frequency,
		Enabled:   js.Enabled,
		LastRunAt: js.LastRunAt,
		NextRunAt: js.NextRunAt,
		CreatedAt: js.CreatedAt,
		UpdatedAt: js.UpdatedAt,
	}
}
