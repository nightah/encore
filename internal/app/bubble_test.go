package app

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// present is where a bubble's clock starts, so the fakes' dates are in the past as they would be.
var present = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

// inBubble runs f in a synctest bubble whose clock reads present.
func inBubble(t *testing.T, f func(t *testing.T)) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		time.Sleep(time.Until(present))
		f(t)
	})
}

// settled waits until everything in the bubble is blocked, then requires cond.
func settled(t *testing.T, cond func() bool, msgAndArgs ...any) {
	t.Helper()
	synctest.Wait()
	require.True(t, cond(), msgAndArgs...)
}

// after moves the bubble's clock on by d, then requires cond once everything has settled.
func after(t *testing.T, d time.Duration, cond func() bool, msgAndArgs ...any) {
	t.Helper()
	time.Sleep(d)
	settled(t, cond, msgAndArgs...)
}

// soon moves the bubble's clock on a little at a time until cond holds, for waits whose exact time the test doesn't pin; it fails after a fake minute.
func soon(t *testing.T, cond func() bool, msgAndArgs ...any) {
	t.Helper()

	for range time.Minute / (10 * time.Millisecond) {
		synctest.Wait()

		if cond() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	require.True(t, cond(), msgAndArgs...)
}
