package auth

import (
	"time"
)

func newLimiter() *limiter {
	return &limiter{keys: map[string]*attempts{}}
}

func (l *limiter) locked(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	a := l.keys[key]
	if a == nil || !now.Before(a.lockedUntil) {
		return 0
	}

	return a.lockedUntil.Sub(now)
}

// fail records a failure and reports whether key is now locked out.
func (l *limiter) fail(key string, limit int, window time.Duration, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.prune(now, window)

	a := l.keys[key]
	if a == nil || now.Sub(a.first) > window {
		a = &attempts{first: now}
		l.keys[key] = a
	}

	a.failures++
	if a.failures >= limit {
		a.lockedUntil = now.Add(window)
		a.failures, a.first = 0, now

		return true
	}

	return false
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.keys, key)
}

// prune drops stale entries so the map cannot grow without bound under a spray of usernames. Callers hold l.mu.
func (l *limiter) prune(now time.Time, window time.Duration) {
	for k, a := range l.keys {
		if now.Sub(a.first) > window && !now.Before(a.lockedUntil) {
			delete(l.keys, k)
		}
	}
}
