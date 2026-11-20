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

	testCases := []struct {
		name     string
		have     string
		expected time.Time
	}{
		{name: "ShouldRunHourlyOnTheNextHour", have: "hourly", expected: time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC)},
		{name: "ShouldRunDailyTomorrowWhenTheTimeHasPassed", have: "daily@03:00", expected: time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)},
		{name: "ShouldRunDailyTodayIgnoringCase", have: "Daily@23:15", expected: time.Date(2026, 10, 26, 23, 15, 0, 0, time.UTC)},
		{name: "ShouldRunDailyTomorrowWhenTheTimeIsNow", have: "daily@10:30", expected: time.Date(2026, 10, 27, 10, 30, 0, 0, time.UTC)},
		{name: "ShouldRunWeeklyOnTheNextSunday", have: "weekly@sun@03:00", expected: time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)},
		{name: "ShouldRunWeeklyLaterToday", have: "weekly@mon@11:00", expected: time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC)},
		{name: "ShouldRunWeeklyNextWeekWhenTodaysTimeHasPassed", have: "weekly@mon@09:00", expected: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)},
		{name: "ShouldRunEveryIntervalFromNow", have: "every@6h", expected: now.Add(6 * time.Hour)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseSchedule(tc.have)
			require.NoError(t, err)
			assert.True(t, s.Enabled())
			assert.Equal(t, tc.expected, s.Next(now))
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
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldTurnOffWhenEmpty", have: ""},
		{name: "ShouldTurnOffForOff", have: "off"},
		{name: "ShouldTurnOffForOffInAnyCaseWithSpaces", have: " OFF "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseSchedule(tc.have)
			require.NoError(t, err)
			assert.False(t, s.Enabled())
			assert.True(t, s.Next(time.Now()).IsZero())
		})
	}
}

func TestParseScheduleInvalid(t *testing.T) {
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldRejectDailyWithoutATime", have: "daily"},
		{name: "ShouldRejectAnHourOutOfRange", have: "daily@25:00"},
		{name: "ShouldRejectATwelveHourTime", have: "daily@3am"},
		{name: "ShouldRejectAnUnknownDay", have: "weekly@funday@03:00"},
		{name: "ShouldRejectWeeklyWithoutATime", have: "weekly@mon"},
		{name: "ShouldRejectAnIntervalTooShort", have: "every@1s"},
		{name: "ShouldRejectAnIntervalThatIsNotADuration", have: "every@soon"},
		{name: "ShouldRejectAnUnknownPeriod", have: "monthly@01"},
		{name: "ShouldRejectCronSyntax", have: "0 3 * * *"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSchedule(tc.have)
			assert.Error(t, err)
		})
	}
}

func TestScheduleDue(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	testCases := []struct {
		name     string
		schedule string
		last     time.Time
		expected time.Time
	}{
		{name: "ShouldFirstRunAnIntervalFromNow", schedule: "every@6h", expected: now.Add(6 * time.Hour)},
		{name: "ShouldCountFromTheLastRunAcrossRestarts", schedule: "every@6h", last: now.Add(-4 * time.Hour), expected: now.Add(2 * time.Hour)},
		{name: "ShouldRunOverdueRunsStraightAway", schedule: "every@6h", last: now.Add(-8 * time.Hour), expected: now},
		{name: "ShouldIgnoreTheLastRunForClockSchedules", schedule: "daily@03:00", last: now.Add(-time.Hour), expected: time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseSchedule(tc.schedule)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, s.Due(tc.last, now))
		})
	}
}
