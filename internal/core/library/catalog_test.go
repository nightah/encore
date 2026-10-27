package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

type clock struct{ ns atomic.Int64 }

func newClock() *clock {
	c := &clock{}
	c.set(t0)

	return c
}

func (c *clock) set(t time.Time)         { c.ns.Store(t.UnixNano()) }
func (c *clock) advance(d time.Duration) { c.ns.Add(int64(d)) }
func (c *clock) now() time.Time          { return time.Unix(0, c.ns.Load()).UTC() }
func movie(id int, title string, added time.Time) media.Item {
	return media.Item{Kind: media.KindMovie, ID: id, Title: title, DateAdded: added}
}

func show(id int, title string) media.Item {
	return media.Item{Kind: media.KindTVShow, ID: id, Title: title, DateAdded: t0.Add(-48 * time.Hour)}
}

func episode(id, showID, season, ep int, added time.Time) media.Item {
	return media.Item{Kind: media.KindEpisode, ID: id, ShowID: showID, Season: season, Episode: ep, Title: "Episode", DateAdded: added}
}

type change struct {
	upserted []media.Item
	removed  []media.Ref
}

type recorder struct {
	mu      sync.Mutex
	changes []change
}

func (r *recorder) hook(instance string, upserted []media.Item, removed []media.Ref) {
	if instance != "lounge" {
		panic("wrong instance " + instance)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.changes = append(r.changes, change{upserted, removed})
}

func (r *recorder) take() []change {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := r.changes
	r.changes = nil

	return out
}

func ids(items []media.Item) []int {
	out := make([]int, len(items))
	for i := range items {
		out[i] = items[i].ID
	}

	return out
}

func newCatalog(b media.Source, clk *clock, opts Options) (*Catalog, *recorder) {
	opts.Now = clk.now
	c := New("lounge", b, opts)
	rec := &recorder{}
	c.OnChange(rec.hook)

	return c, rec
}

func seed() *fakeBackend {
	return newFakeBackend(
		movie(1, "Dune", t0.Add(-72*time.Hour)),
		movie(2, "Hot Fuzz", t0.Add(-48*time.Hour)),
		show(10, "Severance"),
		episode(101, 10, 1, 1, t0.Add(-48*time.Hour)),
		episode(102, 10, 1, 2, t0.Add(-47*time.Hour)),
		media.Item{Kind: media.KindArtist, ID: 5, Title: "Queen"},
	)
}

func TestSyncAndChangeHooks(t *testing.T) {
	b := seed()
	c, rec := newCatalog(b, newClock(), Options{})

	require.NoError(t, c.Sync(t.Context()))
	assert.Equal(t, []int{1, 2}, ids(c.Items(media.KindMovie)))
	assert.Equal(t, []int{10}, ids(c.Items(media.KindTVShow)))
	assert.Empty(t, c.Items(media.KindEpisode), "episodes are not listed by a sync")

	changes := rec.take()
	require.Len(t, changes, 1)
	assert.Len(t, changes[0].upserted, 4)
	assert.Empty(t, changes[0].removed)

	got, ok := c.Get(media.Ref{Kind: media.KindMovie, ID: 2})
	require.True(t, ok)
	assert.Equal(t, "Hot Fuzz", got.Title)

	require.NoError(t, c.Sync(t.Context()))
	assert.Empty(t, rec.take(), "an unchanged library reports nothing")

	b.put(media.Item{Kind: media.KindMovie, ID: 2, Title: "Hot Fuzz", PlayCount: 1, DateAdded: t0.Add(-48 * time.Hour)})
	b.put(movie(3, "Conclave", t0.Add(-time.Hour)))
	b.del(media.Ref{Kind: media.KindMovie, ID: 1})

	require.NoError(t, c.Sync(t.Context()))
	assert.Equal(t, []int{2, 3}, ids(c.Items(media.KindMovie)))

	changes = rec.take()
	require.Len(t, changes, 1)
	assert.ElementsMatch(t, []int{2, 3}, ids(changes[0].upserted))
	assert.Equal(t, []media.Ref{{Kind: media.KindMovie, ID: 1}}, changes[0].removed)

	stats := c.Stats()
	assert.Equal(t, map[media.Kind]int{media.KindMovie: 2, media.KindTVShow: 1, media.KindArtist: 1}, stats.Counts)
	assert.Equal(t, t0, stats.LastSync)
}

func TestSyncKeepsKindThatFails(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})
	require.NoError(t, c.Sync(t.Context()))

	boom := errors.New("boom")

	b.mu.Lock()
	b.fail[media.KindMovie] = boom
	b.mu.Unlock()

	b.del(media.Ref{Kind: media.KindArtist, ID: 5})

	err := c.Sync(t.Context())
	require.ErrorIs(t, err, boom)
	assert.Len(t, c.Items(media.KindMovie), 2, "movies survive a failed listing")
	assert.Empty(t, c.Items(media.KindArtist), "other kinds still sync")
}

