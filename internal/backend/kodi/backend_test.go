package kodi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

var aest = time.FixedZone("AEST", 10*60*60)

func newBackend(t *testing.T, opts fakekodi.Options, bopts BackendOptions) (*Backend, *fakekodi.Server) {
	t.Helper()

	s := newFake(t, opts)

	if bopts.Location == nil {
		bopts.Location = aest
	}

	b := NewBackend(NewClient(testConfig(t, s), nil), bopts)
	// Reverse instead of shuffling, so queue order is predictable but visibly changed.
	b.shuffle = func(n int, swap func(i, j int)) {
		for i := range n / 2 {
			swap(i, n-1-i)
		}
	}

	return b, s
}

func at(s string) time.Time {
	t, err := time.ParseInLocation(kodiTimeLayout, s, aest)
	if err != nil {
		panic(err)
	}

	return t
}

func titles(items []media.Item) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].Title
	}

	return out
}

func lastParams(t *testing.T, s *fakekodi.Server, method string) string {
	t.Helper()

	calls := s.CallsTo(method)
	require.NotEmpty(t, calls, "no call to %s", method)

	return string(calls[len(calls)-1].Params)
}

func TestInfo(t *testing.T) {
	b, _ := newBackend(t, fakekodi.Options{}, BackendOptions{Name: "lounge"})

	info, err := b.Info(t.Context())
	require.NoError(t, err)
	assert.Equal(t, media.Info{Name: "Kodi (fake)", Version: "21.3", APIVersion: "13.5.0"}, info)
}

func TestLibraryMovies(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	movies, err := b.Library(t.Context(), media.KindMovie)
	require.NoError(t, err)
	require.Len(t, movies, 7)

	assert.Equal(t, media.Item{
		Kind:      media.KindMovie,
		ID:        3,
		Title:     "Dune: Part Two",
		Year:      2024,
		Genres:    []string{"Science Fiction"},
		SetID:     1,
		SetTitle:  "Dune Collection",
		Resume:    3605500 * time.Millisecond,
		Runtime:   9960 * time.Second,
		DateAdded: at("2024-06-01 10:00:00"),
	}, movies[2])

	amelie := movies[4]
	assert.Equal(t, "Amelie", amelie.SortTitle)
	assert.Equal(t, "Le Fabuleux Destin d'Amélie Poulain", amelie.OriginalTitle)
	assert.Equal(t, []string{"Comedy", "Romance"}, amelie.Genres)

	hotFuzz := movies[5]
	assert.Equal(t, 3, hotFuzz.PlayCount)
	assert.Equal(t, at("2024-12-24 22:10:00"), hotFuzz.LastPlayed)
	assert.Zero(t, hotFuzz.Resume)

	var params struct {
		Properties []string `json:"properties"`
	}
	require.NoError(t, json.Unmarshal([]byte(lastParams(t, s, "VideoLibrary.GetMovies")), &params))
	assert.Subset(t, params.Properties, []string{"title", "sorttitle", "originaltitle", "resume", "dateadded"})
}

func TestLibraryPaginates(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{PageSize: 3})

	movies, err := b.Library(t.Context(), media.KindMovie)
	require.NoError(t, err)
	assert.Len(t, movies, 7)

	calls := s.CallsTo("VideoLibrary.GetMovies")
	require.Len(t, calls, 3)

	for i, c := range calls {
		var p struct {
			Limits struct{ Start, End int } `json:"limits"`
		}
		require.NoError(t, json.Unmarshal(c.Params, &p))
		assert.Equal(t, i*3, p.Limits.Start)
		assert.Equal(t, i*3+3, p.Limits.End)
	}
}

