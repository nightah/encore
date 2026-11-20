package kodi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUTCOffset(t *testing.T) {
	tod := func(h, m, s int) time.Time { return time.Date(1, 1, 1, h, m, s, 0, time.UTC) }
	at := func(h, m int) time.Time { return time.Date(2026, 10, 26, h, m, 0, 0, time.UTC) }

	testCases := []struct {
		name     string
		have     time.Time
		now      time.Time
		date     bool
		expected time.Duration
	}{
		{name: "ShouldReadMelbourneInDaylightSaving", have: tod(15, 30, 2), now: at(4, 30), expected: 11 * time.Hour},
		{name: "ShouldReadAcrossMidnightBackwards", have: tod(21, 30, 0), now: at(4, 30), expected: -7 * time.Hour},
		{name: "ShouldReadUTCMinus11FromTimeOfDayAlone", have: tod(9, 0, 0), now: at(20, 0), expected: -11 * time.Hour},
		{name: "ShouldTellTongaADayAheadFromTheDate", have: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), now: at(20, 0), date: true, expected: 13 * time.Hour},
		{name: "ShouldTellNiueOnTheSameDayFromTheDate", have: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC), now: at(20, 0), date: true, expected: -11 * time.Hour},
		{name: "ShouldReadIndia", have: tod(10, 0, 4), now: at(4, 30), expected: 5*time.Hour + 30*time.Minute},
		{name: "ShouldReadNepalWithASlowReply", have: tod(10, 14, 50), now: at(4, 30), expected: 5*time.Hour + 45*time.Minute},
		{name: "ShouldReadUTC", have: tod(4, 29, 58), now: at(4, 30)},
		{name: "ShouldReadTongaOnTheSameUTCDay", have: tod(17, 30, 0), now: at(4, 30), expected: 13 * time.Hour},
		{name: "ShouldReadKiribatiAsTheFurthestAhead", have: tod(18, 0, 0), now: at(4, 0), expected: 14 * time.Hour},
		{name: "ShouldReadBakerIslandAsTheFurthestBehind", have: tod(8, 0, 0), now: at(20, 0), expected: -12 * time.Hour},
		{name: "ShouldReadThirteenHoursBehindAsElevenAhead", have: tod(10, 0, 0), now: at(23, 0), expected: 11 * time.Hour},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			offset := utcOffset
			if tc.date {
				offset = dateOffset
			}

			assert.Equal(t, tc.expected, offset(tc.have, tc.now))
		})
	}
}

func TestZoneRelearnsWhenDue(t *testing.T) {
	now := time.Date(2026, 10, 26, 4, 30, 0, 0, time.UTC)
	z := newZone(nil)
	z.now = func() time.Time { return now }

	local := time.Date(2026, 10, 26, 15, 30, 0, 0, time.UTC)
	calls := 0
	clock := func(context.Context) (time.Time, bool, error) {
		calls++

		return local, true, nil
	}

	offset := func() int {
		_, off := time.Date(2026, 1, 1, 0, 0, 0, 0, z.location()).Zone()

		return off
	}

	z.refresh(t.Context(), clock)
	require.Equal(t, 11*3600, offset())

	// Melbourne leaves daylight saving.
	now = now.Add(zoneRelearn - time.Second)
	local = now.Add(10 * time.Hour)
	z.refresh(t.Context(), clock)
	assert.Equal(t, 1, calls, "not due yet")
	assert.Equal(t, 11*3600, offset())

	now = now.Add(time.Second)
	local = now.Add(10 * time.Hour)
	z.refresh(t.Context(), clock)
	assert.Equal(t, 2, calls)
	assert.Equal(t, 10*3600, offset(), "the new offset is learned")
}

func TestZoneRefresh(t *testing.T) {
	now := time.Date(2026, 10, 26, 4, 30, 0, 0, time.UTC)
	z := newZone(nil)
	z.now = func() time.Time { return now }

	calls := 0
	clock := func(context.Context) (time.Time, bool, error) {
		calls++

		return time.Date(2026, 10, 26, 15, 30, 0, 0, time.UTC), true, nil
	}

	z.refresh(t.Context(), clock)
	_, off := time.Date(2026, 1, 1, 0, 0, 0, 0, z.location()).Zone()
	assert.Equal(t, 11*3600, off)

	z.refresh(t.Context(), clock)
	assert.Equal(t, 1, calls, "learned once until it is due again")

	now = now.Add(zoneRelearn)
	z.refresh(t.Context(), func(context.Context) (time.Time, bool, error) { return time.Time{}, false, errors.New("asleep") })
	_, off = time.Date(2026, 1, 1, 0, 0, 0, 0, z.location()).Zone()
	assert.Equal(t, 11*3600, off, "a failed check keeps the last offset")

	fixed := newZone(time.UTC)
	fixed.refresh(t.Context(), clock)
	assert.Equal(t, time.UTC, fixed.location(), "a configured zone is never replaced")
}
