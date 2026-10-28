package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
)

func TestGroupBackendReadsFromTheActiveMember(t *testing.T) {
	lib := fakekodi.DefaultLibrary()
	lounge, bedroom := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})
	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, "upstairs"), kodiConfig("Bedroom", bedroom, "upstairs"))
	ctx := t.Context()
	bedroomID := a.instanceNamed(t, "Bedroom").id

	a.mu.RLock()
	b := a.instanceNamed(t, "Lounge").group.backend
	a.mu.RUnlock()

	// Each member also samples its own library for detection, so only item lookups tell who answered.
	reads := func(srv *fakekodi.Server) int { return len(srv.CallsTo("VideoLibrary.GetMovieDetails")) }

	check := func(t *testing.T) {
		t.Helper()

		eps, err := b.Episodes(ctx, 1)
		require.NoError(t, err)
		assert.Len(t, eps, 3)

		it, err := b.Item(ctx, media.Ref{Kind: media.KindMovie, ID: 6})
		require.NoError(t, err)
		assert.Equal(t, "Hot Fuzz", it.Title)

		next, err := b.NextEpisode(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, 102, next.ID, "the first unwatched episode after the last watched")

		found, err := b.Search(ctx, media.KindMovie, "dune")
		require.NoError(t, err)
		assert.Len(t, found, 3)
	}

	before := reads(bedroom)

	check(t)
	assert.Equal(t, before, reads(bedroom), "the source answers while it's reachable")

	lounge.Close()
	require.Eventually(t, func() bool { return b.current().id == bedroomID }, eventually, 10*time.Millisecond)

	before = reads(bedroom)

	check(t)
	assert.Greater(t, reads(bedroom), before, "another member answers while the source is away")
}

func TestGroupBackendForwardsConnectionEventsOnlyFromTheActiveMember(t *testing.T) {
	lounge, bedroom := &Instance{id: 1, name: "Lounge"}, &Instance{id: 2, name: "Bedroom"}
	b := &groupBackend{members: []*Instance{lounge, bedroom}, source: lounge, nudge: make(chan struct{}, 1)}
	b.active.Store(lounge)

	var got []media.EventKind

	b.forward(lounge, media.Event{Kind: media.EventConnected})
	b.emit = func(ev media.Event) { got = append(got, ev.Kind) }

	b.forward(bedroom, media.Event{Kind: media.EventDisconnected})
	b.forward(lounge, media.Event{Kind: media.EventDisconnected})
	b.forward(bedroom, media.Event{Kind: media.EventLibraryScanned})

	assert.Equal(t, []media.EventKind{media.EventDisconnected, media.EventLibraryScanned}, got, "a non-active member's connection events don't reach the catalog, but its library events do")
	assert.Len(t, b.nudge, 1, "connection events prompt a check of which member should be active")
}
