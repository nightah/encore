package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

var errRefused = errors.New("refused")

func TestDeltaKeepsGoingPastAFailingKind(t *testing.T) {
	b := seed()
	c, rec := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie, media.KindTVShow}})
	require.NoError(t, c.Sync(t.Context()))
	rec.take()

	b.put(movie(3, "Conclave", t0.Add(-time.Hour)))
	b.failOn("RecentlyAdded", errRefused)

	require.ErrorIs(t, c.Delta(t.Context()), errRefused)
	assert.Equal(t, []int{1, 2}, ids(c.Items(media.KindMovie)), "nothing new is known")
	assert.Equal(t, 3, b.count("RecentlyAdded"), "every kind and episodes are still asked for")

	b.failOn("RecentlyAdded", nil)
	require.NoError(t, c.Delta(t.Context()))
	assert.Equal(t, []int{1, 2, 3}, ids(c.Items(media.KindMovie)), "the next delta catches up")
}

func TestDeltaRetriesAFailedFullListing(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie}})
	require.NoError(t, c.Sync(t.Context()))

	c.SetKinds([]media.Kind{media.KindMovie, media.KindArtist})

	b.mu.Lock()
	b.fail[media.KindArtist] = errRefused
	b.mu.Unlock()

	require.ErrorIs(t, c.Delta(t.Context()), errRefused)
	assert.Empty(t, c.Items(media.KindArtist))

	b.mu.Lock()
	delete(b.fail, media.KindArtist)
	b.mu.Unlock()

	require.NoError(t, c.Delta(t.Context()))
	assert.Equal(t, []int{5}, ids(c.Items(media.KindArtist)), "the kind is still listed in full, not by date added")
	assert.Equal(t, 2, b.count("Library:artist"))
}

func TestDeltaStoppedIsOneError(t *testing.T) {
	for name, fail := range map[string]func(*fakeBackend, error){
		"full listing": func(b *fakeBackend, err error) {
			b.mu.Lock()
			b.fail[media.KindPlaylist] = err
			b.mu.Unlock()
		},
		"recently added": func(b *fakeBackend, err error) { b.failOn("RecentlyAdded", err) },
	} {
		t.Run(name, func(t *testing.T) {
			b := seed()
			c, rec := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindPlaylist, media.KindMovie, media.KindTVShow}})
			require.NoError(t, c.Sync(t.Context()))
			rec.take()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			fail(b, ctx.Err())

			err := c.Delta(ctx)

			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, "context canceled", err.Error())
			assert.Empty(t, rec.take(), "a stopped delta changes nothing")
			assert.True(t, c.Stats().LastDelta.IsZero(), "a stopped delta isn't a delta")
		})
	}
}

func TestEpisodesAreNotCachedOnError(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})
	b.failOn("Episodes", errRefused)

	eps, err := c.Episodes(t.Context(), 10)
	require.ErrorIs(t, err, errRefused)
	assert.Nil(t, eps)

	b.failOn("Episodes", nil)

	eps, err = c.Episodes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, []int{101, 102}, ids(eps), "the failure wasn't cached")
	assert.Equal(t, 2, b.count("Episodes"))
}

func TestRefreshEpisode(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})

	_, err := c.Episodes(t.Context(), 10)
	require.NoError(t, err)

	renamed := episode(101, 10, 1, 1, t0.Add(-48*time.Hour))
	renamed.Title = "Good News About Hell"
	b.put(renamed)

	require.NoError(t, c.Refresh(t.Context(), renamed.Ref()))

	eps, err := c.Episodes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, []int{101, 102}, ids(eps), "replaced in place, not added again")
	assert.Equal(t, "Good News About Hell", eps[0].Title)

	got, ok := c.Get(renamed.Ref())
	require.True(t, ok)
	assert.Equal(t, "Good News About Hell", got.Title)
}

func TestRefreshEpisodeOfUncachedShow(t *testing.T) {
	b := seed()
	c, _ := newCatalog(b, newClock(), Options{})

	require.NoError(t, c.Refresh(t.Context(), media.Ref{Kind: media.KindEpisode, ID: 101}))
	_, ok := c.Get(media.Ref{Kind: media.KindEpisode, ID: 101})
	assert.False(t, ok, "episodes of a show nobody asked about aren't cached")

	require.NoError(t, c.Refresh(t.Context(), media.Ref{Kind: media.KindEpisode, ID: 999}), "an unknown episode is already gone")
	assert.Zero(t, c.Stats().Episodes)
}

