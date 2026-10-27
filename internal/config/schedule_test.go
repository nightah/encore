package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseScheduleNext(t *testing.T) {
	// Monday 26 October 2026, 10:30 UTC.
	now := time.Date(2026, 10, 26, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		in   string
		want time.Time
	}{
		{"hourly", time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC)},
		{"daily@03:00", time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)},
		{"Daily@23:15", time.Date(2026, 10, 26, 23, 15, 0, 0, time.UTC)},
		{"daily@10:30", time.Date(2026, 10, 27, 10, 30, 0, 0, time.UTC)},
		{"weekly@sun@03:00", time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)},
		{"weekly@mon@11:00", time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC)},
		{"weekly@mon@09:00", time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)},
		{"every@6h", now.Add(6 * time.Hour)},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			s, err := ParseSchedule(tt.in)
			require.NoError(t, err)
			assert.True(t, s.Enabled())
			assert.Equal(t, tt.want, s.Next(now))
		})
	}
}

func TestParseScheduleHourlyHalfHourZone(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	s, err := ParseSchedule("hourly")
	require.NoError(t, err)

	assert.Equal(t, time.Date(2026, 1, 1, 11, 0, 0, 0, loc), s.Next(time.Date(2026, 1, 1, 10, 45, 0, 0, loc)))
}

func TestParseScheduleOff(t *testing.T) {
	for _, in := range []string{"", "off", " OFF "} {
		s, err := ParseSchedule(in)
		require.NoError(t, err)
		assert.False(t, s.Enabled())
		assert.True(t, s.Next(time.Now()).IsZero())
	}
}

func TestParseScheduleInvalid(t *testing.T) {
	for _, in := range []string{"daily", "daily@25:00", "daily@3am", "weekly@funday@03:00", "weekly@mon", "every@1s", "every@soon", "monthly@01", "0 3 * * *"} {
		_, err := ParseSchedule(in)
		assert.Error(t, err, in)
	}
}

func TestScheduleDue(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	every, err := ParseSchedule("every@6h")
	require.NoError(t, err)
	assert.Equal(t, now.Add(6*time.Hour), every.Due(time.Time{}, now), "first run an interval from now")
	assert.Equal(t, now.Add(2*time.Hour), every.Due(now.Add(-4*time.Hour), now), "counted from the last run, across restarts")
	assert.Equal(t, now, every.Due(now.Add(-8*time.Hour), now), "overdue runs straight away")

	daily, err := ParseSchedule("daily@03:00")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC), daily.Due(now.Add(-time.Hour), now), "clock schedules ignore the last run")
}