func TestDelta(t *testing.T) {
	b := seed()
	clk := newClock()
	c, rec := newCatalog(b, clk, Options{Kinds: []media.Kind{media.KindMovie, media.KindTVShow}})

	require.NoError(t, c.Delta(t.Context()), "delta before any sync does a full sync")
	assert.Equal(t, 2, b.count("Library"))
	rec.take()

	b.put(movie(3, "Conclave", t0.Add(-time.Hour)))
	require.NoError(t, c.Delta(t.Context()))

	assert.Equal(t, []int{1, 2, 3}, ids(c.Items(media.KindMovie)))

	changes := rec.take()
	require.Len(t, changes, 1)
	assert.Equal(t, []int{3}, ids(changes[0].upserted))

	b.mu.Lock()
	movieSince := b.sinces[0]
	b.mu.Unlock()
	assert.Equal(t, t0.Add(-48*time.Hour-deltaSlack), movieSince, "asks from the newest known item, minus slack")

	// Kodi can date a new item by its file time, which a delta never sees; Lookup finds it.
	b.put(movie(4, "Rocky II", t0.Add(-5*365*24*time.Hour)))
	require.NoError(t, c.Delta(t.Context()))
	_, ok := c.Get(media.Ref{Kind: media.KindMovie, ID: 4})
	assert.False(t, ok)

	found, err := c.Lookup(t.Context(), media.KindMovie, "rocky")
	require.NoError(t, err)
	assert.Equal(t, []int{4}, ids(found))

	_, ok = c.Get(media.Ref{Kind: media.KindMovie, ID: 4})
	assert.True(t, ok, "lookup results join the catalog")
	assert.Equal(t, []int{4}, ids(rec.take()[0].upserted))
	assert.Equal(t, t0, c.Stats().LastDelta)
}

func TestWatermarkIgnoresFutureDates(t *testing.T) {
	b := newFakeBackend(movie(1, "Bogus", t0.Add(100*24*time.Hour)))
	c, _ := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie}})

	require.NoError(t, c.Sync(t.Context()))
	require.NoError(t, c.Delta(t.Context()))

	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Equal(t, t0.Add(-deltaSlack), b.sinces[0], "a future date is capped at now")
}

func TestLookup(t *testing.T) {
	plain := seed()
	c, _ := newCatalog(plain, newClock(), Options{})

	found, err := c.Lookup(t.Context(), media.KindMovie, "FUZZ")
	require.NoError(t, err)
	assert.Equal(t, []int{2}, ids(found))
	assert.Equal(t, 1, plain.count("Library:movie"), "without search it lists the kind")

	searching := searchingBackend{seed()}
	c, _ = newCatalog(searching, newClock(), Options{})

	found, err = c.Lookup(t.Context(), media.KindMovie, "dune")
	require.NoError(t, err)
	assert.Equal(t, []int{1}, ids(found))
	assert.Equal(t, 1, searching.count("Search"))
	assert.Zero(t, searching.count("Library"))

	_, err = c.Lookup(t.Context(), media.KindEpisode, "x")
	require.Error(t, err)
}

func TestEpisodesCache(t *testing.T) {
	b := seed()
	clk := newClock()
	c, rec := newCatalog(b, clk, Options{Kinds: []media.Kind{media.KindTVShow}})
	require.NoError(t, c.Sync(t.Context()))
	rec.take()

	eps, err := c.Episodes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, []int{101, 102}, ids(eps))

	_, err = c.Episodes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, b.count("Episodes"), "cached after the first call")

	got, ok := c.Get(media.Ref{Kind: media.KindEpisode, ID: 102})
	require.True(t, ok)
	assert.Equal(t, 2, got.Episode)
	assert.Equal(t, 2, c.Stats().Episodes)

	// A new episode of a cached show arrives through the delta, in order.
	b.put(episode(103, 10, 1, 3, t0.Add(-time.Hour)), episode(100, 10, 0, 1, t0.Add(-time.Hour)))
	require.NoError(t, c.Delta(t.Context()))

	eps, err = c.Episodes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, []int{100, 101, 102, 103}, ids(eps))
	assert.Empty(t, rec.take(), "episodes are not reported to hooks")

	require.NoError(t, c.Refresh(t.Context(), media.Ref{Kind: media.KindEpisode, ID: 100}))
	b.del(media.Ref{Kind: media.KindEpisode, ID: 100})
	require.NoError(t, c.Refresh(t.Context(), media.Ref{Kind: media.KindEpisode, ID: 100}))

	eps, err = c.Episodes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, []int{101, 102, 103}, ids(eps), "a vanished episode is dropped")

	require.NoError(t, c.Sync(t.Context()))
	_, ok = c.Get(media.Ref{Kind: media.KindEpisode, ID: 101})
	assert.False(t, ok, "a full sync empties the episode cache")
}

