package kodi

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

var clockPattern = regexp.MustCompile(`^\s*(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([AaPp])?`)

// Clock implements media.Backend by reading Kodi's own clock, so the time is right for the room the TV is in without Encore knowing the time zone.
func (b *Backend) Clock(ctx context.Context) (time.Time, error) {
	labels, err := b.c.XBMCGetInfoLabels(ctx, kodirpc.XBMCGetInfoLabelsParams{Labels: []string{clockLabel}})
	if err != nil {
		return time.Time{}, err
	}

	return parseClock(labels[clockLabel])
}

// An unreadable date leaves only the time of day, and the bool false.
func (b *Backend) localNow(ctx context.Context) (time.Time, bool, error) {
	labels, err := b.c.XBMCGetInfoLabels(ctx, kodirpc.XBMCGetInfoLabelsParams{Labels: []string{clockLabel, dateLabel}})
	if err != nil {
		return time.Time{}, false, err
	}

	tod, err := parseClock(labels[clockLabel])
	if err != nil {
		return time.Time{}, false, err
	}

	day, err := time.Parse(time.DateOnly, labels[dateLabel])
	if err != nil {
		return tod, false, nil
	}

	return time.Date(day.Year(), day.Month(), day.Day(), tod.Hour(), tod.Minute(), tod.Second(), 0, time.UTC), true, nil
}

// parseClock reads "09:42:17 PM", "21:42:17" or "9:42 AM" as a time of day on 1 January of year 1, UTC.
func parseClock(s string) (time.Time, error) {
	m := clockPattern.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, fmt.Errorf("kodi: unreadable clock %q", s)
	}

	h, _ := strconv.Atoi(m[1])
	mins, _ := strconv.Atoi(m[2])
	secs, _ := strconv.Atoi(m[3])

	switch strings.ToLower(m[4]) {
	case "a":
		h %= 12
	case "p":
		h = h%12 + 12
	}

	if h > 23 || mins > 59 || secs > 59 {
		return time.Time{}, fmt.Errorf("kodi: unreadable clock %q", s)
	}

	return time.Date(1, time.January, 1, h, mins, secs, 0, time.UTC), nil
}

// System implements media.Backend.
func (b *Backend) System(ctx context.Context, cmd media.SystemCommand) error {
	var err error

	switch cmd {
	case media.SystemShutdown:
		_, err = b.c.SystemShutdown(ctx)
	case media.SystemReboot:
		_, err = b.c.SystemReboot(ctx)
	case media.SystemHibernate:
		_, err = b.c.SystemHibernate(ctx)
	case media.SystemSuspend:
		_, err = b.c.SystemSuspend(ctx)
	case media.SystemQuit:
		_, err = b.c.ApplicationQuit(ctx)
	case media.SystemEject:
		_, err = b.c.SystemEjectOpticalDrive(ctx)
	default:
		return fmt.Errorf("kodi: unknown system command %q", cmd)
	}

	return err
}

// ScanLibrary implements media.Backend.
func (b *Backend) ScanLibrary(ctx context.Context, db media.Database) error {
	var err error

	switch db {
	case media.DatabaseVideo:
		_, err = b.c.VideoLibraryScan(ctx, kodirpc.VideoLibraryScanParams{})
	case media.DatabaseMusic:
		_, err = b.c.AudioLibraryScan(ctx, kodirpc.AudioLibraryScanParams{})
	default:
		return fmt.Errorf("kodi: unknown database %q", db)
	}

	return err
}

// CleanLibrary implements media.Backend.
func (b *Backend) CleanLibrary(ctx context.Context, db media.Database) error {
	var err error

	switch db {
	case media.DatabaseVideo:
		_, err = b.c.VideoLibraryClean(ctx, kodirpc.VideoLibraryCleanParams{})
	case media.DatabaseMusic:
		_, err = b.c.AudioLibraryClean(ctx, kodirpc.AudioLibraryCleanParams{})
	default:
		return fmt.Errorf("kodi: unknown database %q", db)
	}

	return err
}
