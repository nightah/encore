package app

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/stretchr/testify/require"
)

// present is where a bubble's clock starts, so the fakes' dates are in the past as they would be.
var present = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

// bleveWorkers is how many analysis workers bleve starts by default.
const bleveWorkers = 4

// inBubble runs f in a synctest bubble whose clock reads present.
//
// bleve analyses documents on shared workers started outside any bubble, which can't answer an index inside one, so the bubble gets workers of its own while it runs and the shared ones are put back after. The package's tests don't run in parallel, so swapping them is safe.
func inBubble(t *testing.T, f func(t *testing.T)) {
	t.Helper()

	outside := *bleve.Config
	defer func() { *bleve.Config = outside }()

	synctest.Test(t, func(t *testing.T) {
		inside := zeroOf(bleve.Config)
		inside.Cache, inside.DefaultHighlighter, inside.DefaultKVStore = outside.Cache, outside.DefaultHighlighter, outside.DefaultKVStore
		inside.DefaultMemKVStore, inside.DefaultIndexType, inside.SlowSearchLogThreshold = outside.DefaultMemKVStore, outside.DefaultIndexType, outside.SlowSearchLogThreshold
		inside.SetAnalysisQueueSize(bleveWorkers)
		*bleve.Config = inside

		t.Cleanup(bleve.Config.Shutdown)

		time.Sleep(time.Until(present))
		f(t)
	})
}

// zeroOf returns the zero value of what p points to, for a type that can't be named.
func zeroOf[T any](*T) (zero T) { return zero }

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