func TestLibraryKinds(t *testing.T) {
	b, _ := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	shows, err := b.Library(ctx, media.KindTVShow)
	require.NoError(t, err)
	assert.Equal(t, []string{"Severance", "Bluey", "The Office (US)", "The Office (UK)"}, titles(shows))
	assert.Equal(t, "Office (US)", shows[2].SortTitle)

	videos, err := b.Library(ctx, media.KindMusicVideo)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "Queen", videos[0].Artist)
	assert.Equal(t, "A Night at the Opera", videos[0].Album)

	artists, err := b.Library(ctx, media.KindArtist)
	require.NoError(t, err)
	assert.Equal(t, []string{"Queen", "The Beatles"}, titles(artists))
	assert.Equal(t, "Beatles, The", artists[1].SortTitle)

	albums, err := b.Library(ctx, media.KindAlbum)
	require.NoError(t, err)
	require.Len(t, albums, 2)
	assert.Equal(t, media.Item{
		Kind: media.KindAlbum, ID: 1, Title: "A Night at the Opera", Year: 1975, Genres: []string{"Rock"},
		Artist: "Queen", Album: "A Night at the Opera", PlayCount: 2, DateAdded: at("2021-05-01 10:00:00"),
	}, albums[0])

	songs, err := b.Library(ctx, media.KindSong)
	require.NoError(t, err)
	require.Len(t, songs, 3)
	assert.Equal(t, 355*time.Second, songs[0].Runtime)
	assert.Equal(t, "A Night at the Opera", songs[0].Album)

	playlists, err := b.Library(ctx, media.KindPlaylist)
	require.NoError(t, err)
	assert.Equal(t, []string{"Kids", "Road Trip"}, titles(playlists))
	assert.Positive(t, playlists[0].ID)
	assert.NotEqual(t, playlists[0].ID, playlists[1].ID)

	_, err = b.Library(ctx, media.KindEpisode)
	require.Error(t, err)
}

func TestEpisodesAndNextEpisode(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	eps, err := b.Episodes(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"Who Is Alive?", "Woe's Hollow", "Trojan's Horse"}, titles(eps))
	assert.Equal(t, media.Item{
		Kind: media.KindEpisode, ID: 101, Title: "Who Is Alive?", ShowID: 1, ShowTitle: "Severance", Season: 2, Episode: 3,
		PlayCount: 1, Runtime: 2820 * time.Second, DateAdded: at("2025-01-31 10:00:00"), LastPlayed: at("2025-02-01 21:00:00"),
	}, eps[0])

	next, err := b.NextEpisode(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 102, next.ID, "first unwatched episode")

	s.Update(func(l *fakekodi.Library) {
		l.Episodes[2].Resume = fakekodi.Resume{Position: 600, Total: 2940}
	})

	next, err = b.NextEpisode(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 103, next.ID, "an episode in progress wins")
	assert.Equal(t, 10*time.Minute, next.Resume)

	s.Update(func(l *fakekodi.Library) {
		l.Episodes = append(l.Episodes,
			fakekodi.Episode{EpisodeID: 200, TVShowID: 2, ShowTitle: "Bluey", Season: 0, Episode: 1, Title: "Special"})
		l.Episodes[3].PlayCount = 1
	})

	next, err = b.NextEpisode(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, 200, next.ID, "specials only once everything else is watched")

	_, err = b.NextEpisode(ctx, 3)
	require.ErrorIs(t, err, media.ErrNotFound)
}

func TestRecentlyAdded(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{PageSize: 2})

	items, err := b.RecentlyAdded(t.Context(), media.KindMovie, at("2024-01-01 00:00:00"))
	require.NoError(t, err)
	assert.Equal(t, []string{"Conclave", "Dune: Part Two"}, titles(items))
	assert.Len(t, s.CallsTo("VideoLibrary.GetMovies"), 2, "stops paging after the first page that reaches older items")
	assert.Contains(t, lastParams(t, s, "VideoLibrary.GetMovies"), `"sort":{"method":"dateadded","order":"descending"}`)

	items, err = b.RecentlyAdded(t.Context(), media.KindMovie, at("2024-06-01 10:00:00"))
	require.NoError(t, err)
	assert.Equal(t, []string{"Conclave"}, titles(items), "since is exclusive")

	items, err = b.RecentlyAdded(t.Context(), media.KindEpisode, at("2025-02-01 00:00:00"))
	require.NoError(t, err)
	assert.Equal(t, []string{"Trojan's Horse", "Woe's Hollow"}, titles(items))
}

func TestItem(t *testing.T) {
	refs := []media.Ref{
		{Kind: media.KindMovie, ID: 5},
		{Kind: media.KindTVShow, ID: 2},
		{Kind: media.KindEpisode, ID: 201},
		{Kind: media.KindMusicVideo, ID: 1},
		{Kind: media.KindArtist, ID: 2},
		{Kind: media.KindAlbum, ID: 2},
		{Kind: media.KindSong, ID: 3},
	}
	want := []string{"Amélie", "Bluey", "Rain", "Bohemian Rhapsody", "The Beatles", "Abbey Road", "Come Together"}

	for name, v := range map[string][3]int{"omega": {13, 5, 0}, "piers": {13, 1000, 0}} {
		t.Run(name, func(t *testing.T) {
			b, _ := newBackend(t, fakekodi.Options{Version: v}, BackendOptions{})

			for i, ref := range refs {
				it, err := b.Item(t.Context(), ref)
				require.NoError(t, err)
				assert.Equal(t, ref, it.Ref())
				assert.Equal(t, want[i], it.Title)

				_, err = b.Item(t.Context(), media.Ref{Kind: ref.Kind, ID: 9999})
				require.ErrorIs(t, err, media.ErrNotFound, "%s", ref.Kind)
			}

			playlists, err := b.Library(t.Context(), media.KindPlaylist)
			require.NoError(t, err)

			it, err := b.Item(t.Context(), playlists[1].Ref())
			require.NoError(t, err)
			assert.Equal(t, "Road Trip", it.Title)
		})
	}
}

