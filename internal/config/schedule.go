package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// ParseSchedule parses a backup schedule. Accepted forms, case-insensitive, with times in the server's local zone:
//
//	off (or empty)      never
//	hourly              at the top of every hour
//	daily@03:00         every day at 03:00
//	weekly@sun@03:00    every Sunday at 03:00 (sun, mon, tue, wed, thu, fri, sat)
//	every@6h            every interval (a Go duration, at least 1m), counted from the previous run
func ParseSchedule(s string) (Schedule, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	switch s {
	case "", "off":
		return Schedule{}, nil
	case "hourly":
		return Schedule{kind: scheduleHourly}, nil
	}

	kind, rest, ok := strings.Cut(s, "@")
	if !ok {
		return Schedule{}, fmt.Errorf("%q must be off, hourly, daily@HH:MM, weekly@DAY@HH:MM or every@DURATION", s)
	}

	switch kind {
	case "daily":
		h, m, err := parseClock(rest)
		if err != nil {
			return Schedule{}, err
		}

		return Schedule{kind: scheduleDaily, hour: h, minute: m}, nil
	case "weekly":
		day, clock, _ := strings.Cut(rest, "@")

		wd, ok := weekdays[day]
		if !ok {
			return Schedule{}, fmt.Errorf("%q is not a weekday (sun, mon, tue, wed, thu, fri, sat)", day)
		}

		h, m, err := parseClock(clock)
		if err != nil {
			return Schedule{}, err
		}

		return Schedule{kind: scheduleWeekly, weekday: wd, hour: h, minute: m}, nil
	case "every":
		d, err := time.ParseDuration(rest)
		if err != nil {
			return Schedule{}, fmt.Errorf("%q is not a duration", rest)
		}

		if d < minEvery {
			return Schedule{}, fmt.Errorf("interval %s must be at least %s", d, minEvery)
		}

		return Schedule{kind: scheduleEvery, every: d}, nil
	default:
		return Schedule{}, fmt.Errorf("unknown schedule kind %q", kind)
	}
}

func parseClock(s string) (hour, minute int, err error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, errors.New("time must be HH:MM (24-hour)")
	}

	return t.Hour(), t.Minute(), nil
}

// Enabled reports whether the schedule ever fires.
func (s Schedule) Enabled() bool { return s.kind != scheduleOff }

// Due returns when the next run is due, given when the last one ran (zero if never). An every@ interval counts from the last run, so it carries across restarts and comes due straight away when overdue; the other kinds follow the clock.
func (s Schedule) Due(last, now time.Time) time.Time {
	if s.kind != scheduleEvery {
		return s.Next(now)
	}

	if last.IsZero() {
		return now.Add(s.every)
	}

	if due := last.Add(s.every); due.After(now) {
		return due
	}

	return now
}

// Next returns the first run strictly after t, in t's location. It returns the zero time when the schedule is disabled.
func (s Schedule) Next(t time.Time) time.Time {
	switch s.kind {
	case scheduleHourly:
		// Not t.Truncate: that rounds in absolute time, which is off by the half hour in zones such as Asia/Kolkata.
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()).Add(time.Hour)
	case scheduleDaily:
		next := time.Date(t.Year(), t.Month(), t.Day(), s.hour, s.minute, 0, 0, t.Location())
		if !next.After(t) {
			next = next.AddDate(0, 0, 1)
		}

		return next
	case scheduleWeekly:
		days := (int(s.weekday) - int(t.Weekday()) + 7) % 7

		next := time.Date(t.Year(), t.Month(), t.Day()+days, s.hour, s.minute, 0, 0, t.Location())
		if !next.After(t) {
			next = next.AddDate(0, 0, 7)
		}

		return next
	case scheduleEvery:
		return t.Add(s.every)
	default:
		return time.Time{}
	}
}
