// Package health keeps the process-wide signals the superadmin bot reports
// on: when the process started, when the background jobs last ran, and
// which internal errors happened recently. It records kinds and times only —
// never message text, which could carry a driver's data.
package health

import (
	"sync"
	"time"

	"shengen/internal/clock"
)

// Error kinds reported by the rest of the app.
const (
	KindTelegramPanic = "telegram_panic" // a panic while handling an update
	KindJobPanic      = "job_panic"      // a panic during a background tick
	KindJobError      = "job_error"      // a background job failed to read data
	KindSendError     = "send_error"     // a notification could not be sent
)

// Errors older than this are forgotten.
const retention = 48 * time.Hour

// maxEvents caps memory if something fails in a tight loop.
const maxEvents = 1000

type Event struct {
	At   time.Time
	Kind string
}

var (
	mu       sync.Mutex
	started  = time.Now()
	lastTick time.Time
	events   []Event
)

// ReportError records that an error of the given kind just happened.
func ReportError(kind string) {
	mu.Lock()
	defer mu.Unlock()
	now := clock.Now()
	events = append(prune(events, now), Event{At: now, Kind: kind})
	if len(events) > maxEvents {
		events = events[len(events)-maxEvents:]
	}
}

func prune(ev []Event, now time.Time) []Event {
	i := 0
	for i < len(ev) && now.Sub(ev[i].At) > retention {
		i++
	}
	return ev[i:]
}

// ErrorsSince counts errors per kind recorded after t.
func ErrorsSince(t time.Time) map[string]int {
	mu.Lock()
	defer mu.Unlock()
	out := map[string]int{}
	for _, e := range events {
		if e.At.After(t) {
			out[e.Kind]++
		}
	}
	return out
}

// MarkJobTick records that the background jobs just ran.
func MarkJobTick(t time.Time) {
	mu.Lock()
	defer mu.Unlock()
	lastTick = t
}

// LastJobTick is when the background jobs last ran; zero if never.
func LastJobTick() time.Time {
	mu.Lock()
	defer mu.Unlock()
	return lastTick
}

// Started is when the process started.
func Started() time.Time { return started }

// Reset forgets every recorded error and tick. Only for tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	events = nil
	lastTick = time.Time{}
}
