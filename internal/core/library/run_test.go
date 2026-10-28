package library

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

// logBuffer collects log output written from Run's goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.String()
}

// startRun runs c in the test's bubble and stops it when the test ends.
func startRun(t *testing.T, c *Catalog) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)

	go func() { done <- c.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})

	synctest.Wait()
}

func newLoggedCatalog(b *fakeBackend, opts Options) (*Catalog, *logBuffer) {
	logs := &logBuffer{}
	opts.Logger = slog.New(slog.NewTextHandler(logs, nil))

	c := New("lounge", b, opts)

	return c, logs
}

func TestRunResubscribesAfterTheSubscriptionEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := seed()
		b.failOn("Subscribe", errRefused)
		c, logs := newLoggedCatalog(b, Options{RefreshInterval: -1})

		startRun(t, c)
		assert.Equal(t, 1, b.count("Subscribe"))
		assert.Contains(t, logs.String(), "backend subscription ended")

		time.Sleep(resubscribeDelay - time.Millisecond)
		synctest.Wait()
		assert.Equal(t, 1, b.count("Subscribe"), "it waits before trying again")

		b.failOn("Subscribe", nil)
		time.Sleep(time.Millisecond)
		synctest.Wait()
		assert.Equal(t, 2, b.count("Subscribe"))

		b.put(movie(3, "Conclave", t0))
		b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 3}})
		time.Sleep(DefaultDebounce)
		synctest.Wait()

		_, ok := c.Get(media.Ref{Kind: media.KindMovie, ID: 3})
		assert.True(t, ok, "events flow again on the new subscription")
	})
}

func TestRunStopsWhileWaitingToResubscribe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := seed()
		b.failOn("Subscribe", errRefused)
		c := New("lounge", b, Options{RefreshInterval: -1})

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error)

		go func() { done <- c.Run(ctx) }()

		synctest.Wait()
		cancel()

		require.NoError(t, <-done, "stopping is not an error")
		assert.Equal(t, 1, b.count("Subscribe"))
	})
}

func TestRunWarnsAboutFailedUpdates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fail  func(*fakeBackend)
		event media.Event
		warn  string
	}{
		{
			name:  "resync",
			fail:  func(b *fakeBackend) { b.mu.Lock(); b.fail[media.KindMovie] = errRefused; b.mu.Unlock() },
			event: media.Event{Kind: media.EventConnected},
			warn:  "library resync failed",
		},
		{
			name:  "delta",
			fail:  func(b *fakeBackend) { b.failOn("RecentlyAdded", errRefused) },
			event: media.Event{Kind: media.EventLibraryScanned},
			warn:  "library delta failed",
		},
		{
			name:  "refresh",
			fail:  func(b *fakeBackend) { b.failOn("Item", errRefused) },
			event: media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1}},
			warn:  "library item refresh failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := seed()
				c, logs := newLoggedCatalog(b, Options{RefreshInterval: -1, MinResync: time.Minute})

				startRun(t, c)
				require.NotContains(t, logs.String(), "level=WARN")

				time.Sleep(time.Hour)
				tc.fail(b)
				b.emit(tc.event)
				time.Sleep(DefaultDebounce)
				synctest.Wait()

				assert.Contains(t, logs.String(), tc.warn)
				assert.Contains(t, logs.String(), "refused", "the cause is logged")

				_, ok := c.Get(media.Ref{Kind: media.KindMovie, ID: 1})
				assert.True(t, ok, "the catalog keeps what it had")
			})
		})
	}
}

func TestRunWarnsAboutAFailedPeriodicDelta(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := seed()
		b.failOn("RecentlyAdded", errRefused)
		c, logs := newLoggedCatalog(b, Options{RefreshInterval: time.Minute})

		startRun(t, c)
		assert.Zero(t, b.count("RecentlyAdded"))

		time.Sleep(time.Minute)
		synctest.Wait()

		assert.Positive(t, b.count("RecentlyAdded"))
		assert.Contains(t, logs.String(), "periodic library delta failed")
	})
}

func TestRunWarnsAboutAFailedInitialSyncAndCarriesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := seed()
		b.mu.Lock()
		b.fail[media.KindArtist] = errRefused
		b.mu.Unlock()

		c, logs := newLoggedCatalog(b, Options{RefreshInterval: -1})

		startRun(t, c)

		assert.Contains(t, logs.String(), "initial library sync incomplete")
		assert.Equal(t, []int{1, 2}, ids(c.Items(media.KindMovie)), "other kinds still load")
		assert.Equal(t, 1, b.count("Subscribe"), "events are followed anyway")
	})
}

func TestRunStopsDuringTheDebounce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := seed()
		c := New("lounge", b, Options{RefreshInterval: -1, Debounce: time.Minute})

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error)

		go func() { done <- c.Run(ctx) }()

		synctest.Wait()
		b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1}})
		time.Sleep(time.Second)
		cancel()

		require.NoError(t, <-done)
		assert.Zero(t, b.count("Item"), "the pending update is dropped, not applied after stopping")
	})
}

func TestRunFullResyncCoversTheRestOfTheBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := seed()
		c, _ := newLoggedCatalog(b, Options{RefreshInterval: -1, MinResync: time.Minute})

		startRun(t, c)
		time.Sleep(time.Hour)

		b.put(movie(1, "Dune: Part One", t0.Add(-72*time.Hour)))
		b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1}})
		b.emit(media.Event{Kind: media.EventLibraryScanned})
		b.emit(media.Event{Kind: media.EventConnected})
		time.Sleep(DefaultDebounce)
		synctest.Wait()

		assert.Equal(t, 2, b.count("Library:movie"), "the reconnect resyncs")
		assert.Zero(t, b.count("Item"), "the resync already fetched the updated item")
		assert.Zero(t, b.count("RecentlyAdded"), "the resync already covers the scan")

		it, _ := c.Get(media.Ref{Kind: media.KindMovie, ID: 1})
		assert.Equal(t, "Dune: Part One", it.Title)
	})
}

func TestRunLastEventForAnItemWins(t *testing.T) {
	ref := media.Ref{Kind: media.KindMovie, ID: 1}

	t.Run("removed then updated is refreshed, not removed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := seed()
			c, _ := newLoggedCatalog(b, Options{RefreshInterval: -1})
			rec := &recorder{}
			c.OnChange(rec.hook)

			startRun(t, c)
			rec.take()

			b.emit(media.Event{Kind: media.EventItemRemoved, Ref: ref})
			b.emit(media.Event{Kind: media.EventItemUpdated, Ref: ref})
			time.Sleep(DefaultDebounce)
			synctest.Wait()

			_, ok := c.Get(ref)
			assert.True(t, ok)
			assert.Equal(t, 1, b.count("Item"))
			assert.Empty(t, rec.take(), "the unchanged film is neither removed nor re-added")
		})
	})

	t.Run("updated then removed is removed without a fetch", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := seed()
			c, _ := newLoggedCatalog(b, Options{RefreshInterval: -1})
			rec := &recorder{}
			c.OnChange(rec.hook)

			startRun(t, c)
			rec.take()

			b.emit(media.Event{Kind: media.EventItemUpdated, Ref: ref})
			b.emit(media.Event{Kind: media.EventItemRemoved, Ref: ref})
			time.Sleep(DefaultDebounce)
			synctest.Wait()

			_, ok := c.Get(ref)
			assert.False(t, ok)
			assert.Zero(t, b.count("Item"))

			changes := rec.take()
			require.Len(t, changes, 1)
			assert.Equal(t, []media.Ref{ref}, changes[0].removed)
		})
	})
}
