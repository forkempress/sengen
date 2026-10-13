package auth

import (
	"sync"
	"time"
)

// LoginLimiter is a simple in-memory sliding-window limiter for login
// attempts, keyed by "ip|email". In-memory is sufficient for a single-binary,
// single-process deploy on one VPS; state resets on restart, which is fine
// for a login throttle.
type LoginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		attempts: make(map[string][]time.Time),
		max:      5,
		window:   15 * time.Minute,
	}
}

// Allow reports whether another attempt for key is permitted right now,
// without recording one.
func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	return len(l.attempts[key]) < l.max
}

// RecordFailure records a failed attempt for key.
func (l *LoginLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	l.attempts[key] = append(l.attempts[key], time.Now())
}

// Reset clears the attempt history for key, called after a successful login.
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *LoginLimiter) prune(key string) {
	cutoff := time.Now().Add(-l.window)
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
