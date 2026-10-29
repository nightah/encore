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
	"testing/synctest"
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

// runCatalog runs c until the test ends, returning once it has synced and subscribed. Call it in a synctest bubble.
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

	synctest.Wait()

	select {
	case <-b.subbed:
	default:
		t.Fatal("catalog never subscribed")
	}
}

func TestRunAppliesEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const debounce = 5 * time.Millisecond

		b := seed()
		clk := newClock()
		c, rec := newCatalog(b, clk, Options{Debounce: debounce, RefreshInterval: -1})
		runCatalog(t, c, b)
		rec.take()

		b.put(media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune", PlayCount: 2, DateAdded: t0.Add(-72 * time.Hour)})
		b.put(movie(7, "Conclave", t0.Add(-9000*time.Hour)))
		b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1}})
		b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 7}})
		b.emit(media.Event{Kind: media.EventItemRemoved, Ref: media.Ref{Kind: media.KindArtist, ID: 5}})
		time.Sleep(debounce)
		synctest.Wait()

		it, ok := c.Get(media.Ref{Kind: media.KindMovie, ID: 1})
		require.True(t, ok)
		assert.Equal(t, 2, it.PlayCount, "an update refetches the item")

		_, added := c.Get(media.Ref{Kind: media.KindMovie, ID: 7})
		assert.True(t, added, "an update for an unknown item adds it")
		assert.Empty(t, c.Items(media.KindArtist), "a removal removes")

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
		time.Sleep(debounce)
		synctest.Wait()

		_, ok = c.Get(media.Ref{Kind: media.KindMovie, ID: 2})
		assert.False(t, ok)

		// A finished scan runs a delta.
		before := b.count("RecentlyAdded")
		b.emit(media.Event{Kind: media.EventLibraryScanned})
		time.Sleep(debounce)
		synctest.Wait()
		assert.Greater(t, b.count("RecentlyAdded"), before)

		// Playback events change nothing.
		rec.take()

		calls := b.count("Item") + b.count("RecentlyAdded") + b.count("Library:movie")

		b.emit(media.Event{Kind: media.EventPlaybackStarted, Ref: media.Ref{Kind: media.KindMovie, ID: 1}})
		time.Sleep(debounce)
		synctest.Wait()
		assert.Equal(t, calls, b.count("Item")+b.count("RecentlyAdded")+b.count("Library:movie"), "playback asks Kodi for nothing")
		assert.Empty(t, rec.take(), "and changes nothing")
	})
}

func TestRunCoalescesBursts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const debounce = 50 * time.Millisecond

		b := seed()
		c, _ := newCatalog(b, newClock(), Options{Debounce: debounce, RefreshInterval: -1})
		runCatalog(t, c, b)

		for range 100 {
			b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1}})
		}

		time.Sleep(debounce - time.Millisecond)
		synctest.Wait()
		assert.Zero(t, b.count("Item"), "nothing is fetched while the burst settles")

		time.Sleep(time.Millisecond)
		synctest.Wait()
		assert.Equal(t, 1, b.count("Item"), "a burst of updates to one item fetches it once")

		time.Sleep(10 * debounce)
		synctest.Wait()
		assert.Equal(t, 1, b.count("Item"), "and only once")
	})
}

func TestRunResyncsOnReconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const debounce = 5 * time.Millisecond

		b := seed()
		clk := newClock()
		c, _ := newCatalog(b, clk, Options{Debounce: debounce, RefreshInterval: -1, MinResync: time.Minute})
		runCatalog(t, c, b)

		require.Equal(t, 1, b.count("Library:movie"), "Run starts with a full sync")

		b.emit(media.Event{Kind: media.EventDisconnected})
		b.emit(media.Event{Kind: media.EventConnected})
		time.Sleep(debounce)
		synctest.Wait()
		assert.Equal(t, 1, b.count("Library:movie"), "a reconnect right after a sync doesn't sync again")

		clk.advance(2 * time.Minute)
		b.emit(media.Event{Kind: media.EventConnected})
		time.Sleep(debounce)
		synctest.Wait()
		assert.Equal(t, 2, b.count("Library:movie"), "a reconnect after MinResync syncs in full")
	})
}

