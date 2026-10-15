package apikeys

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"cv-evaluator/internal/servers/auth"
	apikeysvc "cv-evaluator/internal/services/apikeys"
	"cv-evaluator/internal/services/users"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndToEnd_SignupKeyLifecycle exercises the real services against
// Postgres: sign up, mint a key with the session JWT, call /me with the key,
// revoke it, and confirm the key stops working.
func TestEndToEnd_SignupKeyLifecycle(t *testing.T) {
	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	secret := "e2e-secret"
	usersSvc := users.NewWithAuth(db, nil, nil, secret, time.Hour)
	keysSvc := apikeysvc.New(db)
	verifier := NewVerifier(keysSvc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	auth.New(r, usersSvc, []byte(secret), verifier)
	New(r, keysSvc, []byte(secret), verifier)

	// Sign up.
	w := doRequest(r, http.MethodPost, "/auth/signup", "", `{"email":"e2e@example.com","password":"password123"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var session auth.SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &session))
	jwtHeader := "Bearer " + session.Token

	// Create a key with the session token.
	w = doRequest(r, http.MethodPost, "/api-keys", jwtHeader, `{"name":"laptop"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created KeyResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.Secret)
	keyHeader := "Bearer " + created.Secret

	// The key authenticates as the same user.
	w = doRequest(r, http.MethodGet, "/me", keyHeader, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var me auth.UserResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &me))
	assert.Equal(t, session.User.ID, me.ID)
	assert.Equal(t, "e2e@example.com", me.Email)

	// List shows the key, with last_used_at populated and no secret.
	w = doRequest(r, http.MethodGet, "/api-keys", keyHeader, "")
	require.Equal(t, http.StatusOK, w.Code)
	var list ListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	assert.Equal(t, created.ID, list.Data[0].ID)
	assert.NotNil(t, list.Data[0].LastUsedAt)
	assert.Empty(t, list.Data[0].Secret)

	// Revoke it.
	w = doRequest(r, http.MethodDelete, "/api-keys/"+created.ID, jwtHeader, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// The revoked key is rejected; the JWT still works.
	w = doRequest(r, http.MethodGet, "/me", keyHeader, "")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid_api_key", decodeError(t, w).Error.Code)

	w = doRequest(r, http.MethodGet, "/me", jwtHeader, "")
	require.Equal(t, http.StatusOK, w.Code)
}
