package kodi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
)

func playing(t *testing.T, b *Backend) {
	t.Helper()
	require.NoError(t, b.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 3}, media.PlayOptions{}))
}

func TestQueue(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	require.NoError(t, b.Queue(ctx, []media.Ref{{Kind: media.KindSong, ID: 3}, {Kind: media.KindSong, ID: 1}}))
	assert.JSONEq(t, `{"playlistid":0}`, lastParams(t, s, "Playlist.Clear"))
	assert.JSONEq(t, `{"playlistid":0,"item":[{"songid":3},{"songid":1}]}`, lastParams(t, s, "Playlist.Add"))
	assert.JSONEq(t, `{"item":{"playlistid":0}}`, lastParams(t, s, "Player.Open"))

	require.NoError(t, b.Queue(ctx, []media.Ref{{Kind: media.KindEpisode, ID: 102}, {Kind: media.KindMovie, ID: 1}, {Kind: media.KindMusicVideo, ID: 1}}))
	assert.JSONEq(t, `{"playlistid":1,"item":[{"episodeid":102},{"movieid":1},{"musicvideoid":1}]}`, lastParams(t, s, "Playlist.Add"))

	require.ErrorIs(t, b.Queue(ctx, []media.Ref{{Kind: media.KindSong, ID: 1}, {Kind: media.KindMovie, ID: 1}}), errMixedQueue)
	require.ErrorIs(t, b.Queue(ctx, nil), media.ErrNotFound)
	require.ErrorIs(t, b.Queue(ctx, []media.Ref{{Kind: media.KindAlbum, ID: 1}}), errUnsupportedKind)
}

func TestQueueAddsInPages(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	refs := make([]media.Ref, 250)
	for i := range refs {
		refs[i] = media.Ref{Kind: media.KindSong, ID: 1 + i%3}
	}

	require.NoError(t, b.Queue(t.Context(), refs))

	adds := s.CallsTo("Playlist.Add")
	require.Len(t, adds, 3)

	for i, want := range []int{100, 100, 50} {
		var p struct {
			Item []json.RawMessage `json:"item"`
		}
		require.NoError(t, json.Unmarshal(adds[i].Params, &p))
		assert.Len(t, p.Item, want)
	}
}

func TestPlayShuffledContainers(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	require.NoError(t, b.Play(ctx, media.Ref{Kind: media.KindArtist, ID: 1}, media.PlayOptions{Shuffle: true}))
	assert.JSONEq(t, `{"filter":{"artistid":1},"limits":{"end":1000}}`, lastParams(t, s, "AudioLibrary.GetSongs"))
	assert.JSONEq(t, `{"playlistid":0,"item":[{"songid":2},{"songid":1}]}`, lastParams(t, s, "Playlist.Add"))

	require.NoError(t, b.Play(ctx, media.Ref{Kind: media.KindAlbum, ID: 2}, media.PlayOptions{Shuffle: true}))
	assert.JSONEq(t, `{"filter":{"albumid":2},"limits":{"end":1000}}`, lastParams(t, s, "AudioLibrary.GetSongs"))
	assert.JSONEq(t, `{"playlistid":0,"item":[{"songid":3}]}`, lastParams(t, s, "Playlist.Add"))

	roadTrip := playlistID("special://profile/playlists/music/Road Trip.m3u")
	require.NoError(t, b.Play(ctx, media.Ref{Kind: media.KindPlaylist, ID: roadTrip}, media.PlayOptions{Shuffle: true, Limit: 2}))
	assert.JSONEq(t, `{"directory":"special://profile/playlists/music/Road Trip.m3u","media":"music"}`, lastParams(t, s, "Files.GetDirectory"))
	assert.JSONEq(t, `{"playlistid":0,"item":[{"file":"/media/music/Beatles/01.flac"},{"file":"/media/music/Queen/09.flac"}]}`, lastParams(t, s, "Playlist.Add"),
		"shuffled, then capped at the limit")
	assert.JSONEq(t, `{"item":{"playlistid":0}}`, lastParams(t, s, "Player.Open"))

	kids := playlistID("special://profile/playlists/video/Kids.xsp")
	require.NoError(t, b.Play(ctx, media.Ref{Kind: media.KindPlaylist, ID: kids}, media.PlayOptions{Shuffle: true}))
	assert.JSONEq(t, `{"playlistid":1,"item":[{"file":"/media/tv/Bluey/S03E12.mkv"}]}`, lastParams(t, s, "Playlist.Add"), "smart playlists expand too")

	require.ErrorIs(t, b.Play(ctx, media.Ref{Kind: media.KindArtist, ID: 99}, media.PlayOptions{Shuffle: true}), media.ErrNotFound)
}