func TestSearch(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	items, err := b.Search(t.Context(), media.KindMovie, "dune")
	require.NoError(t, err)
	assert.Len(t, items, 3)
	assert.Contains(t, lastParams(t, s, "VideoLibrary.GetMovies"), `"filter":{"operator":"contains","value":"dune","field":"title"}`)

	items, err = b.Search(t.Context(), media.KindArtist, "beat")
	require.NoError(t, err)
	assert.Equal(t, []string{"The Beatles"}, titles(items))

	items, err = b.Search(t.Context(), media.KindPlaylist, "road")
	require.NoError(t, err)
	assert.Equal(t, []string{"Road Trip"}, titles(items))
}

func TestPlay(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	cases := []struct {
		ref    media.Ref
		opts   media.PlayOptions
		params string
	}{
		{media.Ref{Kind: media.KindMovie, ID: 3}, media.PlayOptions{Resume: true}, `{"item":{"movieid":3},"options":{"resume":true}}`},
		{media.Ref{Kind: media.KindMovie, ID: 3}, media.PlayOptions{}, `{"item":{"movieid":3},"options":{"resume":false}}`},
		{media.Ref{Kind: media.KindEpisode, ID: 102}, media.PlayOptions{}, `{"item":{"episodeid":102},"options":{"resume":false}}`},
		{media.Ref{Kind: media.KindMusicVideo, ID: 1}, media.PlayOptions{}, `{"item":{"musicvideoid":1},"options":{"resume":false}}`},
		{media.Ref{Kind: media.KindSong, ID: 2}, media.PlayOptions{}, `{"item":{"songid":2}}`},
		{media.Ref{Kind: media.KindAlbum, ID: 1}, media.PlayOptions{}, `{"item":{"albumid":1}}`},
		{media.Ref{Kind: media.KindArtist, ID: 1}, media.PlayOptions{}, `{"item":{"artistid":1}}`},
		{media.Ref{Kind: media.KindPlaylist, ID: playlistID("special://profile/playlists/video/Kids.xsp")}, media.PlayOptions{}, `{"item":{"file":"special://profile/playlists/video/Kids.xsp"}}`},
		{media.Ref{Kind: media.KindTVShow, ID: 1}, media.PlayOptions{Resume: true}, `{"item":{"episodeid":102},"options":{"resume":true}}`},
	}

	for _, tc := range cases {
		require.NoError(t, b.Play(ctx, tc.ref, tc.opts), "%v", tc.ref)
		assert.JSONEq(t, tc.params, lastParams(t, s, "Player.Open"), "%v", tc.ref)
	}

	np, err := b.NowPlaying(ctx)
	require.NoError(t, err)
	require.NotNil(t, np)
	assert.Equal(t, media.Ref{Kind: media.KindEpisode, ID: 102}, np.Item.Ref())
	assert.Equal(t, "Severance", np.Item.ShowTitle)
}

func TestPlayResumePosition(t *testing.T) {
	b, _ := newBackend(t, fakekodi.Options{}, BackendOptions{})

	require.NoError(t, b.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 3}, media.PlayOptions{Resume: true}))

	np, err := b.NowPlaying(t.Context())
	require.NoError(t, err)
	require.NotNil(t, np)

	assert.Equal(t, media.KindMovie, np.Item.Kind)
	assert.Equal(t, "Dune: Part Two", np.Item.Title)
	assert.Equal(t, 3605500*time.Millisecond, np.Position)
	assert.Equal(t, 9960*time.Second, np.Duration)
	assert.Equal(t, 9960*time.Second-3605500*time.Millisecond, np.Remaining())
	assert.False(t, np.Paused)
}

