// Package account exposes the routes a signed-in user manages their own
// account with: edit the profile, change the password, export their data
// and delete the account. Signing up, signing in and reading the profile
// (GET /me) stay in package auth, which issues sessions; the routes here
// change or remove what a session belongs to.
package account

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/auth"
	"cv-evaluator/internal/servers/middleware"
	accountsvc "cv-evaluator/internal/services/account"
	"cv-evaluator/internal/services/users"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

// CodeIncorrectPassword is returned when the password confirming a change
// (current_password, or password on deletion) is wrong. It is a 400, not a
// 401: the caller is authenticated, they only mistyped their password.
const CodeIncorrectPassword = "incorrect_password"

// ExportFilename is the file name the export is offered as.
const ExportFilename = "cv-evaluator-export.json"

// Profiles is the part of *users.Service the handler depends on.
type Profiles interface {
	UpdateProfile(ctx context.Context, userID ulid.ULID, req users.UpdateProfileRequest) (*users.Profile, error)
	ChangePassword(ctx context.Context, userID ulid.ULID, current, next string) (*users.AuthResponse, error)
	VerifyPassword(ctx context.Context, userID ulid.ULID, password string) error
}

// Data is the part of *accountsvc.Service the handler depends on.
type Data interface {
	Delete(ctx context.Context, userID ulid.ULID) ([]accountsvc.Deleted, error)
	Export(ctx context.Context, userID ulid.ULID) (*accountsvc.Export, error)
}

type updateRequest struct {
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	Email     *string `json:"email"`
	Phone     *string `json:"phone"`
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type deleteRequest struct {
	Password string `json:"password"`
}

// Handler wires the services to HTTP routes.
type Handler struct {
	profiles Profiles
	data     Data
}

// New registers the account routes on router, all authenticated and scoped
// to the caller:
//
//	PATCH  /me           update the profile
//	POST   /me/password  change the password (returns a fresh session)
//	DELETE /me           delete the account and everything it owns
//	GET    /me/export    download everything stored about the caller
//
// opts are passed to middleware.Authenticate, for example
// middleware.WithAPIKeyVerifier to accept API keys as well as JWTs and
// middleware.WithSessionValidator to reject sessions issued before the
// last password change.
func New(router gin.IRouter, profiles Profiles, data Data, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{profiles: profiles, data: data}

	authn := middleware.Authenticate(jwtSecret, opts...)
	router.PATCH("/me", authn, h.update)
	router.DELETE("/me", authn, h.delete)
	router.POST("/me/password", authn, h.changePassword)
	router.GET("/me/export", authn, h.export)

	return h
}

func (h *Handler) update(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}

	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		invalidJSON(c)
		return
	}
	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		switch {
		case email == "":
			apierror.BadRequest(c, apierror.CodeParameterInvalid, "The email parameter cannot be empty.", "email")
			return
		case !auth.ValidEmail(email):
			apierror.BadRequest(c, apierror.CodeParameterInvalid, "The email parameter must be a valid email address.", "email")
			return
		}
		req.Email = &email
	}

	profile, err := h.profiles.UpdateProfile(c.Request.Context(), userID, users.UpdateProfileRequest(req))
	if err != nil {
		handleServiceError(c, err, "")
		return
	}
	c.JSON(http.StatusOK, auth.ToUser(*profile))
}

func (h *Handler) changePassword(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}

	var req passwordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		invalidJSON(c)
		return
	}
	if req.CurrentPassword == "" {
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The current_password parameter is required.", "current_password")
		return
	}
	if apiErr := auth.ValidatePassword(req.NewPassword, "new_password"); apiErr != nil {
		apierror.Write(c, http.StatusBadRequest, *apiErr)
		return
	}

	session, err := h.profiles.ChangePassword(c.Request.Context(), userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		handleServiceError(c, err, "current_password")
		return
	}
	c.JSON(http.StatusOK, auth.ToSession(session))
}

func (h *Handler) delete(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}

	var req deleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		invalidJSON(c)
		return
	}
	if req.Password == "" {
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The password parameter is required to delete the account.", "password")
		return
	}

	// Re-authenticate even when the caller used an API key: deleting the
	// account must take the password.
	ctx := c.Request.Context()
	if err := h.profiles.VerifyPassword(ctx, userID, req.Password); err != nil {
		handleServiceError(c, err, "password")
		return
	}

	deleted, err := h.data.Delete(ctx, userID)
	if err != nil {
		handleServiceError(c, err, "")
		return
	}

	// Audit trail: the opaque id and row counts only, no personal data.
	attrs := []any{"user_id", userID.String(), "auth_method", c.GetString(middleware.AuthMethodKey)}
	for _, d := range deleted {
		attrs = append(attrs, d.Table, d.Rows)
	}
	slog.InfoContext(ctx, "account deleted", attrs...)

	c.Status(http.StatusNoContent)
}

func (h *Handler) export(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}

	data, err := h.data.Export(c.Request.Context(), userID)
	if err != nil {
		handleServiceError(c, err, "")
		return
	}

	c.Header("Content-Disposition", `attachment; filename="`+ExportFilename+`"`)
	c.Header("Cache-Control", "no-store")
	c.IndentedJSON(http.StatusOK, data)
}

func requireUser(c *gin.Context) (ulid.ULID, bool) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
	}
	return userID, ok
}

func invalidJSON(c *gin.Context) {
	apierror.BadRequest(c, apierror.CodeInvalidJSON, "The request body must be a valid JSON object.", "")
}

// handleServiceError renders err. passwordParam names the parameter an
// incorrect password is reported against.
func handleServiceError(c *gin.Context, err error, passwordParam string) {
	switch {
	case errors.Is(err, users.ErrIncorrectPassword):
		apierror.BadRequest(c, CodeIncorrectPassword, "The password is incorrect.", passwordParam)
	case errors.Is(err, users.ErrEmailAlreadyUsed):
		apierror.Conflict(c, apierror.CodeEmailAlreadyUsed, "An account with this email already exists.", "email")
	case errors.Is(err, users.ErrPhoneAlreadyUsed):
		apierror.Conflict(c, apierror.CodePhoneAlreadyUsed, "An account with this phone number already exists.", "phone")
	case errors.Is(err, users.ErrUserNotFound), errors.Is(err, accountsvc.ErrUserNotFound):
		apierror.NotFound(c, "The account does not exist.", "")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c)
	default:
		slog.ErrorContext(c.Request.Context(), "account handler", "error", err)
		apierror.InternalServerError(c)
	}
}
