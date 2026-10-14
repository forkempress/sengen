package auth

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// SEC-05: the attempt is recorded in the same step as the check, so parallel
// requests cannot all pass a check none of them has recorded yet.
func TestLoginLimiter_TakeIsAtomic(t *testing.T) {
	l := NewLoginLimiter(5, time.Minute)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Take("k") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 5 {
		t.Errorf("%d of 50 parallel attempts allowed, want exactly 5", got)
	}
}

func TestLoginLimiter_WindowSlides(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	l := NewLoginLimiter(2, 15*time.Minute)
	l.now = func() time.Time { return now }
	l.Take("k")
	l.Take("k")
	if l.Take("k") {
		t.Fatal("third attempt inside the window must be refused")
	}
	now = now.Add(16 * time.Minute)
	if !l.Take("k") {
		t.Error("attempts older than the window must not count")
	}
}

// SEC-05: a new key per request must not grow the map without bound.
func TestLoginLimiter_MapIsCapped(t *testing.T) {
	l := NewLoginLimiter(5, time.Hour)
	l.maxKeys = 100
	for i := 0; i < 1000; i++ {
		l.Take(fmt.Sprintf("spray-%d@example.com", i))
	}
	if n := l.Len(); n > 100 {
		t.Errorf("limiter tracks %d keys, want at most the cap of 100", n)
	}
}

// Keys that never come back are swept once their attempts leave the window.
func TestLoginLimiter_SweepsStaleKeys(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	l := NewLoginLimiter(5, 15*time.Minute)
	l.now = func() time.Time { return now }
	for i := 0; i < 50; i++ {
		l.Take(fmt.Sprintf("k%d", i))
	}
	now = now.Add(20 * time.Minute)
	l.Take("other")
	if n := l.Len(); n != 1 {
		t.Errorf("after the window passed limiter still tracks %d keys, want 1", n)
	}
}
