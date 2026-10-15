package apikeys

import (
	"context"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	return New(db), db
}

func createUser(t *testing.T, db *gorm.DB, email string) models.User {
	t.Helper()
	user := models.User{Email: email, Password: "hash"}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func TestService_CreateListDeleteVerify(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	user := createUser(t, db, "keys@example.com")

	created, err := svc.Create(ctx, user.ID, "  CI pipeline  ")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(created.Secret, KeyPrefix))
	assert.Len(t, created.Secret, len(KeyPrefix)+secretLength)
	assert.Equal(t, "CI pipeline", created.Name)
	assert.Equal(t, created.Secret[:displayPrefixLength], created.Prefix)
	assert.Nil(t, created.LastUsedAt)

	// The secret itself is never stored.
	var stored models.APIKey
	require.NoError(t, db.First(&stored, "id = ?", created.ID).Error)
	assert.NotContains(t, stored.KeyHash, created.Secret)
	assert.Equal(t, hashKey(created.Secret), stored.KeyHash)

	keys, err := svc.List(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, created.ID, keys[0].ID)

	owner, err := svc.Verify(ctx, created.Secret)
	require.NoError(t, err)
	assert.Equal(t, user.ID, owner)

	require.NoError(t, db.First(&stored, "id = ?", created.ID).Error)
	require.NotNil(t, stored.LastUsedAt, "verify records last use")

	require.NoError(t, svc.Delete(ctx, user.ID, created.ID))

	_, err = svc.Verify(ctx, created.Secret)
	require.ErrorIs(t, err, ErrInvalidKey, "revoked keys stop working")

	keys, err = svc.List(ctx, user.ID)
	require.NoError(t, err)
	assert.Empty(t, keys)

	// Revocation is a soft delete.
	var count int64
	require.NoError(t, db.Unscoped().Model(&models.APIKey{}).Where("id = ?", created.ID).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestService_Create_Validation(t *testing.T) {
	svc, db := newTestService(t)
	user := createUser(t, db, "validation@example.com")

	_, err := svc.Create(context.Background(), user.ID, "   ")
	require.ErrorIs(t, err, ErrNameRequired)

	_, err = svc.Create(context.Background(), user.ID, strings.Repeat("a", MaxNameLength+1))
	require.ErrorIs(t, err, ErrNameTooLong)

	_, err = svc.Create(context.Background(), user.ID, strings.Repeat("é", MaxNameLength))
	require.NoError(t, err, "length is counted in characters, not bytes")
}

func TestService_Create_UniqueSecrets(t *testing.T) {
	svc, db := newTestService(t)
	user := createUser(t, db, "unique@example.com")

	a, err := svc.Create(context.Background(), user.ID, "a")
	require.NoError(t, err)
	b, err := svc.Create(context.Background(), user.ID, "b")
	require.NoError(t, err)
	assert.NotEqual(t, a.Secret, b.Secret)

	keys, err := svc.List(context.Background(), user.ID)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.Equal(t, b.ID, keys[0].ID, "newest first")
}

func TestService_ScopedToOwner(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	alice := createUser(t, db, "alice@example.com")
	bob := createUser(t, db, "bob@example.com")

	key, err := svc.Create(ctx, alice.ID, "alice key")
	require.NoError(t, err)

	bobKeys, err := svc.List(ctx, bob.ID)
	require.NoError(t, err)
	assert.Empty(t, bobKeys)

	require.ErrorIs(t, svc.Delete(ctx, bob.ID, key.ID), ErrNotFound)

	owner, err := svc.Verify(ctx, key.Secret)
	require.NoError(t, err)
	assert.Equal(t, alice.ID, owner, "bob's delete attempt left the key intact")
}

func TestService_Delete_NotFound(t *testing.T) {
	svc, db := newTestService(t)
	user := createUser(t, db, "missing@example.com")

	require.ErrorIs(t, svc.Delete(context.Background(), user.ID, ulid.Make()), ErrNotFound)
}

func TestService_Delete_Twice(t *testing.T) {
	svc, db := newTestService(t)
	user := createUser(t, db, "twice@example.com")
	key, err := svc.Create(context.Background(), user.ID, "k")
	require.NoError(t, err)

	require.NoError(t, svc.Delete(context.Background(), user.ID, key.ID))
	require.ErrorIs(t, svc.Delete(context.Background(), user.ID, key.ID), ErrNotFound)
}

func TestService_Verify_Rejects(t *testing.T) {
	svc, db := newTestService(t)
	user := createUser(t, db, "rejects@example.com")
	key, err := svc.Create(context.Background(), user.ID, "k")
	require.NoError(t, err)

	cases := map[string]string{
		"empty":         "",
		"no prefix":     strings.TrimPrefix(key.Secret, KeyPrefix),
		"wrong prefix":  "sk_test_" + strings.TrimPrefix(key.Secret, KeyPrefix),
		"truncated":     key.Secret[:len(key.Secret)-1],
		"unknown":       KeyPrefix + strings.Repeat("A", secretLength),
		"one char diff": key.Secret[:len(key.Secret)-1] + flip(key.Secret[len(key.Secret)-1]),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Verify(context.Background(), raw)
			require.ErrorIs(t, err, ErrInvalidKey)
		})
	}
}

func TestService_Verify_DeletedUser(t *testing.T) {
	svc, db := newTestService(t)
	user := createUser(t, db, "gone@example.com")
	key, err := svc.Create(context.Background(), user.ID, "k")
	require.NoError(t, err)

	require.NoError(t, db.Delete(&user).Error)

	_, err = svc.Verify(context.Background(), key.Secret)
	require.ErrorIs(t, err, ErrInvalidKey)
}

func TestService_Verify_ThrottlesLastUsed(t *testing.T) {
	svc, db := newTestService(t)
	user := createUser(t, db, "throttle@example.com")
	key, err := svc.Create(context.Background(), user.ID, "k")
	require.NoError(t, err)

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	current := start
	svc.now = func() time.Time { return current }

	lastUsed := func() time.Time {
		var stored models.APIKey
		require.NoError(t, db.First(&stored, "id = ?", key.ID).Error)
		require.NotNil(t, stored.LastUsedAt)
		return stored.LastUsedAt.UTC()
	}

	_, err = svc.Verify(context.Background(), key.Secret)
	require.NoError(t, err)
	assert.True(t, lastUsed().Equal(start))

	current = start.Add(30 * time.Second)
	_, err = svc.Verify(context.Background(), key.Secret)
	require.NoError(t, err)
	assert.True(t, lastUsed().Equal(start), "not rewritten within the resolution window")

	current = start.Add(2 * time.Minute)
	_, err = svc.Verify(context.Background(), key.Secret)
	require.NoError(t, err)
	assert.True(t, lastUsed().Equal(current))
}

func TestGenerateKey_Alphabet(t *testing.T) {
	for range 50 {
		key, err := generateKey()
		require.NoError(t, err)
		require.Len(t, key, len(KeyPrefix)+secretLength)
		for _, c := range strings.TrimPrefix(key, KeyPrefix) {
			require.True(t, strings.ContainsRune(base62, c), "unexpected char %q", c)
		}
	}
}

func flip(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}