func TestRecentlyAdded(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})
	require.NoError(t, c.Sync(t.Context()))

	_, err := c.Episodes(t.Context(), 10)
	require.NoError(t, err)

	assert.Equal(t, []int{102, 101, 2, 10}, ids(c.RecentlyAdded(4)), "newest first, ties by kind then ID")
	assert.Equal(t, []int{2, 1}, ids(c.RecentlyAdded(10, media.KindMovie)))
	assert.Empty(t, c.RecentlyAdded(0))
}

func runCatalog(t *testing.T, c *Catalog, b *fakeBackend) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = c.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	select {
	case <-b.subbed:
	case <-time.After(2 * time.Second):
		t.Fatal("catalog never subscribed")
	}
}

func TestRunAppliesEvents(t *testing.T) {
	b := seed()
	clk := newClock()
	c, rec := newCatalog(b, clk, Options{Debounce: 5 * time.Millisecond, RefreshInterval: -1})
	runCatalog(t, c, b)
	rec.take()

	b.put(media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune", PlayCount: 2, DateAdded: t0.Add(-72 * time.Hour)})
	b.put(movie(7, "Conclave", t0.Add(-9000*time.Hour)))
	b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1}})
	b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 7}})
	b.emit(media.Event{Kind: media.EventItemRemoved, Ref: media.Ref{Kind: media.KindArtist, ID: 5}})

	require.Eventually(t, func() bool {
		it, ok := c.Get(media.Ref{Kind: media.KindMovie, ID: 1})
		_, added := c.Get(media.Ref{Kind: media.KindMovie, ID: 7})

		return ok && it.PlayCount == 2 && added && len(c.Items(media.KindArtist)) == 0
	}, 2*time.Second, 5*time.Millisecond)

	var upserted []int

	var removed []media.Ref

	for _, ch := range rec.take() {
		upserted = append(upserted, ids(ch.upserted)...)
		removed = append(removed, ch.removed...)
	}

	assert.ElementsMatch(t, []int{1, 7}, upserted)
	assert.Equal(t, []media.Ref{{Kind: media.KindArtist, ID: 5}}, removed)
	assert.Equal(t, t0, c.Stats().LastEvent)

	// An update for something Kodi no longer has removes it.
	b.del(media.Ref{Kind: media.KindMovie, ID: 2})
	b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 2}})
	require.Eventually(t, func() bool { _, ok := c.Get(media.Ref{Kind: media.KindMovie, ID: 2}); return !ok }, 2*time.Second, 5*time.Millisecond)

	// A finished scan runs a delta.
	before := b.count("RecentlyAdded")
	b.emit(media.Event{Kind: media.EventLibraryScanned})
	require.Eventually(t, func() bool { return b.count("RecentlyAdded") > before }, 2*time.Second, 5*time.Millisecond)

	// Playback events change nothing.
	b.emit(media.Event{Kind: media.EventPlaybackStarted, Ref: media.Ref{Kind: media.KindMovie, ID: 1}})
}

func TestRunCoalescesBursts(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{Debounce: 50 * time.Millisecond, RefreshInterval: -1})
	runCatalog(t, c, b)

	for range 100 {
		b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1}})
	}

	require.Eventually(t, func() bool { return b.count("Item") >= 1 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, b.count("Item"), "a burst of updates to one item fetches it once")
}

func TestRunResyncsOnReconnect(t *testing.T) {
	b := seed()
	clk := newClock()
	c, _ := newCatalog(b, clk, Options{Debounce: 5 * time.Millisecond, RefreshInterval: -1, MinResync: time.Minute})
	runCatalog(t, c, b)

	syncs := b.count("Library:movie")
	require.Equal(t, 1, syncs, "Run starts with a full sync")

	b.emit(media.Event{Kind: media.EventDisconnected})
	b.emit(media.Event{Kind: media.EventConnected})
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, b.count("Library:movie"), "a reconnect right after a sync doesn't sync again")

	clk.advance(2 * time.Minute)
	b.emit(media.Event{Kind: media.EventConnected})
	require.Eventually(t, func() bool { return b.count("Library:movie") == 2 }, 2*time.Second, 5*time.Millisecond)
}

func TestRunPeriodicDelta(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{RefreshInterval: 10 * time.Millisecond})
	runCatalog(t, c, b)

	require.Eventually(t, func() bool { return b.count("RecentlyAdded") >= 2 }, 2*time.Second, 5*time.Millisecond)
}

