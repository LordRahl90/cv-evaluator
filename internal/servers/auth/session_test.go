package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"cv-evaluator/internal/servers/middleware"
	"cv-evaluator/internal/services/users"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
)

type sessionCheckerFunc func(ctx context.Context, userID ulid.ULID, issuedAt time.Time) error

func (f sessionCheckerFunc) ValidateSession(ctx context.Context, userID ulid.ULID, issuedAt time.Time) error {
	return f(ctx, userID, issuedAt)
}

func TestNewSessionValidator_MapsErrors(t *testing.T) {
	dbErr := errors.New("db down")
	cases := []struct {
		name        string
		err         error
		wantInvalid bool
	}{
		{"valid", nil, false},
		{"revoked", users.ErrSessionRevoked, true},
		{"user deleted", users.ErrUserNotFound, true},
		{"internal", dbErr, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewSessionValidator(sessionCheckerFunc(func(context.Context, ulid.ULID, time.Time) error { return tc.err }))
			err := v.ValidateSession(context.Background(), ulid.Make(), time.Now())
			assert.Equal(t, tc.wantInvalid, errors.Is(err, middleware.ErrInvalidToken))
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
