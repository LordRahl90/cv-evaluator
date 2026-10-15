package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"cv-evaluator/internal/servers/apierror"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
)

const (
	// UserIDKey is the gin context key holding the authenticated user ULID.
	UserIDKey = "user_id"
	// AuthMethodKey is the gin context key holding how the request was
	// authenticated: AuthMethodJWT or AuthMethodAPIKey.
	AuthMethodKey = "auth_method"

	AuthMethodJWT    = "jwt"
	AuthMethodAPIKey = "api_key"

	// APIKeyPrefix marks a bearer credential as an API key rather than a JWT.
	APIKeyPrefix = "sk_"
)

var ErrInvalidToken = errors.New("invalid token")

// APIKeyVerifier resolves a raw API key to the ULID of the user who owns it.
//
// Implementations must return an error wrapping ErrInvalidToken when the key
// is unknown, revoked or otherwise unacceptable; the middleware answers those
// with 401. Any other error is treated as an internal failure (500).
type APIKeyVerifier interface {
	VerifyAPIKey(ctx context.Context, rawKey string) (ulid.ULID, error)
}

// APIKeyVerifierFunc adapts a plain function to APIKeyVerifier.
type APIKeyVerifierFunc func(ctx context.Context, rawKey string) (ulid.ULID, error)

// VerifyAPIKey calls f.
func (f APIKeyVerifierFunc) VerifyAPIKey(ctx context.Context, rawKey string) (ulid.ULID, error) {
	return f(ctx, rawKey)
}

// Option configures Authenticate.
type Option func(*authConfig)

type authConfig struct {
	apiKeys APIKeyVerifier
}

// WithAPIKeyVerifier enables API key authentication. Bearer credentials that
// start with APIKeyPrefix are passed to v instead of being parsed as JWTs.
// A nil verifier leaves API key authentication disabled.
func WithAPIKeyVerifier(v APIKeyVerifier) Option {
	return func(cfg *authConfig) {
		cfg.apiKeys = v
	}
}

// Authenticate returns a gin middleware that validates the Bearer credential
// and stores the authenticated user ID under UserIDKey (and the method under
// AuthMethodKey) in the gin context.
//
// By default only HS256 JWTs signed with jwtSecret are accepted. Pass
// WithAPIKeyVerifier to also accept personal API keys (sk_...).
// Requests without valid credentials are rejected with 401 Unauthorized and
// an apierror body.
func Authenticate(jwtSecret []byte, opts ...Option) gin.HandlerFunc {
	cfg := authConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(c *gin.Context) {
		credential, err := extractBearer(c.GetHeader("Authorization"))
		if err != nil {
			apierror.Unauthorized(c, apierror.CodeMissingCredentials,
				"No valid credentials provided. Send an 'Authorization: Bearer <token>' header with a session token or API key.")
			return
		}

		if strings.HasPrefix(credential, APIKeyPrefix) {
			authenticateAPIKey(c, cfg.apiKeys, credential)
			return
		}

		userID, err := parseUserID(credential, jwtSecret)
		if err != nil {
			apierror.Unauthorized(c, apierror.CodeInvalidToken, "The session token is invalid or has expired.")
			return
		}

		c.Set(UserIDKey, userID)
		c.Set(AuthMethodKey, AuthMethodJWT)
		c.Next()
	}
}

func authenticateAPIKey(c *gin.Context, verifier APIKeyVerifier, rawKey string) {
	const invalidKeyMsg = "The API key provided is invalid or has been revoked."

	if verifier == nil {
		apierror.Unauthorized(c, apierror.CodeInvalidAPIKey, "API keys are not accepted on this endpoint.")
		return
	}

	userID, err := verifier.VerifyAPIKey(c.Request.Context(), rawKey)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) {
			apierror.Unauthorized(c, apierror.CodeInvalidAPIKey, invalidKeyMsg)
			return
		}
		slog.ErrorContext(c.Request.Context(), "verify api key", "error", err)
		apierror.Abort(c, http.StatusInternalServerError, apierror.Internal())
		return
	}
	if userID == (ulid.ULID{}) {
		apierror.Unauthorized(c, apierror.CodeInvalidAPIKey, invalidKeyMsg)
		return
	}

	c.Set(UserIDKey, userID)
	c.Set(AuthMethodKey, AuthMethodAPIKey)
	c.Next()
}

// UserID returns the authenticated user ULID stored by Authenticate.
func UserID(c *gin.Context) (ulid.ULID, bool) {
	raw, exists := c.Get(UserIDKey)
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

// extractBearer parses the value of an Authorization header and returns the
// bare token string. It expects the format "Bearer <token>".
func extractBearer(header string) (string, error) {
	if header == "" {
		return "", ErrInvalidToken
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", ErrInvalidToken
	}

	return strings.TrimSpace(parts[1]), nil
}

// parseUserID validates the JWT and returns the subject claim as a ULID.
func parseUserID(rawToken string, secret []byte) (ulid.ULID, error) {
	claims := &jwt.RegisteredClaims{}

	token, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return ulid.ULID{}, ErrInvalidToken
	}

	id, err := ulid.ParseStrict(claims.Subject)
	if err != nil || id == (ulid.ULID{}) {
		return ulid.ULID{}, ErrInvalidToken
	}

	return id, nil
}
