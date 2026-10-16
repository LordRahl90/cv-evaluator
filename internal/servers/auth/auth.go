// Package auth exposes account routes: sign up, sign in and the current user.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"cv-evaluator/internal/servers/apierror"
	"cv-evaluator/internal/servers/middleware"
	"cv-evaluator/internal/services/users"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
)

const (
	// MinPasswordLength is the shortest password accepted at signup.
	MinPasswordLength = 8
	// MaxPasswordBytes is bcrypt's input limit; longer passwords are rejected
	// rather than silently truncated.
	MaxPasswordBytes = 72
)

// Service is the domain interface the handler depends on.
// *users.Service satisfies it.
type Service interface {
	Signup(ctx context.Context, req users.SignupRequest) (*users.AuthResponse, error)
	Signin(ctx context.Context, req users.SigninRequest) (*users.AuthResponse, error)
	GetProfile(ctx context.Context, userID ulid.ULID) (*users.Profile, error)
}

// SessionChecker is the subset of *users.Service that decides whether a
// session token is still valid.
type SessionChecker interface {
	ValidateSession(ctx context.Context, userID ulid.ULID, issuedAt time.Time) error
}

// NewSessionValidator adapts the users service to
// middleware.SessionValidator, translating users.ErrSessionRevoked (token
// issued before the last password change) and users.ErrUserNotFound (the
// account was deleted) into middleware.ErrInvalidToken so the middleware
// answers with 401 (and with 500 for anything else).
func NewSessionValidator(svc SessionChecker) middleware.SessionValidator {
	return middleware.SessionValidatorFunc(func(ctx context.Context, userID ulid.ULID, issuedAt time.Time) error {
		err := svc.ValidateSession(ctx, userID, issuedAt)
		if errors.Is(err, users.ErrSessionRevoked) || errors.Is(err, users.ErrUserNotFound) {
			return fmt.Errorf("%w: %w", middleware.ErrInvalidToken, err)
		}
		return err
	})
}

type signupRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Password  string `json:"password"`
}

type signinRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UserResponse is the public representation of a user.
type UserResponse struct {
	Object    string `json:"object"`
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

// SessionResponse is returned by signup and signin.
type SessionResponse struct {
	Object    string       `json:"object"`
	Token     string       `json:"token"`
	TokenType string       `json:"token_type"`
	User      UserResponse `json:"user"`
}

// Handler wires the Service to HTTP routes.
type Handler struct {
	svc Service
}

// New registers the account routes on router:
//
//	POST /auth/signup  (public)
//	POST /auth/signin  (public)
//	GET  /me           (authenticated)
//
// opts are passed to middleware.Authenticate for /me, for example
// middleware.WithAPIKeyVerifier to accept API keys as well as JWTs.
// The other account routes (PATCH and DELETE /me, ...) are registered by
// package account.
func New(router gin.IRouter, svc Service, jwtSecret []byte, opts ...middleware.Option) *Handler {
	h := &Handler{svc: svc}

	router.POST("/auth/signup", h.signup)
	router.POST("/auth/signin", h.signin)
	router.GET("/me", middleware.Authenticate(jwtSecret, opts...), h.me)

	return h
}

func (h *Handler) signup(c *gin.Context) {
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.BadRequest(c, apierror.CodeInvalidJSON, "The request body must be a valid JSON object.", "")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if apiErr := validateSignup(req); apiErr != nil {
		apierror.Write(c, http.StatusBadRequest, *apiErr)
		return
	}

	resp, err := h.svc.Signup(c.Request.Context(), users.SignupRequest{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Email:     req.Email,
		Phone:     req.Phone,
		Password:  req.Password,
	})
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, ToSession(resp))
}

func (h *Handler) signin(c *gin.Context) {
	var req signinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.BadRequest(c, apierror.CodeInvalidJSON, "The request body must be a valid JSON object.", "")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The email parameter is required.", "email")
		return
	}
	if req.Password == "" {
		apierror.BadRequest(c, apierror.CodeParameterMissing, "The password parameter is required.", "password")
		return
	}

	resp, err := h.svc.Signin(c.Request.Context(), users.SigninRequest{Email: req.Email, Password: req.Password})
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, ToSession(resp))
}

func (h *Handler) me(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		apierror.Unauthorized(c, apierror.CodeMissingCredentials, "Authentication required.")
		return
	}

	profile, err := h.svc.GetProfile(c.Request.Context(), userID)
	if errors.Is(err, users.ErrUserNotFound) {
		// The credential was valid but its user no longer exists.
		apierror.Unauthorized(c, apierror.CodeInvalidToken, "The authenticated user no longer exists.")
		return
	}
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, ToUser(*profile))
}

func validateSignup(req signupRequest) *apierror.Error {
	invalid := func(code, msg, param string) *apierror.Error {
		e := apierror.InvalidRequest(code, msg, param)
		return &e
	}

	switch {
	case req.Email == "":
		return invalid(apierror.CodeParameterMissing, "The email parameter is required.", "email")
	case !ValidEmail(req.Email):
		return invalid(apierror.CodeParameterInvalid, "The email parameter must be a valid email address.", "email")
	}
	return ValidatePassword(req.Password, "password")
}

// ValidatePassword checks a new password against the signup rules: present,
// at least MinPasswordLength characters and at most MaxPasswordBytes bytes.
// param names the request parameter in the returned error.
func ValidatePassword(password, param string) *apierror.Error {
	invalid := func(code, msg string) *apierror.Error {
		e := apierror.InvalidRequest(code, msg, param)
		return &e
	}

	switch {
	case password == "":
		return invalid(apierror.CodeParameterMissing, fmt.Sprintf("The %s parameter is required.", param))
	case len([]rune(password)) < MinPasswordLength:
		return invalid(apierror.CodeParameterInvalid, fmt.Sprintf("The password must be at least %d characters long.", MinPasswordLength))
	case len(password) > MaxPasswordBytes:
		return invalid(apierror.CodeParameterInvalid, fmt.Sprintf("The password must be at most %d bytes long.", MaxPasswordBytes))
	}
	return nil
}

// ValidEmail reports whether email is a bare email address (no display
// name), as signup requires.
func ValidEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

func handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, users.ErrInvalidCredentials):
		apierror.Unauthorized(c, apierror.CodeInvalidCredentials, "The email or password is incorrect.")
	case errors.Is(err, users.ErrEmailAlreadyUsed):
		apierror.Conflict(c, apierror.CodeEmailAlreadyUsed, "An account with this email already exists.", "email")
	case errors.Is(err, users.ErrPhoneAlreadyUsed):
		apierror.Conflict(c, apierror.CodePhoneAlreadyUsed, "An account with this phone number already exists.", "phone")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apierror.GatewayTimeout(c)
	default:
		slog.ErrorContext(c.Request.Context(), "auth handler", "error", err)
		apierror.InternalServerError(c)
	}
}

// ToSession renders a session (a token and its user).
func ToSession(resp *users.AuthResponse) SessionResponse {
	return SessionResponse{
		Object:    "session",
		Token:     resp.Token,
		TokenType: "Bearer",
		User:      ToUser(resp.User),
	}
}

// ToUser renders a user profile.
func ToUser(p users.Profile) UserResponse {
	return UserResponse{
		Object:    "user",
		ID:        p.ID.String(),
		FirstName: p.FirstName,
		LastName:  p.LastName,
		Email:     p.Email,
		Phone:     p.Phone,
	}
}