func TestPlayFileAndPartyMode(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	require.NoError(t, b.PlayFile(t.Context(), "plugin://plugin.video.youtube/play/?video_id=x"))
	assert.JSONEq(t, `{"item":{"file":"plugin://plugin.video.youtube/play/?video_id=x"}}`, lastParams(t, s, "Player.Open"))

	require.NoError(t, b.PartyMode(t.Context()))
	assert.JSONEq(t, `{"item":{"partymode":"music"}}`, lastParams(t, s, "Player.Open"))
}

func TestShuffleAndRepeat(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	require.ErrorIs(t, b.SetShuffle(ctx, true), media.ErrNothingPlaying)

	_, err := b.SetRepeat(ctx, media.RepeatCycle)
	require.ErrorIs(t, err, media.ErrNothingPlaying)

	playing(t, b)

	require.NoError(t, b.SetShuffle(ctx, true))
	assert.JSONEq(t, `{"playerid":1,"shuffle":true}`, lastParams(t, s, "Player.SetShuffle"))

	mode, err := b.SetRepeat(ctx, media.RepeatCycle)
	require.NoError(t, err)
	assert.Equal(t, media.RepeatOne, mode)
	assert.JSONEq(t, `{"playerid":1,"repeat":"cycle"}`, lastParams(t, s, "Player.SetRepeat"))

	mode, err = b.SetRepeat(ctx, media.RepeatOff)
	require.NoError(t, err)
	assert.Equal(t, media.RepeatOff, mode)
	assert.JSONEq(t, `{"playerid":1,"repeat":"off"}`, lastParams(t, s, "Player.SetRepeat"))
}

func TestPicture(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	playing(t, b)

	cases := []struct {
		cmd    media.PictureCommand
		method string
		params string
	}{
		{media.PictureZoomIn, "Player.Zoom", `{"playerid":1,"zoom":"in"}`},
		{media.PictureZoomOut, "Player.Zoom", `{"playerid":1,"zoom":"out"}`},
		{media.PictureZoomReset, "Player.Zoom", `{"playerid":1,"zoom":1}`},
		{media.PictureRotateClockwise, "Player.Rotate", `{"playerid":1,"value":"clockwise"}`},
		{media.PictureRotateCounterClockwise, "Player.Rotate", `{"playerid":1,"value":"counterclockwise"}`},
		{media.PictureMoveUp, "Player.Move", `{"playerid":1,"direction":"up"}`},
		{media.PictureMoveLeft, "Player.Move", `{"playerid":1,"direction":"left"}`},
	}

	for _, tc := range cases {
		require.NoError(t, b.Picture(t.Context(), tc.cmd), tc.cmd)
		assert.JSONEq(t, tc.params, lastParams(t, s, tc.method), tc.cmd)
	}

	require.Error(t, b.Picture(t.Context(), "spin"))
}

