package users

import (
	"context"
	"testing"
	"time"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(s string) *string { return &s }

func signup(t *testing.T, svc *Service, email, phone string) *AuthResponse {
	t.Helper()
	resp, err := svc.Signup(context.Background(), SignupRequest{
		FirstName: "Ada", LastName: "Lovelace", Email: email, Phone: phone, Password: "password123",
	})
	require.NoError(t, err)
	return resp
}

func TestService_UpdateProfile_EachField(t *testing.T) {
	svc, db := newAuthTestService(t)
	user := signup(t, svc, "ada@example.com", "111").User
	ctx := context.Background()

	cases := []struct {
		name  string
		req   UpdateProfileRequest
		check func(t *testing.T, p *Profile)
	}{
		{"first name is trimmed", UpdateProfileRequest{FirstName: ptr("  Augusta ")}, func(t *testing.T, p *Profile) {
			assert.Equal(t, "Augusta", p.FirstName)
			assert.Equal(t, "Lovelace", p.LastName, "other fields are kept")
		}},
		{"last name", UpdateProfileRequest{LastName: ptr("King")}, func(t *testing.T, p *Profile) {
			assert.Equal(t, "King", p.LastName)
			assert.Equal(t, "Augusta", p.FirstName)
		}},
		{"email is trimmed and lowercased", UpdateProfileRequest{Email: ptr(" Ada.King@Example.COM ")}, func(t *testing.T, p *Profile) {
			assert.Equal(t, "ada.king@example.com", p.Email)
		}},
		{"phone", UpdateProfileRequest{Phone: ptr(" +4512345678 ")}, func(t *testing.T, p *Profile) {
			assert.Equal(t, "+4512345678", p.Phone)
		}},
		{"phone can be cleared", UpdateProfileRequest{Phone: ptr("")}, func(t *testing.T, p *Profile) {
			assert.Empty(t, p.Phone)
		}},
		{"empty request changes nothing", UpdateProfileRequest{}, func(t *testing.T, p *Profile) {
			assert.Equal(t, "Augusta", p.FirstName)
			assert.Equal(t, "ada.king@example.com", p.Email)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := svc.UpdateProfile(ctx, user.ID, tc.req)
			require.NoError(t, err)
			tc.check(t, p)

			stored, err := svc.GetProfile(ctx, user.ID)
			require.NoError(t, err)
			assert.Equal(t, p, stored, "the update is persisted")
		})
	}

	// The account still signs in with the new email.
	_, err := svc.Signin(ctx, SigninRequest{Email: "ada.king@example.com", Password: "password123"})
	require.NoError(t, err)

	var count int64
	require.NoError(t, db.Model(&models.User{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestService_UpdateProfile_Conflicts(t *testing.T) {
	svc, _ := newAuthTestService(t)
	ctx := context.Background()
	me := signup(t, svc, "me@example.com", "111").User
	signup(t, svc, "other@example.com", "222")

	_, err := svc.UpdateProfile(ctx, me.ID, UpdateProfileRequest{Email: ptr("OTHER@example.com")})
	require.ErrorIs(t, err, ErrEmailAlreadyUsed)

	_, err = svc.UpdateProfile(ctx, me.ID, UpdateProfileRequest{Phone: ptr("222")})
	require.ErrorIs(t, err, ErrPhoneAlreadyUsed)

	// A rejected update changes nothing, even fields that were valid.
	_, err = svc.UpdateProfile(ctx, me.ID, UpdateProfileRequest{FirstName: ptr("New"), Email: ptr("other@example.com")})
	require.ErrorIs(t, err, ErrEmailAlreadyUsed)
	p, err := svc.GetProfile(ctx, me.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ada", p.FirstName)

	// Keeping your own email and phone is not a conflict.
	p, err = svc.UpdateProfile(ctx, me.ID, UpdateProfileRequest{Email: ptr("me@example.com"), Phone: ptr("111")})
	require.NoError(t, err)
	assert.Equal(t, "me@example.com", p.Email)
}

func TestService_UpdateProfile_UnknownUser(t *testing.T) {
	svc, _ := newAuthTestService(t)
	_, err := svc.UpdateProfile(context.Background(), ulid.Make(), UpdateProfileRequest{FirstName: ptr("x")})
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestService_ChangePassword(t *testing.T) {
	svc, db := newAuthTestService(t)
	ctx := context.Background()
	user := signup(t, svc, "ada@example.com", "").User

	// A session from before the change, issued a little earlier so it is not
	// in the same second as the change.
	oldIssued := time.Now().Add(-2 * time.Second).Truncate(time.Second)
	oldToken, err := svc.generateTokenAt(user.ID, oldIssued)
	require.NoError(t, err)
	require.NoError(t, svc.ValidateSession(ctx, user.ID, oldIssued), "valid before the change")

	_, err = svc.ChangePassword(ctx, user.ID, "wrong-password", "new-password-1")
	require.ErrorIs(t, err, ErrIncorrectPassword)

	resp, err := svc.ChangePassword(ctx, user.ID, "password123", "new-password-1")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Token)
	assert.NotEqual(t, oldToken, resp.Token)
	assert.Equal(t, user.ID, resp.User.ID)

	var stored models.User
	require.NoError(t, db.Take(&stored, "id = ?", user.ID).Error)
	require.NotNil(t, stored.PasswordChangedAt)
	assert.NotEqual(t, "new-password-1", stored.Password, "only the hash is stored")

	// The old session is rejected, the fresh one is accepted.
	require.ErrorIs(t, svc.ValidateSession(ctx, user.ID, oldIssued), ErrSessionRevoked)
	freshID, err := svc.parseTokenUserID(resp.Token)
	require.NoError(t, err)
	assert.Equal(t, user.ID, freshID)
	require.NoError(t, svc.ValidateSession(ctx, user.ID, *stored.PasswordChangedAt))
	require.NoError(t, svc.ValidateSession(ctx, user.ID, time.Now()))

	// Only the new password signs in.
	_, err = svc.Signin(ctx, SigninRequest{Email: "ada@example.com", Password: "password123"})
	require.ErrorIs(t, err, ErrInvalidCredentials)
	_, err = svc.Signin(ctx, SigninRequest{Email: "ada@example.com", Password: "new-password-1"})
	require.NoError(t, err)

	_, err = svc.ChangePassword(ctx, ulid.Make(), "x", "new-password-1")
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestService_VerifyPassword(t *testing.T) {
	svc, _ := newAuthTestService(t)
	ctx := context.Background()
	user := signup(t, svc, "ada@example.com", "").User

	require.NoError(t, svc.VerifyPassword(ctx, user.ID, "password123"))
	require.ErrorIs(t, svc.VerifyPassword(ctx, user.ID, "nope"), ErrIncorrectPassword)
	require.ErrorIs(t, svc.VerifyPassword(ctx, ulid.Make(), "password123"), ErrUserNotFound)
}

func TestService_ValidateSession(t *testing.T) {
	svc, db := newAuthTestService(t)
	ctx := context.Background()
	user := signup(t, svc, "ada@example.com", "").User

	require.NoError(t, svc.ValidateSession(ctx, user.ID, time.Time{}), "never changed: any token is fine")
	require.ErrorIs(t, svc.ValidateSession(ctx, ulid.Make(), time.Now()), ErrUserNotFound)

	changed := time.Date(2026, 5, 1, 12, 0, 0, 500_000_000, time.UTC)
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", user.ID).Update("password_changed_at", changed).Error)

	require.ErrorIs(t, svc.ValidateSession(ctx, user.ID, changed.Add(-time.Second)), ErrSessionRevoked)
	require.ErrorIs(t, svc.ValidateSession(ctx, user.ID, time.Time{}), ErrSessionRevoked, "no iat after a change")
	// JWT times are whole seconds: a token from the change's second is accepted.
	require.NoError(t, svc.ValidateSession(ctx, user.ID, changed.Truncate(time.Second)))
	require.NoError(t, svc.ValidateSession(ctx, user.ID, changed.Add(time.Second)))
}
