package queue_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"cv-evaluator/internal/testutil/postgres"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// All tests in this package share one Postgres container and truncate the
// queue table before each test, so they must not run in parallel.
var testDB *gorm.DB

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()
	container, err := postgres.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		return 1
	}
	defer func() { _ = container.Terminate(context.Background()) }()

	testDB, err = container.OpenGorm(&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "open gorm: %v\n", err)
		return 1
	}
	return m.Run()
}

func resetDB(t *testing.T) *gorm.DB {
	t.Helper()
	require.NoError(t, testDB.Exec("TRUNCATE queue_tasks").Error)
	return testDB
}

// fakeClock is a settable clock for queue.Options.Now.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
