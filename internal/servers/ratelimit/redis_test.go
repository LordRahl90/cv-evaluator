package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// runValkey starts the Valkey image the cluster runs and returns a client.
func runValkey(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "valkey/valkey:8-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	addr, err := ctr.PortEndpoint(ctx, "6379/tcp", "")
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestShared_LimitsAcrossLimiters(t *testing.T) {
	client := runValkey(t)
	store := NewRedisStore(client)

	// Two limiters with one name stand for two API replicas.
	replicaA := NewShared(store, "signin", 2, time.Minute)
	replicaB := NewShared(store, "signin", 2, time.Minute)

	ok, _ := replicaA.AllowContext(t.Context(), "ada@example.com")
	assert.True(t, ok)
	ok, _ = replicaB.AllowContext(t.Context(), "ada@example.com")
	assert.True(t, ok)

	ok, retry := replicaA.AllowContext(t.Context(), "ada@example.com")
	assert.False(t, ok, "the third attempt is over the limit on either replica")
	assert.Greater(t, retry, 50*time.Second)
	assert.LessOrEqual(t, retry, time.Minute)

	ok, _ = replicaB.AllowContext(t.Context(), "grace@example.com")
	assert.True(t, ok, "keys are counted separately")

	other := NewShared(store, "signup", 1, time.Minute)
	ok, _ = other.AllowContext(t.Context(), "ada@example.com")
	assert.True(t, ok, "names are counted separately")
}

func TestShared_WindowExpires(t *testing.T) {
	client := runValkey(t)
	l := NewShared(NewRedisStore(client), "short", 1, 200*time.Millisecond)

	ok, _ := l.AllowContext(t.Context(), "k")
	require.True(t, ok)
	ok, _ = l.AllowContext(t.Context(), "k")
	require.False(t, ok)

	assert.Eventually(t, func() bool {
		ok, _ := l.AllowContext(t.Context(), "k")
		return ok
	}, 2*time.Second, 50*time.Millisecond)
}

type failingStore struct{}

func (failingStore) Incr(context.Context, string, time.Duration) (int64, time.Duration, error) {
	return 0, 0, errors.New("connection refused")
}

func TestShared_AllowsWhenTheStoreFails(t *testing.T) {
	l := NewShared(failingStore{}, "signin", 1, time.Minute)
	for range 3 {
		ok, _ := l.AllowContext(t.Context(), "k")
		assert.True(t, ok)
	}
}
