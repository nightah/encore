package kodi

import (
	"context"
	"time"
)

func newZone(configured *time.Location) *zone {
	if configured != nil {
		return &zone{loc: configured, fixed: true, now: time.Now}
	}

	// Until Kodi has been asked, the server's own zone is the best guess.
	return &zone{loc: time.Local, now: time.Now}
}

func (z *zone) location() *time.Location {
	z.mu.Lock()
	defer z.mu.Unlock()

	return z.loc
}

// Learns the offset unless it was learned recently. clock returns the host's local time; with dateKnown false only its time of day counts. A failure keeps the current guess; the next library read tries again.
func (z *zone) refresh(ctx context.Context, clock func(context.Context) (local time.Time, dateKnown bool, err error)) {
	z.mu.Lock()
	due := !z.fixed && (z.learned.IsZero() || z.now().Sub(z.learned) >= zoneRelearn)
	z.mu.Unlock()

	if !due {
		return
	}

	local, dateKnown, err := clock(ctx)
	if err != nil {
		return
	}

	now := z.now()

	offset := utcOffset(local, now)
	if dateKnown {
		offset = dateOffset(local, now)
	}

	loc := time.FixedZone("Kodi", int(offset.Seconds()))

	z.mu.Lock()
	z.loc, z.learned = loc, now
	z.mu.Unlock()
}

// From the host's local date and time read at now, rounded to 15 minutes.
func dateOffset(local, now time.Time) time.Duration {
	wall := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, time.UTC)

	return wall.Sub(now.UTC()).Round(15 * time.Minute)
}

// From the host's local time of day alone, read at now, for a Kodi that doesn't report its date. The difference is kept within UTC−12 to UTC+14 (so UTC+13 and UTC−11 can't be told apart; the date settles that), and rounded to 15 minutes, the finest step any zone uses, which also absorbs the time the request took.
func utcOffset(localTimeOfDay, now time.Time) time.Duration {
	u := now.UTC()
	utcTOD := time.Duration(u.Hour())*time.Hour + time.Duration(u.Minute())*time.Minute + time.Duration(u.Second())*time.Second
	kodiTOD := time.Duration(localTimeOfDay.Hour())*time.Hour + time.Duration(localTimeOfDay.Minute())*time.Minute + time.Duration(localTimeOfDay.Second())*time.Second

	d := kodiTOD - utcTOD

	switch {
	case d > 14*time.Hour:
		d -= 24 * time.Hour
	case d < -12*time.Hour:
		d += 24 * time.Hour
	}

	return d.Round(15 * time.Minute)
}
