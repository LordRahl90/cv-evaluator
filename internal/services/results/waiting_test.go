package results

import (
	"context"
	"testing"
	"time"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitingReasons(t *testing.T) {
	svc, db := newTestService(t)
	svc.dailyCap = 2
	ctx := context.Background()
	s := &seeder{t: t, db: db}
	user := ulid.Make()
	js := s.search(user, "Go")

	var scored []ulid.ULID
	for range 2 {
		r := s.result(js, resultOpts{score: ptr(70)})
		require.NoError(t, db.Model(r).Update("evaluated_at", fixedNow.Add(-time.Hour)).Error)
		scored = append(scored, r.ID)
	}
	queued := s.result(js, resultOpts{})
	forced := s.result(js, resultOpts{})
	require.NoError(t, db.Model(forced).Update("forced", true).Error)
	held := s.result(js, resultOpts{evalStatus: models.EvaluationHeld})

	reasons := func() map[ulid.ULID]string {
		page, err := svc.List(ctx, user, ListParams{})
		require.NoError(t, err)
		out := map[ulid.ULID]string{}
		for _, r := range page.Results {
			out[r.ID] = r.WaitingReason
		}
		return out
	}
	assert.Equal(t, map[ulid.ULID]string{
		scored[0]: "", scored[1]: "",
		queued.ID: WaitingDailyCap, forced.ID: WaitingQueued, held.ID: WaitingLowSimilarity,
	}, reasons(), "two evaluations in the last 24 hours reach the cap of 2; the user's own request is not held back")

	d, err := svc.Get(ctx, user, queued.ID)
	require.NoError(t, err)
	assert.Equal(t, WaitingDailyCap, d.WaitingReason)
	d, err = svc.Get(ctx, user, held.ID)
	require.NoError(t, err)
	assert.Equal(t, WaitingLowSimilarity, d.WaitingReason)
	d, err = svc.Get(ctx, user, scored[0])
	require.NoError(t, err)
	assert.Empty(t, d.WaitingReason)

	sum, err := svc.Summary(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, int64(3), sum.Waiting, "pending and held")
	assert.True(t, sum.DailyCapReached)

	// The window moves on.
	require.NoError(t, db.Model(&models.SearchResult{}).Where("id IN ?", scored).
		Update("evaluated_at", fixedNow.Add(-25*time.Hour)).Error)
	assert.Equal(t, WaitingQueued, reasons()[queued.ID])
	sum, err = svc.Summary(ctx, user)
	require.NoError(t, err)
	assert.False(t, sum.DailyCapReached)

	// No cap configured: never daily_cap.
	svc.dailyCap = 0
	require.NoError(t, db.Model(&models.SearchResult{}).Where("id IN ?", scored).
		Update("evaluated_at", fixedNow).Error)
	assert.Equal(t, WaitingQueued, reasons()[queued.ID])

	page, err := svc.List(ctx, user, ListParams{EvaluationStatuses: []string{"held"}})
	require.NoError(t, err, "held is a valid evaluation_status filter")
	require.Len(t, page.Results, 1)
	assert.Equal(t, held.ID, page.Results[0].ID)
}