func TestSnapshotRestore(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})
	require.NoError(t, c.Sync(t.Context()))

	_, err := c.Episodes(t.Context(), 10)
	require.NoError(t, err)

	data, err := json.Marshal(c.Snapshot())
	require.NoError(t, err)

	var snap Snapshot
	require.NoError(t, json.Unmarshal(data, &snap))

	restored, rec := newCatalog(newFakeBackend(movie(99, "Stale", t0)), newClock(), Options{})
	require.NoError(t, restored.Sync(t.Context()))
	rec.take()

	require.NoError(t, restored.Restore(snap))
	assert.Equal(t, c.Items(media.KindMovie), restored.Items(media.KindMovie))
	assert.Equal(t, c.Stats(), restored.Stats())

	eps, err := restored.Episodes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, []int{101, 102}, ids(eps))

	changes := rec.take()
	require.Len(t, changes, 1)
	assert.Len(t, changes[0].upserted, 4)
	assert.Equal(t, []media.Ref{{Kind: media.KindMovie, ID: 99}}, changes[0].removed)

	require.Error(t, New("bedroom", b, Options{}).Restore(snap))
}

func TestRestoreSkipsKindsNoLongerLoaded(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})
	require.NoError(t, c.Sync(t.Context()))

	restored, _ := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie}})
	require.NoError(t, restored.Restore(c.Snapshot()))

	assert.Equal(t, []int{1, 2}, ids(restored.Items(media.KindMovie)))
	assert.Empty(t, restored.Items(media.KindArtist), "no sync would ever remove a kind the catalog doesn't load")
	assert.Empty(t, restored.Items(media.KindTVShow))
}

func TestConcurrentReadsDuringUpdates(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{Debounce: time.Millisecond, RefreshInterval: time.Millisecond})
	runCatalog(t, c, b)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup

	for range 4 {
		wg.Go(func() {
			for ctx.Err() == nil {
				_ = c.Items(media.KindMovie)
				_, _ = c.Get(media.Ref{Kind: media.KindMovie, ID: 1})
				_ = c.RecentlyAdded(3)
				_ = c.Stats()
				_ = c.Snapshot()
				_, _ = c.Episodes(ctx, 10)
			}
		})
	}

	wg.Go(func() {
		for i := 0; ctx.Err() == nil; i++ {
			b.put(movie(1000+i%10, "Churn", t0))
			b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1000 + i%10}})
			_ = c.Sync(ctx)
		}
	})

	wg.Wait()
}

func TestSetKinds(t *testing.T) {
	b := seed()
	c, rec := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie, media.KindArtist}})

	require.NoError(t, c.Sync(t.Context()))
	assert.Equal(t, []int{5}, ids(c.Items(media.KindArtist)))
	rec.take()

	// Music turned off: its titles leave at once and later syncs don't ask Kodi for them.
	c.SetKinds([]media.Kind{media.KindMovie})
	assert.Empty(t, c.Items(media.KindArtist))
	changes := rec.take()
	require.Len(t, changes, 1)
	assert.Equal(t, []media.Ref{{Kind: media.KindArtist, ID: 5}}, changes[0].removed)

	listed := b.count("Library")
	require.NoError(t, c.Sync(t.Context()))
	assert.Equal(t, listed+1, b.count("Library"), "only movies are listed")
	assert.Empty(t, c.Items(media.KindArtist))

	// Back on: the next delta lists artists in full, not just the recently added ones.
	c.SetKinds([]media.Kind{media.KindMovie, media.KindArtist})
	require.NoError(t, c.Delta(t.Context()))
	assert.Equal(t, []int{5}, ids(c.Items(media.KindArtist)))

	before := b.count("Library")
	require.NoError(t, c.Delta(t.Context()))
	assert.Equal(t, before, b.count("Library"), "after that, deltas go by date added again")
}

func TestSyncErrorsReadOnOneLine(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie, media.KindArtist}})

	movies, artists := errors.New("listing movies: refused"), errors.New("listing artists: refused")

	b.mu.Lock()
	b.fail[media.KindMovie], b.fail[media.KindArtist] = movies, artists
	b.mu.Unlock()

	err := c.Sync(t.Context())
	require.EqualError(t, err, "listing movies: refused; listing artists: refused", "joined on one line, each saying its kind once")
	require.ErrorIs(t, err, movies)
	require.ErrorIs(t, err, artists)
}

func TestSyncStoppedIsOneError(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	b.mu.Lock()
	b.fail[media.KindMovie] = ctx.Err()
	b.mu.Unlock()

	err := c.Sync(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "context canceled", err.Error(), "stopping isn't listed once per kind")
}

func TestRunDoesNotWarnWhenStopping(t *testing.T) {
	b := seed()

	var buf bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	b.mu.Lock()
	b.fail[media.KindMovie] = ctx.Err()
	b.mu.Unlock()

	c, _ := newCatalog(b, newClock(), Options{Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	_ = c.Run(ctx)

	assert.NotContains(t, buf.String(), "level=WARN", "an interrupted sync means Encore is stopping, not that something failed")
}