func TestRefreshFailureKeepsTheItem(t *testing.T) {
	b := seed()
	c, rec := newCatalog(b, newClock(), Options{})
	require.NoError(t, c.Sync(t.Context()))
	rec.take()

	b.failOn("Item", errRefused)

	err := c.Refresh(t.Context(), media.Ref{Kind: media.KindMovie, ID: 1})

	require.ErrorIs(t, err, errRefused)
	require.EqualError(t, err, "refreshing movie 1: refused")

	_, ok := c.Get(media.Ref{Kind: media.KindMovie, ID: 1})
	assert.True(t, ok, "a failed refresh isn't a removal")
	assert.Empty(t, rec.take())
}

func TestLookupFailure(t *testing.T) {
	for name, b := range map[string]media.Source{
		"listing":   seed(),
		"searching": searchingBackend{seed()},
	} {
		t.Run(name, func(t *testing.T) {
			c, rec := newCatalog(b, newClock(), Options{})

			var fake *fakeBackend

			switch b := b.(type) {
			case *fakeBackend:
				fake = b
			case searchingBackend:
				fake = b.fakeBackend
			}

			fake.mu.Lock()
			fake.fail[media.KindMovie] = errRefused
			fake.mu.Unlock()
			fake.failOn("Search", errRefused)

			found, err := c.Lookup(t.Context(), media.KindMovie, "Dune")

			require.ErrorIs(t, err, errRefused)
			require.EqualError(t, err, "looking up movie “Dune”: refused")
			assert.Nil(t, found)
			assert.Empty(t, c.Items(media.KindMovie))
			assert.Empty(t, rec.take())
		})
	}
}

func TestLookupFindsNothing(t *testing.T) {
	c, rec := newCatalog(seed(), newClock(), Options{})

	found, err := c.Lookup(t.Context(), media.KindMovie, "paddington")

	require.NoError(t, err)
	assert.Empty(t, found)
	assert.Empty(t, rec.take(), "nothing to report")
}

func TestSyncWithSomeKindsFailingStillCountsAsASync(t *testing.T) {
	b := seed()
	b.mu.Lock()
	b.fail[media.KindArtist] = errRefused
	b.mu.Unlock()

	clk := newClock()
	c, _ := newCatalog(b, clk, Options{Kinds: []media.Kind{media.KindMovie, media.KindArtist}})

	require.ErrorIs(t, c.Sync(t.Context()), errRefused)
	assert.Equal(t, t0, c.Stats().LastSync)

	b.mu.Lock()
	delete(b.fail, media.KindArtist)
	b.mu.Unlock()

	clk.advance(time.Minute)
	require.NoError(t, c.Delta(t.Context()))
	assert.Equal(t, 1, b.count("Library:movie"), "a delta, not another full sync")
	assert.Equal(t, []int{5}, ids(c.Items(media.KindArtist)), "the kind that failed catches up by date added")
}

func TestSyncWithEveryKindFailingIsNotASync(t *testing.T) {
	b := seed()
	b.mu.Lock()
	b.fail[media.KindMovie] = errRefused
	b.mu.Unlock()

	c, _ := newCatalog(b, newClock(), Options{Kinds: []media.Kind{media.KindMovie}})

	require.ErrorIs(t, c.Sync(t.Context()), errRefused)
	assert.True(t, c.Stats().LastSync.IsZero())

	b.mu.Lock()
	delete(b.fail, media.KindMovie)
	b.mu.Unlock()

	require.NoError(t, c.Delta(t.Context()))
	assert.Equal(t, 2, b.count("Library:movie"), "the delta falls back to a full sync")
	assert.Zero(t, b.count("RecentlyAdded"))
}

func TestResyncKeepsTheEpisodeWatermark(t *testing.T) {
	b := seed()
	clk := newClock()
	c, _ := newCatalog(b, clk, Options{Kinds: []media.Kind{media.KindMovie}})
	require.NoError(t, c.Sync(t.Context()))

	b.put(episode(103, 10, 1, 3, t0.Add(-time.Hour)))
	require.NoError(t, c.Delta(t.Context()))

	clk.advance(48 * time.Hour)
	require.NoError(t, c.Sync(t.Context()))
	require.NoError(t, c.Delta(t.Context()))

	b.mu.Lock()
	defer b.mu.Unlock()

	require.Len(t, b.sinces, 4)
	assert.Equal(t, t0.Add(-episodeLookback-deltaSlack), b.sinces[1], "the first sync starts episodes a day back")
	assert.Equal(t, t0.Add(-time.Hour-deltaSlack), b.sinces[3], "a later sync keeps the newest episode seen")
}
