package users

import (
	"context"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAuthTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	return NewWithAuth(db, nil, nil, "test-secret", time.Hour), db
}

func TestService_SignupSigninProfile(t *testing.T) {
	svc, db := newAuthTestService(t)

	signupResp, err := svc.Signup(context.Background(), SignupRequest{
		FirstName: "Ada",
		LastName:  "Lovelace",
		Email:     "ada@example.com",
		Phone:     "1234",
		Password:  "password123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, signupResp.Token)
	require.Equal(t, "ada@example.com", signupResp.User.Email)

	var dbUser models.User
	err = db.Where("email = ?", "ada@example.com").First(&dbUser).Error
	require.NoError(t, err)
	require.NotEqual(t, "password123", dbUser.Password)

	signinResp, err := svc.Signin(context.Background(), SigninRequest{
		Email:    "ada@example.com",
		Password: "password123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, signinResp.Token)

	profileResp, err := svc.Profile(context.Background(), ProfileRequest{Token: signinResp.Token})
	require.NoError(t, err)
	require.Equal(t, dbUser.ID, profileResp.ID)
	require.Equal(t, "ada@example.com", profileResp.Email)
}

func TestService_Signin_InvalidCredentials(t *testing.T) {
	svc, _ := newAuthTestService(t)

	_, err := svc.Signup(context.Background(), SignupRequest{
		Email:    "auth@example.com",
		Password: "correct-password",
	})
	require.NoError(t, err)

	_, err = svc.Signin(context.Background(), SigninRequest{
		Email:    "auth@example.com",
		Password: "wrong-password",
	})
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestService_Profile_InvalidToken(t *testing.T) {
	svc, _ := newAuthTestService(t)

	_, err := svc.Profile(context.Background(), ProfileRequest{Token: "not-a-token"})
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestService_Signup_DuplicateEmail(t *testing.T) {
	svc, _ := newAuthTestService(t)

	_, err := svc.Signup(context.Background(), SignupRequest{
		FirstName: "First",
		LastName:  "User",
		Email:     "dup@example.com",
		Phone:     "111111",
		Password:  "password123",
	})
	require.NoError(t, err)

	_, err = svc.Signup(context.Background(), SignupRequest{
		FirstName: "Second",
		LastName:  "User",
		Email:     "dup@example.com",
		Phone:     "222222",
		Password:  "password123",
	})
	require.ErrorIs(t, err, ErrEmailAlreadyUsed)
}

func TestService_Signup_DuplicatePhone(t *testing.T) {
	svc, _ := newAuthTestService(t)

	_, err := svc.Signup(context.Background(), SignupRequest{
		FirstName: "First",
		LastName:  "User",
		Email:     "first@example.com",
		Phone:     "555555",
		Password:  "password123",
	})
	require.NoError(t, err)

	_, err = svc.Signup(context.Background(), SignupRequest{
		FirstName: "Second",
		LastName:  "User",
		Email:     "second@example.com",
		Phone:     "555555",
		Password:  "password123",
	})
	require.ErrorIs(t, err, ErrPhoneAlreadyUsed)
}

func TestService_GetProfile(t *testing.T) {
	svc, _ := newAuthTestService(t)

	signupResp, err := svc.Signup(context.Background(), SignupRequest{
		FirstName: "Grace",
		Email:     "grace@example.com",
		Password:  "password123",
	})
	require.NoError(t, err)

	profile, err := svc.GetProfile(context.Background(), signupResp.User.ID)
	require.NoError(t, err)
	require.Equal(t, "grace@example.com", profile.Email)
	require.Equal(t, "Grace", profile.FirstName)

	_, err = svc.GetProfile(context.Background(), ulid.Make())
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestCreateUser_UniqueIndexCatchesARace(t *testing.T) {
	svc, db := newAuthTestService(t)
	ctx := t.Context()

	// Simulate a concurrent sign-up that slipped past the check: insert the
	// row directly, then let the service's insert hit the index.
	require.NoError(t, db.Create(&models.User{Email: "Race@Example.com", Password: "x"}).Error)
	err := db.Create(&models.User{Email: "race@example.com", Password: "x"}).Error
	require.ErrorIs(t, alreadyUsed(err), ErrEmailAlreadyUsed, "emails are unique in any case")

	require.NoError(t, db.Create(&models.User{Email: "a@example.com", Phone: "+4512345678", Password: "x"}).Error)
	err = db.Create(&models.User{Email: "b@example.com", Phone: "+4512345678", Password: "x"}).Error
	require.ErrorIs(t, alreadyUsed(err), ErrPhoneAlreadyUsed)

	require.NoError(t, db.Create(&models.User{Email: "c@example.com", Password: "x"}).Error)
	require.NoError(t, db.Create(&models.User{Email: "d@example.com", Password: "x"}).Error, "empty phones are not unique")

	_, err = svc.Signin(ctx, SigninRequest{Email: "nobody@example.com", Password: "whatever"})
	require.ErrorIs(t, err, ErrInvalidCredentials)
}
