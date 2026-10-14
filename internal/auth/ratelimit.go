package auth

import (
	"sync"
	"time"
)

// LoginLimiter is an in-memory sliding-window limiter for login attempts.
// In-memory is sufficient for a single-binary, single-process deploy on one
// VPS; state resets on restart, which is fine for a login throttle.
//
// The login handler keeps two of them: one keyed by client IP (stops one
// address spraying a password across many accounts) and one keyed by
// account (stops many addresses guessing one account's password). A combined
// "ip|email" key would stop neither.
//
// Take records the attempt and checks the limit under one lock, before the
// password is checked, so parallel requests cannot all slip past a check that
// has not been recorded yet.
type LoginLimiter struct {
	mu        sync.Mutex
	attempts  map[string][]time.Time
	max       int
	window    time.Duration
	maxKeys   int
	lastSweep time.Time
	now       func() time.Time
}

// DefaultMaxKeys bounds the limiter's memory: an attacker sending a new
// email (or address) on every request cannot grow the map past it.
const DefaultMaxKeys = 10000

// NewLoginLimiter allows max attempts per key within window.
func NewLoginLimiter(max int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{
		attempts: make(map[string][]time.Time),
		max:      max,
		window:   window,
		maxKeys:  DefaultMaxKeys,
		now:      time.Now,
	}
}

// Take records an attempt for key and reports whether it is within the
// limit. A refused attempt is not recorded, so a blocked key unblocks once
// its oldest attempts leave the window.
func (l *LoginLimiter) Take(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if now.Sub(l.lastSweep) >= l.window {
		l.sweep(now)
	}
	l.prune(key, now)
	if len(l.attempts[key]) >= l.max {
		return false
	}
	if _, ok := l.attempts[key]; !ok && len(l.attempts) >= l.maxKeys {
		l.sweep(now)
		// Still full of live keys: drop one to make room. Map iteration
		// order is random, so the attacker cannot pick whose history goes.
		for k := range l.attempts {
			if len(l.attempts) < l.maxKeys {
				break
			}
			delete(l.attempts, k)
		}
	}
	l.attempts[key] = append(l.attempts[key], now)
	return true
}

// Reset clears the attempt history for key, called after a successful login.
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// Len reports how many keys are tracked (for tests).
func (l *LoginLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.attempts)
}

func (l *LoginLimiter) prune(key string, now time.Time) {
	cutoff := now.Add(-l.window)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
	} else {
		l.attempts[key] = kept
	}
}

// sweep drops every key whose attempts have all left the window, so keys
// that never come back do not stay in memory forever.
func (l *LoginLimiter) sweep(now time.Time) {
	for k := range l.attempts {
		l.prune(k, now)
	}
	l.lastSweep = now
}
