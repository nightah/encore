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

	assert.Equal(t, 11*time.Hour, utcOffset(tod(15, 30, 2), at(4, 30)), "Melbourne in daylight saving")
	assert.Equal(t, -7*time.Hour, utcOffset(tod(21, 30, 0), at(4, 30)), "across midnight backwards")
	assert.Equal(t, -11*time.Hour, utcOffset(tod(9, 0, 0), at(20, 0)), "the time of day alone can't tell UTC−11 from UTC+13")
	assert.Equal(t, 13*time.Hour, dateOffset(time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), at(20, 0)), "the date can: Tonga is a day ahead")
	assert.Equal(t, -11*time.Hour, dateOffset(time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC), at(20, 0)), "Niue is the same day")
	assert.Equal(t, 5*time.Hour+30*time.Minute, utcOffset(tod(10, 0, 4), at(4, 30)), "India")
	assert.Equal(t, 5*time.Hour+45*time.Minute, utcOffset(tod(10, 14, 50), at(4, 30)), "Nepal, with a slow reply")
	assert.Zero(t, utcOffset(tod(4, 29, 58), at(4, 30)))
	assert.Equal(t, 13*time.Hour, utcOffset(tod(17, 30, 0), at(4, 30)), "Tonga, on the same UTC day")
	assert.Equal(t, 14*time.Hour, utcOffset(tod(18, 0, 0), at(4, 0)), "Kiribati is the furthest ahead")
	assert.Equal(t, -12*time.Hour, utcOffset(tod(8, 0, 0), at(20, 0)), "Baker Island is the furthest behind")
	assert.Equal(t, 11*time.Hour, utcOffset(tod(10, 0, 0), at(23, 0)), "thirteen hours behind is read as eleven ahead")
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