func TestStreams(t *testing.T) {
	for name, tc := range map[string]struct {
		version [3]int
		english string
	}{
		"omega reports ISO 639-2": {[3]int{13, 5, 0}, "eng"},
		"piers reports BCP 47":    {[3]int{13, 1000, 0}, "en"},
	} {
		t.Run(name, func(t *testing.T) {
			b, s := newBackend(t, fakekodi.Options{Version: tc.version}, BackendOptions{})
			playing(t, b)

			st, err := b.Streams(t.Context())
			require.NoError(t, err)
			require.Len(t, st.Audio, 3)
			require.Len(t, st.Subtitles, 3)
			assert.Equal(t, media.Stream{Index: 0, Name: "English 5.1", Language: tc.english}, *st.CurrentAudio)
			assert.False(t, st.SubtitlesOn)

			var p struct {
				Properties []string `json:"properties"`
			}
			require.NoError(t, json.Unmarshal([]byte(lastParams(t, s, "Player.GetProperties")), &p))
			assert.ElementsMatch(t, []string{"currentaudiostream", "audiostreams", "subtitleenabled", "currentsubtitle", "subtitles"}, p.Properties)
		})
	}
}

func TestStreamsAudioOnly(t *testing.T) {
	b, _ := newBackend(t, fakekodi.Options{}, BackendOptions{})
	require.NoError(t, b.Play(t.Context(), media.Ref{Kind: media.KindSong, ID: 1}, media.PlayOptions{}))

	st, err := b.Streams(t.Context())
	require.NoError(t, err)
	assert.Empty(t, st.Subtitles)
	assert.Nil(t, st.CurrentSubtitle, "Kodi's empty current-subtitle object is not a stream")
}

func TestSetStreams(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()
	playing(t, b)

	cases := []struct {
		set    func() error
		method string
		params string
	}{
		{func() error { return b.SetSubtitle(ctx, media.StreamChange{Step: media.StreamOn}) }, "Player.SetSubtitle", `{"playerid":1,"subtitle":"on"}`},
		{func() error { return b.SetSubtitle(ctx, media.StreamChange{Step: media.StreamOff}) }, "Player.SetSubtitle", `{"playerid":1,"subtitle":"off"}`},
		{func() error { return b.SetSubtitle(ctx, media.StreamChange{Step: media.StreamNext}) }, "Player.SetSubtitle", `{"playerid":1,"subtitle":"next","enable":true}`},
		{func() error { return b.SetSubtitle(ctx, media.StreamChange{Index: 1}) }, "Player.SetSubtitle", `{"playerid":1,"subtitle":1,"enable":true}`},
		{func() error { return b.SetAudioStream(ctx, media.StreamChange{Step: media.StreamPrevious}) }, "Player.SetAudioStream", `{"playerid":1,"stream":"previous"}`},
		{func() error { return b.SetAudioStream(ctx, media.StreamChange{Index: 2}) }, "Player.SetAudioStream", `{"playerid":1,"stream":2}`},
	}

	for _, tc := range cases {
		require.NoError(t, tc.set())
		assert.JSONEq(t, tc.params, lastParams(t, s, tc.method))
	}

	st, err := b.Streams(ctx)
	require.NoError(t, err)
	assert.True(t, st.SubtitlesOn)
	assert.Equal(t, "Deutsch", st.CurrentSubtitle.Name)
	assert.Equal(t, "Français", st.CurrentAudio.Name)

	require.Error(t, b.SetAudioStream(ctx, media.StreamChange{Step: media.StreamOn}), "audio can't be turned off")
}