func TestRunPeriodicDelta(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const every = 10 * time.Millisecond

		b := seed()
		c, _ := newCatalog(b, newClock(), Options{RefreshInterval: every})
		runCatalog(t, c, b)

		before := b.count("RecentlyAdded")

		time.Sleep(every - time.Millisecond)
		synctest.Wait()
		assert.Equal(t, before, b.count("RecentlyAdded"), "no delta before the interval")

		time.Sleep(time.Millisecond)
		synctest.Wait()

		first := b.count("RecentlyAdded")
		assert.Greater(t, first, before, "a delta every interval")

		time.Sleep(every)
		synctest.Wait()
		assert.Equal(t, 2*first-before, b.count("RecentlyAdded"), "and again the next")
	})
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

func TestChangeHooksSeeFinishedUpdates(t *testing.T) {
	b := seed()
	src, _ := newCatalog(b, newClock(), Options{})
	require.NoError(t, src.Sync(t.Context()))

	_, err := src.Episodes(t.Context(), 10)
	require.NoError(t, err)

	clk := newClock()
	c := New("lounge", b, Options{Now: clk.now})

	var seen []Stats

	c.OnChange(func(string, []media.Item, []media.Ref) { seen = append(seen, c.Stats()) })

	require.NoError(t, c.Restore(src.Snapshot()))
	require.Len(t, seen, 1)
	assert.Equal(t, src.Stats().LastSync, seen[0].LastSync, "a restored catalog is seen with its sync time")
	assert.Equal(t, 2, seen[0].Episodes, "and its episodes")

	clk.advance(time.Hour)
	b.put(movie(3, "Conclave", t0.Add(-time.Hour)))
	require.NoError(t, c.Sync(t.Context()))
	require.Len(t, seen, 2)
	assert.Equal(t, clk.now(), seen[1].LastSync, "a synced catalog is seen with its new sync time")

	clk.advance(time.Hour)
	b.put(movie(4, "Paddington", t0.Add(time.Hour)))
	require.NoError(t, c.Delta(t.Context()))
	require.Len(t, seen, 3)
	assert.Equal(t, clk.now(), seen[2].LastDelta, "and after a delta, with its delta time")
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

// Run under -race: readers, the event loop, periodic deltas and full syncs all touch the catalog at once.
func TestConcurrentReadsDuringUpdates(t *testing.T) {
	const rounds = 200

	b := seed()
	c, _ := newCatalog(b, newClock(), Options{Debounce: time.Millisecond, RefreshInterval: time.Millisecond})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = c.Run(ctx)
	}()

	<-b.subbed

	var wg sync.WaitGroup

	for range 4 {
		wg.Go(func() {
			for range rounds {
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
		for i := range rounds {
			b.put(movie(1000+i%10, "Churn", t0))
			b.emit(media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 1000 + i%10}})
			assert.NoError(t, c.Sync(ctx))
		}
	})

	wg.Wait()
	cancel()
	<-done

	assert.Len(t, c.Items(media.KindMovie), 12, "every churned movie is in the catalog")
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

func TestInstance(t *testing.T) {
	assert.Equal(t, "lounge", New("lounge", seed(), Options{}).Instance())
}

func TestOnChangeRemove(t *testing.T) {
	b := seed()
	c := New("lounge", b, Options{Kinds: []media.Kind{media.KindMovie}})

	var first, second int

	removeFirst := c.OnChange(func(string, []media.Item, []media.Ref) { first++ })
	c.OnChange(func(string, []media.Item, []media.Ref) { second++ })

	require.NoError(t, c.Sync(t.Context()))
	assert.Equal(t, 1, first)
	assert.Equal(t, 1, second)

	removeFirst()
	removeFirst()

	b.put(movie(3, "Conclave", t0))
	require.NoError(t, c.Sync(t.Context()))
	assert.Equal(t, 1, first, "a removed hook isn't called")
	assert.Equal(t, 2, second, "removing one hook leaves the others")
}

func TestUnchangedItemsAreNotReported(t *testing.T) {
	b := seed()
	c, rec := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie, media.KindArtist}})

	require.NoError(t, c.Sync(t.Context()))
	rec.take()

	require.NoError(t, c.Sync(t.Context()))
	assert.Empty(t, rec.take(), "a sync that changes nothing reports nothing")

	watched := movie(2, "Hot Fuzz", t0.Add(-48*time.Hour))
	watched.PlayCount = 1
	b.put(watched)

	require.NoError(t, c.Sync(t.Context()))

	changes := rec.take()
	require.Len(t, changes, 1)
	assert.Equal(t, []int{2}, ids(changes[0].upserted), "only the changed item is reported")
	assert.Empty(t, changes[0].removed)
}

