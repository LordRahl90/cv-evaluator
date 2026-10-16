package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sessionCall records what the validator was asked.
type sessionCall struct {
	userID   ulid.ULID
	issuedAt time.Time
}

// revokedBefore rejects tokens issued before cutoff, like a password change.
func revokedBefore(cutoff time.Time, calls *[]sessionCall) SessionValidator {
	return SessionValidatorFunc(func(_ context.Context, userID ulid.ULID, issuedAt time.Time) error {
		*calls = append(*calls, sessionCall{userID, issuedAt})
		if issuedAt.Before(cutoff) {
			return fmt.Errorf("revoked: %w", ErrInvalidToken)
		}
		return nil
	})
}

func tokenIssuedAt(t *testing.T, subject string, iat time.Time) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(iat),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testSecret)
	require.NoError(t, err)
	return tok
}

func TestAuthenticate_SessionValidator_AcceptsCurrentSession(t *testing.T) {
	userID := ulid.Make()
	issued := time.Now().Truncate(time.Second)
	var calls []sessionCall
	r := newRouterWithOpts(WithSessionValidator(revokedBefore(issued.Add(-time.Minute), &calls)))

	w := performRequest(r, "Bearer "+tokenIssuedAt(t, userID.String(), issued))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, calls, 1)
	assert.Equal(t, userID, calls[0].userID)
	assert.True(t, issued.Equal(calls[0].issuedAt), "the validator gets the iat claim")
}

func TestAuthenticate_SessionValidator_RejectsRevokedSession(t *testing.T) {
	cutoff := time.Now().Truncate(time.Second)
	var calls []sessionCall
	r := newRouterWithOpts(WithSessionValidator(revokedBefore(cutoff, &calls)))

	w := performRequest(r, "Bearer "+tokenIssuedAt(t, ulid.Make().String(), cutoff.Add(-time.Hour)))

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid_token", decodeError(t, w).Error.Code)
	assert.NotEmpty(t, w.Header().Get("WWW-Authenticate"))
}

func TestAuthenticate_SessionValidator_MissingIssuedAtIsZero(t *testing.T) {
	claims := jwt.RegisteredClaims{
		Subject:   ulid.Make().String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testSecret)
	require.NoError(t, err)

	var calls []sessionCall
	r := newRouterWithOpts(WithSessionValidator(revokedBefore(time.Unix(1, 0), &calls)))
	w := performRequest(r, "Bearer "+tok)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Len(t, calls, 1)
	assert.True(t, calls[0].issuedAt.IsZero())
}

func TestAuthenticate_SessionValidator_FailureIs500(t *testing.T) {
	r := newRouterWithOpts(WithSessionValidator(SessionValidatorFunc(
		func(context.Context, ulid.ULID, time.Time) error { return errors.New("connection refused") })))

	w := performRequest(r, "Bearer "+makeToken(t, ulid.Make().String(), time.Hour))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "internal_error", decodeError(t, w).Error.Code)
}

func TestAuthenticate_SessionValidator_NotCalledForBadOrAPIKeyCredentials(t *testing.T) {
	owner := ulid.Make()
	var calls []sessionCall
	var keyCalls int
	r := newRouterWithOpts(
		WithAPIKeyVerifier(stubVerifier(owner, &keyCalls)),
		WithSessionValidator(revokedBefore(time.Now().Add(time.Hour), &calls)),
	)

	// API keys are revoked separately and stay valid across password changes.
	w := performRequest(r, "Bearer "+validTestKey)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// A token that fails signature or expiry checks never reaches the validator.
	w = performRequest(r, "Bearer "+makeToken(t, ulid.Make().String(), -time.Minute))
	require.Equal(t, http.StatusUnauthorized, w.Code)

	assert.Empty(t, calls)
}

func TestAuthenticate_NilSessionValidatorOption(t *testing.T) {
	r := newRouterWithOpts(WithSessionValidator(nil))
	w := performRequest(r, "Bearer "+makeToken(t, ulid.Make().String(), time.Hour))
	assert.Equal(t, http.StatusOK, w.Code)
}