func TestInput(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	cases := []struct {
		action media.InputAction
		method string
		params string
	}{
		{media.InputUp, "Input.Up", ``},
		{media.InputDown, "Input.Down", ``},
		{media.InputLeft, "Input.Left", ``},
		{media.InputRight, "Input.Right", ``},
		{media.InputSelect, "Input.Select", ``},
		{media.InputBack, "Input.Back", ``},
		{media.InputHome, "Input.Home", ``},
		{media.InputInfo, "Input.Info", ``},
		{media.InputContextMenu, "Input.ContextMenu", ``},
		{media.InputOSD, "Input.ShowOSD", ``},
		{media.InputFullscreen, "GUI.SetFullscreen", `{"fullscreen":"toggle"}`},
		{media.InputPageUp, "Input.ExecuteAction", `{"action":"pageup"}`},
		{media.InputPageDown, "Input.ExecuteAction", `{"action":"pagedown"}`},
		{media.InputStereoscopic, "Input.ExecuteAction", `{"action":"togglestereomode"}`},
		{media.InputPassthrough, "Input.ExecuteAction", `{"action":"audiotoggledigital"}`},
	}

	for _, tc := range cases {
		require.NoError(t, b.Input(t.Context(), tc.action), tc.action)

		if tc.params != "" {
			assert.JSONEq(t, tc.params, lastParams(t, s, tc.method), tc.action)
		} else {
			assert.NotEmpty(t, s.CallsTo(tc.method), tc.action)
		}
	}

	require.Error(t, b.Input(t.Context(), "jump"))

	require.NoError(t, b.SendText(t.Context(), "the expanse"))
	assert.JSONEq(t, `{"text":"the expanse","done":false}`, lastParams(t, s, "Input.SendText"))
}

func TestShowView(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	cases := []struct {
		view   media.View
		params string
	}{
		{media.View{Section: media.SectionMovies}, `{"window":"videos","parameters":["videodb://movies/titles/","return"]}`},
		{media.View{Section: media.SectionAlbums}, `{"window":"music","parameters":["musicdb://albums/","return"]}`},
		{media.View{Section: media.SectionRecentEpisodes}, `{"window":"videos","parameters":["videodb://recentlyaddedepisodes/","return"]}`},
		{media.View{Section: media.SectionRecentMusic}, `{"window":"music","parameters":["musicdb://recentlyaddedalbums/","return"]}`},
		{media.View{Section: media.SectionMovies, Genre: "comedy"}, `{"window":"videos","parameters":["videodb://movies/genres/1/","return"]}`},
		{media.View{Section: media.SectionTVShows, Genre: "Mystery"}, `{"window":"videos","parameters":["videodb://tvshows/genres/5/","return"]}`},
		{media.View{Section: media.SectionMusic, Genre: "Rock"}, `{"window":"music","parameters":["musicdb://genres/1/","return"]}`},
		{media.View{Section: media.SectionFavourites}, `{"window":"favouritesbrowser"}`},
		{media.View{Section: media.SectionSubtitleSearch}, `{"window":"subtitlesearch"}`},
		{
			media.View{Section: media.SectionPlaylist, Playlist: media.Ref{Kind: media.KindPlaylist, ID: playlistID("special://profile/playlists/music/Road Trip.m3u")}},
			`{"window":"music","parameters":["special://profile/playlists/music/Road Trip.m3u","return"]}`,
		},
	}

	for _, tc := range cases {
		require.NoError(t, b.ShowView(ctx, tc.view), "%+v", tc.view)
		assert.JSONEq(t, tc.params, lastParams(t, s, "GUI.ActivateWindow"), "%+v", tc.view)
	}

	assert.JSONEq(t, `{"type":"tvshow"}`, lastParams(t, s, "VideoLibrary.GetGenres"))
	require.ErrorIs(t, b.ShowView(ctx, media.View{Section: media.SectionMovies, Genre: "Western"}), media.ErrNotFound)
	require.ErrorIs(t, b.ShowView(ctx, media.View{Section: media.SectionAlbums, Genre: "Rock"}), errUnsupportedKind)
}