func TestPlayShuffledShow(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	require.NoError(t, b.Play(t.Context(), media.Ref{Kind: media.KindTVShow, ID: 1}, media.PlayOptions{Shuffle: true}))

	assert.JSONEq(t, `{"playlistid":1}`, lastParams(t, s, "Playlist.Clear"))
	assert.JSONEq(t, `{"playlistid":1,"item":[{"episodeid":103},{"episodeid":102},{"episodeid":101}]}`, lastParams(t, s, "Playlist.Add"))
	assert.JSONEq(t, `{"item":{"playlistid":1}}`, lastParams(t, s, "Player.Open"), "encore shuffles; Kodi's own shuffle ignores long playlists")
}

func TestPlayMissing(t *testing.T) {
	for name, v := range map[string][3]int{"omega": {13, 5, 0}, "piers": {13, 1000, 0}} {
		t.Run(name, func(t *testing.T) {
			b, _ := newBackend(t, fakekodi.Options{Version: v}, BackendOptions{})

			err := b.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 404}, media.PlayOptions{})
			require.ErrorIs(t, err, media.ErrNotFound)

			err = b.Play(t.Context(), media.Ref{Kind: media.KindPlaylist, ID: 404}, media.PlayOptions{})
			require.ErrorIs(t, err, media.ErrNotFound)
		})
	}
}

