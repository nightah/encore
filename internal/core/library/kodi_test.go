package library_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/library"
	"github.com/nightah/encore/internal/core/media"
)

// TestKodiCatalog runs the catalog against the Kodi backend and fakekodi, end to end.
func TestKodiCatalog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const debounce = 10 * time.Millisecond

		// The bubble's clock starts in 2000; move it on so the library's dates are in the past.
		time.Sleep(time.Until(time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)))

		s := fakekodi.NewNetwork().New(t, fakekodi.Options{})

		cfg, err := kodi.ConfigFromURLs(s.URL(), s.WebSocketURL())
		require.NoError(t, err)

		cfg.ReconnectMin, cfg.ReconnectMax = 10*time.Millisecond, 50*time.Millisecond
		cfg.HTTPClient = s.Client()

		b := kodi.NewBackend(kodi.NewClient(cfg, nil), kodi.BackendOptions{Location: time.UTC})
		c := library.New("lounge", b, library.Options{Debounce: debounce, RefreshInterval: -1})

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
		require.Len(t, c.Items(media.KindMovie), 7)
		require.Equal(t, 1, s.WebSocketClients())
		assert.Len(t, c.Items(media.KindSong), 3)
		assert.Len(t, c.Items(media.KindPlaylist), 2)

		paddington := media.Ref{Kind: media.KindMovie, ID: 8}
		wicked := media.Ref{Kind: media.KindMovie, ID: 9}

		// Kodi dated this one by its file, years ago, so only the scan's update notification brings it in.
		s.Update(func(l *fakekodi.Library) {
			l.Movies = append(l.Movies, fakekodi.Movie{MovieID: 8, Label: "Paddington", Title: "Paddington", Year: 2014, DateAdded: "2015-01-01 10:00:00"})
		})
		s.Notify("VideoLibrary.OnUpdate", map[string]any{"id": 8, "type": "movie", "added": true})

		time.Sleep(debounce)
		synctest.Wait()

		_, ok := c.Get(paddington)
		require.True(t, ok)

		// Without a notification, the live re-query still finds it.
		s.Update(func(l *fakekodi.Library) {
			l.Movies = append(l.Movies, fakekodi.Movie{MovieID: 9, Label: "Wicked", Title: "Wicked", Year: 2024, DateAdded: "2016-01-01 10:00:00"})
		})

		require.NoError(t, c.Delta(t.Context()))
		_, ok = c.Get(wicked)
		assert.False(t, ok, "an old dateadded hides the movie from deltas")

		found, err := c.Lookup(t.Context(), media.KindMovie, "wick")
		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.Equal(t, wicked, found[0].Ref())

		_, ok = c.Get(wicked)
		assert.True(t, ok)

		s.Update(func(l *fakekodi.Library) {
			l.Movies = slices.DeleteFunc(l.Movies, func(m fakekodi.Movie) bool { return m.MovieID == 8 })
		})
		s.Notify("VideoLibrary.OnRemove", map[string]any{"id": 8, "type": "movie"})

		time.Sleep(debounce)
		synctest.Wait()

		_, ok = c.Get(paddington)
		assert.False(t, ok)
	})
}
