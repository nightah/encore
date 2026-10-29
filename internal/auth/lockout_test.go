package auth

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLimiterLocksAtTheLimit(t *testing.T) {
	l := newLimiter()
	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	assert.Zero(t, l.locked("k", start), "an unknown key is not locked")
	assert.False(t, l.fail("k", 3, time.Minute, start))
	assert.False(t, l.fail("k", 3, time.Minute, start.Add(time.Second)))
	assert.Zero(t, l.locked("k", start.Add(time.Second)), "failures below the limit do not lock")

	assert.True(t, l.fail("k", 3, time.Minute, start.Add(2*time.Second)))
	assert.Equal(t, time.Minute, l.locked("k", start.Add(2*time.Second)))
	assert.Equal(t, time.Second, l.locked("k", start.Add(61*time.Second)))
	assert.Zero(t, l.locked("k", start.Add(62*time.Second)), "the lock ends exactly at the window")

	assert.False(t, l.fail("k", 3, time.Minute, start.Add(62*time.Second)), "counting starts afresh after a lockout")
}

func TestLimiterWindowForgetsOldFailures(t *testing.T) {
	l := newLimiter()
	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	assert.False(t, l.fail("k", 2, time.Minute, start))
	assert.False(t, l.fail("k", 2, time.Minute, start.Add(61*time.Second)), "a failure outside the window starts a new count")
	assert.True(t, l.fail("k", 2, time.Minute, start.Add(62*time.Second)))
}

func TestLimiterWindowIncludesItsLastInstant(t *testing.T) {
	l := newLimiter()
	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	assert.False(t, l.fail("k", 2, time.Minute, start))
	assert.True(t, l.fail("k", 2, time.Minute, start.Add(time.Minute)), "a failure exactly one window after the first still counts towards it")
}

func TestLimiterResetAndIsolation(t *testing.T) {
	l := newLimiter()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	assert.True(t, l.fail("a", 1, time.Minute, now))
	assert.Zero(t, l.locked("b", now), "keys are independent")

	l.reset("a")
	assert.Zero(t, l.locked("a", now))
	l.reset("never-seen")
}

func TestLimiterPrunesStaleKeys(t *testing.T) {
	l := newLimiter()
	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	for i := range 100 {
		l.fail(fmt.Sprintf("user:%d", i), 5, time.Minute, start)
	}

	l.fail("locked", 1, time.Hour, start)

	l.fail("fresh", 5, time.Minute, start.Add(2*time.Minute))

	l.mu.Lock()
	defer l.mu.Unlock()

	assert.Len(t, l.keys, 2, "stale entries are dropped, a live lockout is kept")
	assert.Contains(t, l.keys, "locked")
	assert.Contains(t, l.keys, "fresh")
}

func TestLimiterIsSafeForConcurrentUse(t *testing.T) {
	l := newLimiter()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		locked int
	)

	for range 50 {
		wg.Go(func() {
			if l.fail("k", 10, time.Minute, now) {
				mu.Lock()
				locked++
				mu.Unlock()
			}

			l.locked("k", now)
		})
	}

	wg.Wait()
	assert.Equal(t, 5, locked, "every tenth failure locks, and the count restarts")
}