func TestTransport(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	require.ErrorIs(t, b.Transport(ctx, media.TransportPause), media.ErrNothingPlaying)

	np, err := b.NowPlaying(ctx)
	require.NoError(t, err)
	assert.Nil(t, np)

	require.NoError(t, b.Play(ctx, media.Ref{Kind: media.KindAlbum, ID: 1}, media.PlayOptions{}))

	require.NoError(t, b.Transport(ctx, media.TransportPause))
	assert.JSONEq(t, `{"playerid":0,"play":false}`, lastParams(t, s, "Player.PlayPause"))

	np, err = b.NowPlaying(ctx)
	require.NoError(t, err)
	assert.True(t, np.Paused)

	require.NoError(t, b.Transport(ctx, media.TransportPause))
	assert.Len(t, s.CallsTo("Player.PlayPause"), 1, "pausing while paused is a no-op")

	require.NoError(t, b.Transport(ctx, media.TransportResume))
	assert.JSONEq(t, `{"playerid":0,"play":true}`, lastParams(t, s, "Player.PlayPause"))

	require.NoError(t, b.Transport(ctx, media.TransportNext))
	assert.JSONEq(t, `{"playerid":0,"to":"next"}`, lastParams(t, s, "Player.GoTo"))

	np, err = b.NowPlaying(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Bohemian Rhapsody", np.Item.Title, "album plays in track order")
	assert.Equal(t, media.KindSong, np.Item.Kind)

	require.NoError(t, b.Transport(ctx, media.TransportPrevious))
	assert.JSONEq(t, `{"playerid":0,"to":"previous"}`, lastParams(t, s, "Player.GoTo"))

	s.SetPosition(90 * time.Second)
	require.NoError(t, b.Transport(ctx, media.TransportStartOver))
	assert.JSONEq(t, `{"playerid":0,"value":{"time":{}}}`, lastParams(t, s, "Player.Seek"))

	np, err = b.NowPlaying(ctx)
	require.NoError(t, err)
	assert.Zero(t, np.Position)

	require.NoError(t, b.Transport(ctx, media.TransportStop))

	np, err = b.NowPlaying(ctx)
	require.NoError(t, err)
	assert.Nil(t, np)
}

func TestSeek(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	require.ErrorIs(t, b.Seek(ctx, media.Seek{By: time.Minute}), media.ErrNothingPlaying)
	require.NoError(t, b.Play(ctx, media.Ref{Kind: media.KindMovie, ID: 1}, media.PlayOptions{}))

	to := time.Hour + 2*time.Minute + 3*time.Second + 400*time.Millisecond

	cases := []struct {
		seek   media.Seek
		params string
		want   time.Duration
	}{
		{media.Seek{By: 90 * time.Second}, `{"seconds":90}`, 90 * time.Second},
		{media.Seek{By: -30 * time.Second}, `{"seconds":-30}`, 60 * time.Second},
		{media.Seek{To: &to}, `{"time":{"hours":1,"minutes":2,"seconds":3,"milliseconds":400}}`, to},
		{media.Seek{Step: media.SeekBigBackward}, `{"step":"bigbackward"}`, to - 10*time.Minute},
		{media.Seek{Step: media.SeekSmallForward}, `{"step":"smallforward"}`, to - 10*time.Minute + 30*time.Second},
	}

	for _, tc := range cases {
		require.NoError(t, b.Seek(ctx, tc.seek))
		assert.JSONEq(t, `{"playerid":1,"value":`+tc.params+`}`, lastParams(t, s, "Player.Seek"))

		np, err := b.NowPlaying(ctx)
		require.NoError(t, err)
		assert.Equal(t, tc.want, np.Position)
	}

	require.Error(t, b.Seek(ctx, media.Seek{Step: "sideways"}))

	before := -5 * time.Second
	require.NoError(t, b.Seek(ctx, media.Seek{To: &before}))
	assert.JSONEq(t, `{"playerid":1,"value":{"time":{}}}`, lastParams(t, s, "Player.Seek"), "a position before the start seeks to the start")
}

func TestVolume(t *testing.T) {
	b, _ := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	set := func(n int) *int { return &n }
	mute := func(m bool) *bool { return &m }

	steps := []struct {
		change media.VolumeChange
		want   media.VolumeState
	}{
		{media.VolumeChange{Set: set(30)}, media.VolumeState{Level: 30}},
		{media.VolumeChange{Step: 15}, media.VolumeState{Level: 45}},
		{media.VolumeChange{Step: -50}, media.VolumeState{Level: 0}},
		{media.VolumeChange{Set: set(140)}, media.VolumeState{Level: 100}},
		{media.VolumeChange{Mute: mute(true)}, media.VolumeState{Level: 100, Muted: true}},
		{media.VolumeChange{ToggleMute: true}, media.VolumeState{Level: 100}},
		{media.VolumeChange{}, media.VolumeState{Level: 100}},
	}

	for _, st := range steps {
		got, err := b.Volume(ctx, st.change)
		require.NoError(t, err)
		assert.Equal(t, st.want, got, "%+v", st.change)
	}
}

func TestVolumeSendsOnlyLevelsKodiAccepts(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()
	level := func(n int) *int { return &n }

	for _, tc := range []struct {
		name   string
		change media.VolumeChange
		sent   string
	}{
		{"a step below zero", media.VolumeChange{Step: -150}, `{"volume":0}`},
		{"a negative level", media.VolumeChange{Set: level(-5)}, `{"volume":0}`},
		{"a step past the top", media.VolumeChange{Step: 150}, `{"volume":100}`},
		{"a level past the top", media.VolumeChange{Set: level(101)}, `{"volume":100}`},
	} {
		_, err := b.Volume(ctx, tc.change)
		require.NoError(t, err, tc.name)
		assert.JSONEq(t, tc.sent, lastParams(t, s, "Application.SetVolume"), tc.name)
	}
}

func collectEvents(t *testing.T, b *Backend) <-chan media.Event {
	t.Helper()

	events := make(chan media.Event, 64)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = b.Subscribe(ctx, func(ev media.Event) { events <- ev })
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return events
}

// heard returns the event the backend reported once everything has settled, failing if there is none.
func heard(t *testing.T, events <-chan media.Event) media.Event {
	t.Helper()
	synctest.Wait()

	select {
	case ev := <-events:
		return ev
	default:
		t.Fatal("no event")

		return media.Event{}
	}
}

func TestSubscribe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
		events := collectEvents(t, b)

		require.Equal(t, media.Event{Kind: media.EventConnected}, heard(t, events))

		s.Notify("VideoLibrary.OnUpdate", map[string]any{"id": 7, "type": "movie", "added": true})
		assert.Equal(t, media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMovie, ID: 7}}, heard(t, events))

		s.Notify("GUI.OnScreensaverActivated", map[string]any{})
		s.Notify("VideoLibrary.OnRemove", map[string]any{"id": 103, "type": "episode"})
		assert.Equal(t, media.Event{Kind: media.EventItemRemoved, Ref: media.Ref{Kind: media.KindEpisode, ID: 103}}, heard(t, events),
			"notifications encore doesn't use are dropped")

		s.Notify("AudioLibrary.OnUpdate", map[string]any{"id": 2, "type": "song"})
		assert.Equal(t, media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindSong, ID: 2}}, heard(t, events))

		s.Notify("VideoLibrary.OnScanFinished", nil)
		assert.Equal(t, media.Event{Kind: media.EventLibraryScanned}, heard(t, events))

		s.Notify("AudioLibrary.OnCleanFinished", nil)
		assert.Equal(t, media.Event{Kind: media.EventLibraryScanned}, heard(t, events))

		require.NoError(t, b.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 6}, media.PlayOptions{}))
		assert.Equal(t, media.Event{Kind: media.EventPlaybackStarted, Ref: media.Ref{Kind: media.KindMovie, ID: 6}}, heard(t, events))

		require.NoError(t, b.Transport(t.Context(), media.TransportStop))
		assert.Equal(t, media.Event{Kind: media.EventPlaybackStopped, Ref: media.Ref{Kind: media.KindMovie, ID: 6}}, heard(t, events))

		s.Notify("System.OnQuit", map[string]any{"exitcode": 0})
		assert.Equal(t, media.Event{Kind: media.EventDisconnected}, heard(t, events))

		s.Notify("System.OnWake", nil)
		assert.Equal(t, media.Event{Kind: media.EventConnected}, heard(t, events))

		s.DropConnections()
		assert.Equal(t, media.Event{Kind: media.EventDisconnected}, heard(t, events))
		assert.Empty(t, events, "nothing more until the transport reconnects")

		time.Sleep(b.Client().Transport().cfg.ReconnectMin)
		assert.Equal(t, media.Event{Kind: media.EventConnected}, heard(t, events), "reconnects and says so")
	})
}

