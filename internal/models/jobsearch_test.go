package models

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobSearch_NextRun(t *testing.T) {
	from := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		frequency string
		want      time.Time
	}{
		{FrequencyHourly, from.Add(time.Hour)},
		{FrequencyDaily, from.Add(24 * time.Hour)},
		{FrequencyWeekly, from.Add(7 * 24 * time.Hour)},
		{"", from.Add(24 * time.Hour)},
		{"monthly", from.Add(24 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.frequency, func(t *testing.T) {
			js := &JobSearch{Frequency: tt.frequency}
			assert.Equal(t, tt.want, js.NextRun(from))
		})
	}
}

func TestFrequencyInterval(t *testing.T) {
	for _, f := range Frequencies {
		_, ok := FrequencyInterval(f)
		assert.True(t, ok, f)
	}
	_, ok := FrequencyInterval("monthly")
	assert.False(t, ok)
}

func TestJobSearch_BeforeCreate(t *testing.T) {
	js := &JobSearch{}
	require.NoError(t, js.BeforeCreate(nil))
	assert.NotEqual(t, ulid.ULID{}, js.ID)

	id := ulid.Make()
	js = &JobSearch{ID: id}
	require.NoError(t, js.BeforeCreate(nil))
	assert.Equal(t, id, js.ID)
}

func TestPortalSuggestion_BeforeCreate(t *testing.T) {
	ps := &PortalSuggestion{}
	require.NoError(t, ps.BeforeCreate(nil))
	assert.NotEqual(t, ulid.ULID{}, ps.ID)
	assert.Equal(t, PortalSuggestionSubmitted, ps.Status)

	ps = &PortalSuggestion{Status: PortalSuggestionPlanned}
	require.NoError(t, ps.BeforeCreate(nil))
	assert.Equal(t, PortalSuggestionPlanned, ps.Status)
}
