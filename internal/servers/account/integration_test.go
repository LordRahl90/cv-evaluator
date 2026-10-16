package account

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	apikeyhttp "cv-evaluator/internal/servers/apikeys"
	"cv-evaluator/internal/servers/auth"
	"cv-evaluator/internal/servers/middleware"
	accountsvc "cv-evaluator/internal/services/account"
	apikeysvc "cv-evaluator/internal/services/apikeys"
	"cv-evaluator/internal/services/users"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestEndToEnd_AccountLifecycle runs the real services and middleware, wired
// as cmd/api does: edit the profile, change the password (older sessions
// stop working, the fresh one and API keys keep working), export, then
// delete the account (every credential stops working).
func TestEndToEnd_AccountLifecycle(t *testing.T) {
	container := postgres.MustRun(t)
	db, err := container.OpenGorm(&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)

	secret := []byte("e2e-account-secret")
	usersSvc := users.NewWithAuth(db, nil, nil, string(secret), time.Hour)
	keysSvc := apikeysvc.New(db)
	opts := []middleware.Option{
		middleware.WithAPIKeyVerifier(apikeyhttp.NewVerifier(keysSvc)),
		middleware.WithSessionValidator(auth.NewSessionValidator(usersSvc)),
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	auth.New(r, usersSvc, secret, opts...)
	apikeyhttp.New(r, keysSvc, secret, opts...)
	New(r, usersSvc, accountsvc.New(db), secret, opts...)
	f := &fixture{router: r}

	w := f.doAs("", http.MethodPost, "/auth/signup", `{"first_name":"Ada","email":"ada@example.com","password":"password123"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var session auth.SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &session))
	userID := ulid.MustParseStrict(session.User.ID)

	// A session from a little earlier (JWT times are whole seconds, so one
	// from the same second as the password change would still be accepted).
	old, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-5 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(secret)
	require.NoError(t, err)
	oldSession := "Bearer " + old

	w = f.doAs(oldSession, http.MethodPost, "/api-keys", `{"name":"ci"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var key apikeyhttp.KeyResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &key))
	apiKey := "Bearer " + key.Secret

	// Edit the profile.
	w = f.doAs(oldSession, http.MethodPatch, "/me", `{"last_name":"King","email":"Ada.King@Example.com"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var me auth.UserResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &me))
	assert.Equal(t, "King", me.LastName)
	assert.Equal(t, "ada.king@example.com", me.Email)

	// Change the password.
	w = f.doAs(oldSession, http.MethodPost, "/me/password", `{"current_password":"wrong-one","new_password":"new-password"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, CodeIncorrectPassword, decodeError(t, w).Error.Code)

	w = f.doAs(oldSession, http.MethodPost, "/me/password", `{"current_password":"password123","new_password":"new-password"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var fresh auth.SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fresh))
	freshSession := "Bearer " + fresh.Token

	w = f.doAs(oldSession, http.MethodGet, "/me", "")
	require.Equal(t, http.StatusUnauthorized, w.Code, "sessions from before the change are revoked")
	assert.Equal(t, "invalid_token", decodeError(t, w).Error.Code)
	w = f.doAs(freshSession, http.MethodGet, "/me", "")
	require.Equal(t, http.StatusOK, w.Code, "the fresh session keeps the user signed in")
	w = f.doAs(apiKey, http.MethodGet, "/me", "")
	require.Equal(t, http.StatusOK, w.Code, "API keys survive a password change")

	// Export.
	w = f.doAs(freshSession, http.MethodGet, "/me/export", "")
	require.Equal(t, http.StatusOK, w.Code)
	var exp accountsvc.Export
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &exp))
	assert.Equal(t, "ada.king@example.com", exp.Profile.Email)
	require.Len(t, exp.APIKeys, 1)
	assert.NotContains(t, w.Body.String(), key.Secret)

	// Delete, with an API key: the password is still required.
	w = f.doAs(apiKey, http.MethodDelete, "/me", `{"password":"password123"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, "the old password no longer works")
	w = f.doAs(apiKey, http.MethodDelete, "/me", `{"password":"new-password"}`)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	for _, cred := range []string{freshSession, apiKey} {
		w = f.doAs(cred, http.MethodGet, "/me", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "no credential outlives the account")
	}
	w = f.doAs("", http.MethodPost, "/auth/signin", `{"email":"ada.king@example.com","password":"new-password"}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
