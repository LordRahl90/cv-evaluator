package middleware

import (
	"errors"
	"net/http"
	"strings"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"

	"github.com/gin-gonic/gin"
)

const UserIDKey = "user_id"

var ErrInvalidToken = errors.New("invalid token")

// Authenticate returns a gin middleware that validates a Bearer JWT and stores
// the authenticated user ID under UserIDKey in the gin context.
// Requests without a valid token are rejected with 401 Unauthorized.
func Authenticate(jwtSecret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawToken, err := extractBearer(c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed authorization header"})
			return
		}

		userID, err := parseUserID(rawToken, jwtSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set(UserIDKey, userID)
		c.Next()
	}
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
