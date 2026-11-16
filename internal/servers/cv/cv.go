package cv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"

	"cv-evaluator/internal/integrations/extractor"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"
	"cv-evaluator/internal/services/users"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

// Service is the domain interface the handler depends on.
type Service interface {
	UploadCV(ctx context.Context, userID ulid.ULID, cv *multipart.FileHeader) error
	ListUserCVs(ctx context.Context, userID ulid.ULID) ([]models.CV, error)
	GetCV(ctx context.Context, userID, cvID ulid.ULID) (*models.CV, error)
	DeleteCV(ctx context.Context, userID, cvID ulid.ULID) error
}

// cvSummary is the JSON shape returned in list / after upload.
type cvSummary struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Handler wires the Service to HTTP routes.
type Handler struct {
	svc Service
}

// New registers all CV routes on the provided router, protected by the auth
// middleware. opts are passed to middleware.Authenticate, for example
// middleware.WithAPIKeyVerifier to accept API keys as well as JWTs.
func New(router *gin.Engine, svc Service, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{svc: svc}
	h.registerRoutes(router, jwtSecret, opts)
	return h
}

func (h *Handler) registerRoutes(router *gin.Engine, jwtSecret []byte, opts []middleware.Option) {
	cvGroup := router.Group("cv")
	cvGroup.Use(middleware.Authenticate(jwtSecret, opts...))
	{
		cvGroup.POST("/", h.uploadCV)
		cvGroup.GET("/", h.listCVs)
		cvGroup.GET("/download/:id", h.downloadCV)
		cvGroup.PATCH("/", h.updateCV)
		cvGroup.DELETE("/:id", h.deleteCV)
	}
}

// uploadCV handles POST /cv/ — accepts a multipart "cv" field, processes it immediately.
func (h *Handler) uploadCV(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("unauthorized"))
		return
	}

	fileHeader, err := c.FormFile("cv")
	if err != nil {
		if maxBytesErr := new(http.MaxBytesError); errors.As(err, &maxBytesErr) {
			apierror.Write(c, http.StatusRequestEntityTooLarge, apierror.InvalidRequest(apierror.CodeBodyTooLarge,
				fmt.Sprintf("The CV must be a PDF of at most %d MB.", extractor.MaxFileBytes>>20), "cv"))
			return
		}
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The cv file is required.", "cv")
		return
	}

	if err := h.svc.UploadCV(c.Request.Context(), userID, fileHeader); err != nil {
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusAccepted)
}

// listCVs handles GET /cv/ — returns all CVs for the authenticated user.
func (h *Handler) listCVs(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("unauthorized"))
		return
	}

	cvs, err := h.svc.ListUserCVs(c.Request.Context(), userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	resp := make([]cvSummary, 0, len(cvs))
	for _, cv := range cvs {
		resp = append(resp, cvSummary{
			ID:        cv.ID.String(),
			CreatedAt: cv.CreatedAt,
			UpdatedAt: cv.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, resp)
}

// downloadCV handles GET /cv/download/:id — streams the extracted CV content as a text file.
// Only the owning user can download their CV.
func (h *Handler) downloadCV(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("unauthorized"))
		return
	}

	cvID, err := ulid.ParseStrict(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid cv id"))
		return
	}

	cv, err := h.svc.GetCV(c.Request.Context(), userID, cvID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.Header("Content-Disposition", "attachment; filename=\"cv.txt\"")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(cv.ExtractedContent))
}

// updateCV is pending implementation.
func (h *Handler) updateCV(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, errorResponse("not implemented"))
}

// deleteCV handles DELETE /cv/:id — deletes the authenticated user's CV.
// Returns 404 if the CV does not exist or belongs to a different user.
func (h *Handler) deleteCV(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("unauthorized"))
		return
	}

	cvID, err := ulid.ParseStrict(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid cv id"))
		return
	}

	if err := h.svc.DeleteCV(c.Request.Context(), userID, cvID); err != nil {
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// getUserID retrieves the authenticated user ULID stored by the auth middleware.
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

// handleServiceError maps well-known service errors to HTTP responses.
// Anything else is logged and answered with a generic 500, so internal
// details (database, embedding or parser errors) never reach the client.
func handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, users.ErrCVNotFound):
		apierror.NotFound(c, "No CV has that id.", "id")
	case errors.Is(err, extractor.ErrTooLarge):
		apierror.Write(c, http.StatusRequestEntityTooLarge, apierror.InvalidRequest(apierror.CodeBodyTooLarge,
			fmt.Sprintf("The CV must be a PDF of at most %d MB.", extractor.MaxFileBytes>>20), "cv"))
	case extractor.IsInvalidPDF(err):
		slog.InfoContext(c.Request.Context(), "cv upload rejected", "error", err)
		apierror.Write(c, http.StatusUnprocessableEntity, apierror.InvalidRequest(CodeInvalidPDF, invalidPDFMessage(err), "cv"))
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c)
	default:
		slog.ErrorContext(c.Request.Context(), "cv request failed", "error", err)
		apierror.InternalServerError(c)
	}
}

// CodeInvalidPDF is the error code for an upload that is not a usable PDF.
const CodeInvalidPDF = "invalid_pdf"

// invalidPDFMessage explains a rejected upload without parser details.
func invalidPDFMessage(err error) string {
	switch {
	case errors.Is(err, extractor.ErrNotPDF):
		return "The CV must be a PDF file."
	case errors.Is(err, extractor.ErrTooManyPages):
		return fmt.Sprintf("The CV must have at most %d pages.", extractor.MaxPages)
	default:
		return "We could not read text from this PDF. Export it again as a text-based PDF and retry."
	}
}

func errorResponse(msg string) gin.H {
	return gin.H{"error": msg}
}