func TestItemEquality(t *testing.T) {
	base := media.Item{
		Kind: media.KindEpisode, ID: 1, Title: "T", SortTitle: "S", OriginalTitle: "O", Year: 2020, Genres: []string{"Drama"},
		ShowID: 2, ShowTitle: "Show", Season: 1, Episode: 2, Artist: "A", Album: "B", PlayCount: 1, Resume: time.Minute,
		Runtime: time.Hour, DateAdded: t0, LastPlayed: t0,
	}

	same := base
	same.Genres = []string{"Drama"}
	same.DateAdded = t0.In(time.FixedZone("AEDT", 11*60*60))
	assert.True(t, equal(&base, &same), "a copy is equal, whatever the time zone")

	for name, change := range map[string]func(*media.Item){
		"kind":           func(i *media.Item) { i.Kind = media.KindMovie },
		"id":             func(i *media.Item) { i.ID++ },
		"title":          func(i *media.Item) { i.Title = "U" },
		"sort title":     func(i *media.Item) { i.SortTitle = "U" },
		"original title": func(i *media.Item) { i.OriginalTitle = "U" },
		"year":           func(i *media.Item) { i.Year++ },
		"genres":         func(i *media.Item) { i.Genres = []string{"Drama", "Crime"} },
		"show":           func(i *media.Item) { i.ShowID++ },
		"show title":     func(i *media.Item) { i.ShowTitle = "U" },
		"season":         func(i *media.Item) { i.Season++ },
		"episode":        func(i *media.Item) { i.Episode++ },
		"artist":         func(i *media.Item) { i.Artist = "U" },
		"album":          func(i *media.Item) { i.Album = "U" },
		"play count":     func(i *media.Item) { i.PlayCount++ },
		"resume":         func(i *media.Item) { i.Resume++ },
		"runtime":        func(i *media.Item) { i.Runtime++ },
		"date added":     func(i *media.Item) { i.DateAdded = i.DateAdded.Add(time.Second) },
		"last played":    func(i *media.Item) { i.LastPlayed = time.Time{} },
	} {
		other := base
		other.Genres = []string{"Drama"}
		change(&other)
		assert.False(t, equal(&base, &other), name)
	}
}

func TestRestoreBareSnapshot(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})

	require.NoError(t, c.Restore(Snapshot{Instance: "lounge", Items: []media.Item{show(10, "Severance")}}))

	eps, err := c.Episodes(t.Context(), 10)
	require.NoError(t, err, "a snapshot without watermarks still takes new ones")
	assert.Equal(t, []int{101, 102}, ids(eps))
	assert.True(t, c.Stats().LastSync.IsZero())

	require.NoError(t, c.Delta(t.Context()))
	assert.Equal(t, []int{1, 2}, ids(c.Items(media.KindMovie)), "never synced, so the delta is a full sync")
}

func TestRestoreWithoutTVDropsEpisodes(t *testing.T) {
	b := seed()
	src, _ := newCatalog(b, newClock(), Options{})
	require.NoError(t, src.Sync(t.Context()))

	_, err := src.Episodes(t.Context(), 10)
	require.NoError(t, err)

	c, _ := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie}})
	require.NoError(t, c.Restore(src.Snapshot()))

	assert.Zero(t, c.Stats().Episodes)
	_, ok := c.Get(media.Ref{Kind: media.KindEpisode, ID: 101})
	assert.False(t, ok, "episodes of shows the catalog doesn't load aren't restored")
	assert.Equal(t, []int{1, 2}, ids(c.Items(media.KindMovie)))
}
