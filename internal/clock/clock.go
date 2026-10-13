// Package clock centralizes the application's notion of "today": always the
// local calendar date in Europe/Belgrade, never UTC or the host's local zone.
// Every part of the app that needs "today" (validation, dashboard, jobs)
// should go through here so the timezone rule lives in exactly one place.
package clock

import "time"

var belgrade = mustLoadLocation("Europe/Belgrade")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("clock: cannot load location " + name + ": " + err.Error())
	}
	return loc
}

// Today returns today's calendar date in Europe/Belgrade, at UTC midnight
// (date-only — matches the convention used by internal/calc).
func Today() time.Time {
	now := time.Now().In(belgrade)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// Now returns the current wall-clock time in Europe/Belgrade (with
// time-of-day) — used by background jobs that need to know the local hour,
// e.g. "send the daily digest at 07:00".
func Now() time.Time {
	return time.Now().In(belgrade)
}
