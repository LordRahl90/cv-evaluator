package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMustRun(t *testing.T) {
	ctx := context.Background()
	container := MustRun(t)

	sqlDB, err := container.OpenSQL(ctx)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	var extension string
	err = sqlDB.QueryRowContext(ctx, "SELECT extname FROM pg_extension WHERE extname = 'vector'").Scan(&extension)
	require.NoError(t, err)
	require.Equal(t, "vector", extension)

	gormDB, err := container.OpenGorm(nil)
	require.NoError(t, err)

	var one int
	require.NoError(t, gormDB.WithContext(ctx).Raw("SELECT 1").Scan(&one).Error)
	require.Equal(t, 1, one)
	require.Contains(t, container.ConnectionString(), "sslmode=disable")
}
