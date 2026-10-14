// Package clock centralizes the application's notion of "today": always the
// local calendar date in Europe/Belgrade, never UTC or the host's local zone.
// Every part of the app that needs "today" (validation, dashboard, jobs)
// should go through here so the timezone rule lives in exactly one place.
package clock

import (
	"time"
	_ "time/tzdata" // the zones below must load even on hosts without zoneinfo
)

var (
	belgrade = mustLoadLocation("Europe/Belgrade")
	// Sofia stands for the EET zone (BG, RO, GR, FI, the Baltics): the
	// easternmost Schengen time, an hour ahead of Belgrade.
	eet = mustLoadLocation("Europe/Sofia")
)

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("clock: cannot load location " + name + ": " + err.Error())
	}
	return loc
}

var now = time.Now

// Set replaces the source of the current time and returns a function that
// restores the previous one. Only for tests.
func Set(f func() time.Time) (restore func()) {
	prev := now
	now = f
	return func() { now = prev }
}

// Today returns today's calendar date in Europe/Belgrade, at UTC midnight
// (date-only — matches the convention used by internal/calc).
func Today() time.Time {
	return dateIn(now(), belgrade)
}

// LatestToday is today's date in the easternmost Schengen time zone. Between
// 23:00 and midnight Belgrade time it is already tomorrow there, and that is
// the date a border stamp in Bulgaria, Romania or Greece carries — so it is
// the latest date a stay may have without being in the future.
func LatestToday() time.Time {
	return dateIn(now(), eet)
}

// Now returns the current wall-clock time in Europe/Belgrade (with
// time-of-day) — used by background jobs that need to know the local hour,
// e.g. "send the daily digest at 07:00".
func Now() time.Time {
	return now().In(belgrade)
}

// NearMidnight reports whether the Belgrade time is between 23:00 and 01:30,
// when the date of a border crossing depends on which side's clock is read
// (EET borders are an hour ahead) and on how late the driver pressed the
// button.
func NearMidnight() bool {
	t := Now()
	m := t.Hour()*60 + t.Minute()
	return m >= 23*60 || m < 90
}

func dateIn(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