func TestFavourites(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	favs, err := b.Favourites(ctx)
	require.NoError(t, err)
	require.Len(t, favs, 3)
	assert.Equal(t, media.Favourite{Title: "Kids Movies", Type: "window", Window: "10025", WindowParameter: "videodb://movies/genres/7/"}, favs[0])
	assert.JSONEq(t, `{"properties":["window","windowparameter","path"]}`, lastParams(t, s, "Favourites.GetFavourites"))

	require.NoError(t, b.OpenFavourite(ctx, favs[0]))
	assert.JSONEq(t, `{"window":"10025","parameters":["videodb://movies/genres/7/"]}`, lastParams(t, s, "GUI.ActivateWindow"))

	require.NoError(t, b.OpenFavourite(ctx, favs[1]))
	assert.JSONEq(t, `{"item":{"file":"http://live-radio01.mediahubaustralia.com/2RNW/mp3/"}}`, lastParams(t, s, "Player.Open"))

	require.NoError(t, b.OpenFavourite(ctx, favs[2]))
	assert.JSONEq(t, `{"addonid":"script.weather"}`, lastParams(t, s, "Addons.ExecuteAddon"))

	require.ErrorIs(t, b.OpenFavourite(ctx, media.Favourite{Title: "App", Type: "androidapp", Path: "com.netflix"}), errFavouriteType)

	s.Update(func(l *fakekodi.Library) { l.Favourites = nil })

	favs, err = b.Favourites(ctx)
	require.NoError(t, err)
	assert.Empty(t, favs)
}

func TestAddons(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	addons, err := b.Addons(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []media.Addon{
		{ID: "plugin.video.youtube", Name: "YouTube", Type: "xbmc.python.pluginsource"},
		{ID: "script.globalsearch", Name: "Global Search", Type: "xbmc.python.script"},
	}, addons, "skins and disabled add-ons can't be launched")
	assert.JSONEq(t, `{"enabled":true,"properties":["name"]}`, lastParams(t, s, "Addons.GetAddons"))

	require.NoError(t, b.ExecuteAddon(t.Context(), "script.globalsearch", map[string]string{"searchstring": "dune"}))
	assert.JSONEq(t, `{"addonid":"script.globalsearch","params":{"searchstring":"dune"}}`, lastParams(t, s, "Addons.ExecuteAddon"))
}

func TestClock(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	got, err := b.Clock(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "21:42:17", got.Format(time.TimeOnly))
	assert.JSONEq(t, `{"labels":["System.Time(hh:mm:ss xx)"]}`, lastParams(t, s, "XBMC.GetInfoLabels"))

	s.SetClock("garbage")

	_, err = b.Clock(t.Context())
	require.Error(t, err)
}

func TestParseClock(t *testing.T) {
	for in, want := range map[string]string{
		"09:42:17 PM": "21:42:17",
		"12:05:00 AM": "00:05:00",
		"12:30:00 pm": "12:30:00",
		"21:42:17":    "21:42:17",
		"7:05 AM":     "07:05:00",
	} {
		got, err := parseClock(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got.Format(time.TimeOnly), in)
	}

	for _, bad := range []string{"", "noon", "25:00:00", "10:75"} {
		_, err := parseClock(bad)
		require.Error(t, err, bad)
	}
}

func TestSystemAndLibrary(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	for cmd, method := range map[media.SystemCommand]string{
		media.SystemShutdown:  "System.Shutdown",
		media.SystemReboot:    "System.Reboot",
		media.SystemHibernate: "System.Hibernate",
		media.SystemSuspend:   "System.Suspend",
		media.SystemQuit:      "Application.Quit",
		media.SystemEject:     "System.EjectOpticalDrive",
	} {
		require.NoError(t, b.System(ctx, cmd), cmd)
		assert.Len(t, s.CallsTo(method), 1, cmd)
	}

	require.Error(t, b.System(ctx, "explode"))

	require.NoError(t, b.ScanLibrary(ctx, media.DatabaseVideo))
	require.NoError(t, b.ScanLibrary(ctx, media.DatabaseMusic))
	require.NoError(t, b.CleanLibrary(ctx, media.DatabaseVideo))
	require.NoError(t, b.CleanLibrary(ctx, media.DatabaseMusic))

	for _, m := range []string{"VideoLibrary.Scan", "AudioLibrary.Scan", "VideoLibrary.Clean", "AudioLibrary.Clean"} {
		assert.Len(t, s.CallsTo(m), 1, m)
	}

	require.Error(t, b.ScanLibrary(ctx, "pictures"))
	require.Error(t, b.CleanLibrary(ctx, "pictures"))
}