func TestTranslate(t *testing.T) {
	cases := []struct {
		method string
		params string
		want   media.Event
		ok     bool
	}{
		{"VideoLibrary.OnUpdate", `{"data":{"id":4,"type":"tvshow"}}`, media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindTVShow, ID: 4}}, true},
		{"VideoLibrary.OnUpdate", `{"data":{"item":{"id":9,"type":"musicvideo"},"playcount":1}}`, media.Event{Kind: media.EventItemUpdated, Ref: media.Ref{Kind: media.KindMusicVideo, ID: 9}}, true},
		{"VideoLibrary.OnUpdate", `{"data":{"id":2,"type":"set"}}`, media.Event{}, false},
		{"VideoLibrary.OnUpdate", `not json`, media.Event{}, false},
		{"VideoLibrary.OnUpdate", `{"data":{"id":0,"type":"movie"}}`, media.Event{}, false},
		{"VideoLibrary.OnRemove", `{"data":{"item":{"type":"episode"}}}`, media.Event{}, false},
		{"VideoLibrary.OnScanFinished", `{}`, media.Event{Kind: media.EventLibraryScanned}, true},
		{"Player.OnPlay", `{"data":{"item":{"type":"movie","id":0}}}`, media.Event{Kind: media.EventPlaybackStarted}, true},
		{"Player.OnPlay", `{"data":{"item":{"type":"episode","id":7}}}`, media.Event{Kind: media.EventPlaybackStarted, Ref: media.Ref{Kind: media.KindEpisode, ID: 7}}, true},
		{"System.OnWake", `{}`, media.Event{Kind: media.EventConnected}, true},
		{"AudioLibrary.OnRemove", `{"data":{"id":5,"type":"album"}}`, media.Event{Kind: media.EventItemRemoved, Ref: media.Ref{Kind: media.KindAlbum, ID: 5}}, true},
		{"Player.OnPlay", `{"data":{"item":{"type":"channel","id":3},"player":{"playerid":1}}}`, media.Event{Kind: media.EventPlaybackStarted}, true},
		{"Player.OnStop", `{"data":{"end":true}}`, media.Event{Kind: media.EventPlaybackStopped}, true},
		{"System.OnRestart", `{}`, media.Event{Kind: media.EventDisconnected}, true},
		{"Application.OnVolumeChanged", `{"data":{"volume":3}}`, media.Event{}, false},
	}

	for _, tc := range cases {
		got, ok := translate(Notification{Method: kodirpc.NotificationName(tc.method), Params: json.RawMessage(tc.params)})
		assert.Equal(t, tc.ok, ok, "%s %s", tc.method, tc.params)
		assert.Equal(t, tc.want, got, "%s %s", tc.method, tc.params)
	}
}

func TestNowPlayingArtAndImage(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "s3cret"})
	cfg := testConfig(t, s)
	cfg.Username, cfg.Password = "kodi", "s3cret"
	b := NewBackend(NewClient(cfg, nil), BackendOptions{})
	ctx := t.Context()

	require.NoError(t, b.Play(ctx, media.Ref{Kind: media.KindMovie, ID: 1}, media.PlayOptions{}))

	np, err := b.NowPlaying(ctx)
	require.NoError(t, err)
	require.NotNil(t, np)
	assert.Equal(t, "image://fake/movie-1-poster.png/", np.Item.Art.Cover())

	body, contentType, err := b.Client().Image(ctx, np.Item.Art.Cover())
	require.NoError(t, err, "fetched with Kodi's credentials")
	defer body.Close()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "image/png", contentType)
	assert.True(t, bytes.HasPrefix(data, []byte("\x89PNG")))

	_, _, err = b.Client().Image(ctx, "http://192.0.2.1/secret")
	require.ErrorIs(t, err, ErrNotImage, "only image:// references are fetched")
}

func TestActivePlayerPrefersVideoThenAudio(t *testing.T) {
	for _, tc := range []struct {
		name    string
		players string
		want    string
	}{
		{"video first", `[{"playerid":2,"type":"picture"},{"playerid":0,"type":"audio"},{"playerid":1,"type":"video"}]`, `{"playerid":1,"shuffle":true}`},
		{"then audio", `[{"playerid":2,"type":"picture"},{"playerid":0,"type":"audio"}]`, `{"playerid":0,"shuffle":true}`},
		{"then anything", `[{"playerid":2,"type":"picture"}]`, `{"playerid":2,"shuffle":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
			s.Handle("Player.GetActivePlayers", func(json.RawMessage) (any, *fakekodi.Error) { return json.RawMessage(tc.players), nil })
			s.Handle("Player.SetShuffle", func(json.RawMessage) (any, *fakekodi.Error) { return "OK", nil })

			require.NoError(t, b.SetShuffle(t.Context(), true))
			assert.JSONEq(t, tc.want, lastParams(t, s, "Player.SetShuffle"))
		})
	}
}

func TestNowPlayingNothing(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	np, err := b.NowPlaying(t.Context())

	require.NoError(t, err)
	assert.Nil(t, np, "nothing playing is not an error")
	assert.Empty(t, s.CallsTo("Player.GetItem"))
}

func TestNowPlayingOutsideTheLibrary(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	playing(t, b)

	s.Handle("Player.GetItem", func(json.RawMessage) (any, *fakekodi.Error) {
		return json.RawMessage(`{"item":{"type":"channel","id":42,"label":"ABC News","duration":0,"runtime":1800}}`), nil
	})
	s.Handle("Player.GetProperties", func(json.RawMessage) (any, *fakekodi.Error) {
		return json.RawMessage(`{"speed":1,"time":{"hours":0,"minutes":5,"seconds":0,"milliseconds":0}}`), nil
	})

	np, err := b.NowPlaying(t.Context())
	require.NoError(t, err)
	require.NotNil(t, np)

	assert.Equal(t, media.Item{Title: "ABC News", Runtime: 30 * time.Minute}, np.Item, "no kind and no ID for something outside the library")
	assert.Equal(t, 5*time.Minute, np.Position)
	assert.Equal(t, 30*time.Minute, np.Duration, "without a total time the runtime is the duration")
	assert.False(t, np.Paused)
}

func TestNowPlayingKinds(t *testing.T) {
	for typ, kind := range map[string]media.Kind{"movie": media.KindMovie, "episode": media.KindEpisode, "musicvideo": media.KindMusicVideo, "song": media.KindSong} {
		m := mapper{zone: newZone(aest)}

		it := m.playing(&kodirpc.ListItemAll{ListItemBase: kodirpc.ListItemBase{ID: 7, Type: kodirpc.ListItemBaseType(typ), Title: "T"}})

		assert.Equal(t, kind, it.Kind, typ)
		assert.Equal(t, 7, it.ID, typ)
	}
}

func TestParseTime(t *testing.T) {
	m := mapper{zone: newZone(aest)}

	assert.Equal(t, at("2024-06-01 10:00:00"), m.parseTime("2024-06-01 10:00:00"))
	assert.True(t, m.parseTime("").IsZero())
	assert.True(t, m.parseTime("yesterday").IsZero(), "a malformed date is no date")
	assert.True(t, m.parseTime("2024-06-01").IsZero())
	assert.Zero(t, globalDuration(nil))
}

func TestSearchEachKind(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	for _, tc := range []struct {
		kind   media.Kind
		title  string
		method string
		field  string
		want   []string
	}{
		{media.KindTVShow, "office", "VideoLibrary.GetTVShows", "title", []string{"The Office (US)", "The Office (UK)"}},
		{media.KindMusicVideo, "rhapsody", "VideoLibrary.GetMusicVideos", "title", []string{"Bohemian Rhapsody"}},
		{media.KindAlbum, "abbey", "AudioLibrary.GetAlbums", "album", []string{"Abbey Road"}},
		{media.KindSong, "love", "AudioLibrary.GetSongs", "title", []string{"Love of My Life"}},
		{media.KindSong, "zzz", "AudioLibrary.GetSongs", "title", []string{}},
	} {
		items, err := b.Search(t.Context(), tc.kind, tc.title)
		require.NoError(t, err, tc.kind)
		assert.ElementsMatch(t, tc.want, titles(items), tc.kind)
		assert.Contains(t, lastParams(t, s, tc.method), `"filter":{"operator":"contains","value":"`+tc.title+`","field":"`+tc.field+`"}`, tc.kind)
	}
}

func TestPlaylistsSkipFoldersAndUnreachableKodi(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	s.Handle("Files.GetDirectory", func(params json.RawMessage) (any, *fakekodi.Error) {
		if bytes.Contains(params, []byte("Mix.m3u")) {
			return json.RawMessage(`{"files":[{"file":"/m/1.flac","filetype":"file","label":"1"},{"file":"/m/sub/","filetype":"directory","label":"sub"},{"file":"","filetype":"file","label":"stream"}]}`), nil
		}

		if bytes.Contains(params, []byte("music")) {
			return json.RawMessage(`{"files":[{"file":"special://profile/playlists/music/Mix.m3u","filetype":"file","label":""},{"file":"special://profile/playlists/music/Old/","filetype":"directory","label":"Old"}]}`), nil
		}

		return nil, &fakekodi.Error{Code: CodeInvalidParams, Message: "no such folder"}
	})

	items, err := b.Library(t.Context(), media.KindPlaylist)
	require.NoError(t, err, "a missing playlist folder means no playlists there")
	require.Len(t, items, 1, "folders aren't playlists")
	assert.Equal(t, "special://profile/playlists/music/Mix.m3u", items[0].Title, "the path stands in for a missing label")

	require.NoError(t, b.Play(t.Context(), items[0].Ref(), media.PlayOptions{Shuffle: true}))
	assert.JSONEq(t, `{"playlistid":0,"item":[{"file":"/m/1.flac"}]}`, lastParams(t, s, "Playlist.Add"), "only playable files are queued")

	_, err = b.Item(t.Context(), media.Ref{Kind: media.KindPlaylist, ID: 12345})
	require.ErrorIs(t, err, media.ErrNotFound)

	s.Close()

	_, err = b.Library(t.Context(), media.KindPlaylist)
	require.ErrorIs(t, err, media.ErrUnreachable, "an unreachable Kodi isn't an empty one")
}

func TestShuffleEmptyContainer(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	s.Update(func(l *fakekodi.Library) { l.Songs = nil })

	err := b.Play(t.Context(), media.Ref{Kind: media.KindArtist, ID: 1}, media.PlayOptions{Shuffle: true})

	require.ErrorIs(t, err, media.ErrNotFound)
	assert.Empty(t, s.CallsTo("Playlist.Clear"), "the queue is left alone")
}

func TestZoneIsLearnedFromKodi(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels func(now time.Time) map[string]string
		offset int
	}{
		{"date and time", func(now time.Time) map[string]string {
			local := now.In(time.FixedZone("", 11*3600))

			return map[string]string{clockLabel: local.Format("03:04:05 PM"), dateLabel: local.Format(time.DateOnly)}
		}, 11 * 3600},
		{"time only", func(now time.Time) map[string]string {
			return map[string]string{clockLabel: now.In(time.FixedZone("", 10*3600)).Format("15:04:05"), dateLabel: ""}
		}, 10 * 3600},
		{"unreadable clock", func(time.Time) map[string]string { return map[string]string{clockLabel: "soon"} }, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fakekodi.New(t, fakekodi.Options{})
			b := NewBackend(NewClient(testConfig(t, s), nil), BackendOptions{})
			now := time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)
			b.m.zone.now = func() time.Time { return now }
			s.Handle(kodirpc.MethodXBMCGetInfoLabels, func(json.RawMessage) (any, *fakekodi.Error) { return tc.labels(now), nil })

			movies, err := b.Library(t.Context(), media.KindMovie)
			require.NoError(t, err)

			_, offset := movies[0].DateAdded.Zone()

			if tc.offset < 0 {
				_, local := time.Date(2019, 3, 2, 10, 0, 0, 0, time.Local).Zone()
				assert.Equal(t, local, offset, "the server's own zone stays the guess")

				return
			}

			assert.Equal(t, tc.offset, offset, "dates are read in Kodi's zone")
			assert.Equal(t, 10, movies[0].DateAdded.Hour())

			_, err = b.Library(t.Context(), media.KindMovie)
			require.NoError(t, err)
			assert.Len(t, s.CallsTo(kodirpc.MethodXBMCGetInfoLabels), 1, "the zone is learned once, not on every read")
		})
	}
}
