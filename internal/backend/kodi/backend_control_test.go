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

	clears := len(s.CallsTo("Playlist.Clear"))
	require.ErrorIs(t, b.Queue(ctx, nil), media.ErrNotFound)
	assert.Len(t, s.CallsTo("Playlist.Clear"), clears, "nothing to queue leaves the playlist as it was")
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

	testCases := []struct {
		name     string
		have     media.PictureCommand
		method   string
		expected string
	}{
		{name: "ShouldZoomIn", have: media.PictureZoomIn, method: "Player.Zoom", expected: `{"playerid":1,"zoom":"in"}`},
		{name: "ShouldZoomOut", have: media.PictureZoomOut, method: "Player.Zoom", expected: `{"playerid":1,"zoom":"out"}`},
		{name: "ShouldResetZoom", have: media.PictureZoomReset, method: "Player.Zoom", expected: `{"playerid":1,"zoom":1}`},
		{name: "ShouldRotateClockwise", have: media.PictureRotateClockwise, method: "Player.Rotate", expected: `{"playerid":1,"value":"clockwise"}`},
		{name: "ShouldRotateCounterClockwise", have: media.PictureRotateCounterClockwise, method: "Player.Rotate", expected: `{"playerid":1,"value":"counterclockwise"}`},
		{name: "ShouldMoveUp", have: media.PictureMoveUp, method: "Player.Move", expected: `{"playerid":1,"direction":"up"}`},
		{name: "ShouldMoveDown", have: media.PictureMoveDown, method: "Player.Move", expected: `{"playerid":1,"direction":"down"}`},
		{name: "ShouldMoveLeft", have: media.PictureMoveLeft, method: "Player.Move", expected: `{"playerid":1,"direction":"left"}`},
		{name: "ShouldMoveRight", have: media.PictureMoveRight, method: "Player.Move", expected: `{"playerid":1,"direction":"right"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, b.Picture(t.Context(), tc.have))
			assert.JSONEq(t, tc.expected, lastParams(t, s, tc.method))
		})
	}

	require.Error(t, b.Picture(t.Context(), "spin"))
}

func TestStreams(t *testing.T) {
	testCases := []struct {
		name    string
		version [3]int
		english string
	}{
		{name: "ShouldReportISO6392OnOmega", version: [3]int{13, 5, 0}, english: "eng"},
		{name: "ShouldReportBCP47OnPiers", version: [3]int{13, 1000, 0}, english: "en"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

	testCases := []struct {
		name     string
		set      func() error
		method   string
		expected string
	}{
		{name: "ShouldTurnSubtitlesOn", set: func() error { return b.SetSubtitle(ctx, media.StreamChange{Step: media.StreamOn}) }, method: "Player.SetSubtitle", expected: `{"playerid":1,"subtitle":"on"}`},
		{name: "ShouldTurnSubtitlesOff", set: func() error { return b.SetSubtitle(ctx, media.StreamChange{Step: media.StreamOff}) }, method: "Player.SetSubtitle", expected: `{"playerid":1,"subtitle":"off"}`},
		{name: "ShouldEnableNextSubtitle", set: func() error { return b.SetSubtitle(ctx, media.StreamChange{Step: media.StreamNext}) }, method: "Player.SetSubtitle", expected: `{"playerid":1,"subtitle":"next","enable":true}`},
		{name: "ShouldEnableSubtitleByIndex", set: func() error { return b.SetSubtitle(ctx, media.StreamChange{Index: 1}) }, method: "Player.SetSubtitle", expected: `{"playerid":1,"subtitle":1,"enable":true}`},
		{name: "ShouldSelectPreviousAudioStream", set: func() error { return b.SetAudioStream(ctx, media.StreamChange{Step: media.StreamPrevious}) }, method: "Player.SetAudioStream", expected: `{"playerid":1,"stream":"previous"}`},
		{name: "ShouldSelectAudioStreamByIndex", set: func() error { return b.SetAudioStream(ctx, media.StreamChange{Index: 2}) }, method: "Player.SetAudioStream", expected: `{"playerid":1,"stream":2}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.set())
			assert.JSONEq(t, tc.expected, lastParams(t, s, tc.method))
		})
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

	testCases := []struct {
		name     string
		have     media.InputAction
		method   string
		expected string
	}{
		{name: "ShouldSendUp", have: media.InputUp, method: "Input.Up"},
		{name: "ShouldSendDown", have: media.InputDown, method: "Input.Down"},
		{name: "ShouldSendLeft", have: media.InputLeft, method: "Input.Left"},
		{name: "ShouldSendRight", have: media.InputRight, method: "Input.Right"},
		{name: "ShouldSendSelect", have: media.InputSelect, method: "Input.Select"},
		{name: "ShouldSendBack", have: media.InputBack, method: "Input.Back"},
		{name: "ShouldSendHome", have: media.InputHome, method: "Input.Home"},
		{name: "ShouldSendInfo", have: media.InputInfo, method: "Input.Info"},
		{name: "ShouldSendContextMenu", have: media.InputContextMenu, method: "Input.ContextMenu"},
		{name: "ShouldShowOSD", have: media.InputOSD, method: "Input.ShowOSD"},
		{name: "ShouldToggleFullscreen", have: media.InputFullscreen, method: "GUI.SetFullscreen", expected: `{"fullscreen":"toggle"}`},
		{name: "ShouldPageUp", have: media.InputPageUp, method: "Input.ExecuteAction", expected: `{"action":"pageup"}`},
		{name: "ShouldPageDown", have: media.InputPageDown, method: "Input.ExecuteAction", expected: `{"action":"pagedown"}`},
		{name: "ShouldToggleStereoMode", have: media.InputStereoscopic, method: "Input.ExecuteAction", expected: `{"action":"togglestereomode"}`},
		{name: "ShouldTogglePassthrough", have: media.InputPassthrough, method: "Input.ExecuteAction", expected: `{"action":"audiotoggledigital"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, b.Input(t.Context(), tc.have))

			if tc.expected != "" {
				assert.JSONEq(t, tc.expected, lastParams(t, s, tc.method))
			} else {
				assert.NotEmpty(t, s.CallsTo(tc.method))
			}
		})
	}

	require.Error(t, b.Input(t.Context(), "jump"))

	require.NoError(t, b.SendText(t.Context(), "the expanse"))
	assert.JSONEq(t, `{"text":"the expanse","done":false}`, lastParams(t, s, "Input.SendText"))
}

func TestShowView(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	testCases := []struct {
		name     string
		have     media.View
		expected string
	}{
		{name: "ShouldOpenMovies", have: media.View{Section: media.SectionMovies}, expected: `{"window":"videos","parameters":["videodb://movies/titles/","return"]}`},
		{name: "ShouldOpenAlbums", have: media.View{Section: media.SectionAlbums}, expected: `{"window":"music","parameters":["musicdb://albums/","return"]}`},
		{name: "ShouldOpenRecentEpisodes", have: media.View{Section: media.SectionRecentEpisodes}, expected: `{"window":"videos","parameters":["videodb://recentlyaddedepisodes/","return"]}`},
		{name: "ShouldOpenRecentMusic", have: media.View{Section: media.SectionRecentMusic}, expected: `{"window":"music","parameters":["musicdb://recentlyaddedalbums/","return"]}`},
		{name: "ShouldOpenMovieGenre", have: media.View{Section: media.SectionMovies, Genre: "comedy"}, expected: `{"window":"videos","parameters":["videodb://movies/genres/1/","return"]}`},
		{name: "ShouldOpenTVShowGenre", have: media.View{Section: media.SectionTVShows, Genre: "Mystery"}, expected: `{"window":"videos","parameters":["videodb://tvshows/genres/5/","return"]}`},
		{name: "ShouldOpenMusicGenre", have: media.View{Section: media.SectionMusic, Genre: "Rock"}, expected: `{"window":"music","parameters":["musicdb://genres/1/","return"]}`},
		{name: "ShouldOpenFavourites", have: media.View{Section: media.SectionFavourites}, expected: `{"window":"favouritesbrowser"}`},
		{name: "ShouldOpenSubtitleSearch", have: media.View{Section: media.SectionSubtitleSearch}, expected: `{"window":"subtitlesearch"}`},
		{name: "ShouldFilterMoviesByTitle", have: media.View{Section: media.SectionMovies, Title: "the matrix"}, expected: `{"window":"videos","parameters":["videodb://movies/titles/?xsp=%7B%22rules%22%3A%7B%22and%22%3A%5B%7B%22field%22%3A%22title%22%2C%22operator%22%3A%22contains%22%2C%22value%22%3A%5B%22the%20matrix%22%5D%7D%5D%7D%2C%22type%22%3A%22movies%22%7D","return"]}`},
		{name: "ShouldFilterTVShowsByTitle", have: media.View{Section: media.SectionTVShows, Title: "star"}, expected: `{"window":"videos","parameters":["videodb://tvshows/titles/?xsp=%7B%22rules%22%3A%7B%22and%22%3A%5B%7B%22field%22%3A%22title%22%2C%22operator%22%3A%22contains%22%2C%22value%22%3A%5B%22star%22%5D%7D%5D%7D%2C%22type%22%3A%22tvshows%22%7D","return"]}`},
		{name: "ShouldFilterArtistsByName", have: media.View{Section: media.SectionArtists, Title: "daft & punk"}, expected: `{"window":"music","parameters":["musicdb://artists/?xsp=%7B%22rules%22%3A%7B%22and%22%3A%5B%7B%22field%22%3A%22artist%22%2C%22operator%22%3A%22contains%22%2C%22value%22%3A%5B%22daft%20%26%20punk%22%5D%7D%5D%7D%2C%22type%22%3A%22artists%22%7D","return"]}`},
		{name: "ShouldFilterAlbumsByName", have: media.View{Section: media.SectionAlbums, Title: "discovery"}, expected: `{"window":"music","parameters":["musicdb://albums/?xsp=%7B%22rules%22%3A%7B%22and%22%3A%5B%7B%22field%22%3A%22album%22%2C%22operator%22%3A%22contains%22%2C%22value%22%3A%5B%22discovery%22%5D%7D%5D%7D%2C%22type%22%3A%22albums%22%7D","return"]}`},
		{name: "ShouldFilterMusicVideosByTitle", have: media.View{Section: media.SectionMusicVideos, Title: "thriller"}, expected: `{"window":"videos","parameters":["videodb://musicvideos/titles/?xsp=%7B%22rules%22%3A%7B%22and%22%3A%5B%7B%22field%22%3A%22title%22%2C%22operator%22%3A%22contains%22%2C%22value%22%3A%5B%22thriller%22%5D%7D%5D%7D%2C%22type%22%3A%22musicvideos%22%7D","return"]}`},
		{name: "ShouldOpenPlaylist", have: media.View{Section: media.SectionPlaylist, Playlist: media.Ref{Kind: media.KindPlaylist, ID: playlistID("special://profile/playlists/music/Road Trip.m3u")}}, expected: `{"window":"music","parameters":["special://profile/playlists/music/Road Trip.m3u","return"]}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, b.ShowView(ctx, tc.have))
			assert.JSONEq(t, tc.expected, lastParams(t, s, "GUI.ActivateWindow"))
		})
	}

	assert.JSONEq(t, `{"type":"tvshow"}`, lastParams(t, s, "VideoLibrary.GetGenres"))
	require.ErrorIs(t, b.ShowView(ctx, media.View{Section: media.SectionMovies, Genre: "Western"}), media.ErrNotFound)
	require.ErrorIs(t, b.ShowView(ctx, media.View{Section: media.SectionAlbums, Genre: "Rock"}), errUnsupportedKind)
	require.ErrorIs(t, b.ShowView(ctx, media.View{Section: media.SectionRecentMovies, Title: "dune"}), errUnsupportedKind)
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
	testCases := []struct {
		name     string
		have     string
		expected string
		err      bool
	}{
		{name: "ShouldParseTwelveHourPM", have: "09:42:17 PM", expected: "21:42:17"},
		{name: "ShouldParseMidnightHourAM", have: "12:05:00 AM", expected: "00:05:00"},
		{name: "ShouldParseNoonHourLowercasePM", have: "12:30:00 pm", expected: "12:30:00"},
		{name: "ShouldParseTwentyFourHour", have: "21:42:17", expected: "21:42:17"},
		{name: "ShouldParseWithoutSeconds", have: "7:05 AM", expected: "07:05:00"},
		{name: "ShouldRejectEmpty", err: true},
		{name: "ShouldRejectWord", have: "noon", err: true},
		{name: "ShouldRejectHourOutOfRange", have: "25:00:00", err: true},
		{name: "ShouldRejectMinuteOutOfRange", have: "10:75", err: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := parseClock(tc.have)

			if tc.err {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual.Format(time.TimeOnly))
		})
	}
}

func TestSystemAndLibrary(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	ctx := t.Context()

	testCases := []struct {
		name   string
		have   media.SystemCommand
		method string
	}{
		{name: "ShouldShutdown", have: media.SystemShutdown, method: "System.Shutdown"},
		{name: "ShouldReboot", have: media.SystemReboot, method: "System.Reboot"},
		{name: "ShouldHibernate", have: media.SystemHibernate, method: "System.Hibernate"},
		{name: "ShouldSuspend", have: media.SystemSuspend, method: "System.Suspend"},
		{name: "ShouldQuit", have: media.SystemQuit, method: "Application.Quit"},
		{name: "ShouldEject", have: media.SystemEject, method: "System.EjectOpticalDrive"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, b.System(ctx, tc.have))
			assert.Len(t, s.CallsTo(tc.method), 1)
		})
	}

	require.Error(t, b.System(ctx, "explode"))

	require.NoError(t, b.ScanLibrary(ctx, media.DatabaseVideo))
	require.NoError(t, b.ScanLibrary(ctx, media.DatabaseMusic))
	require.NoError(t, b.CleanLibrary(ctx, media.DatabaseVideo))
	require.NoError(t, b.CleanLibrary(ctx, media.DatabaseMusic))

	for _, m := range []string{"VideoLibrary.Scan", "AudioLibrary.Scan", "VideoLibrary.Clean", "AudioLibrary.Clean"} {
		assert.Len(t, s.CallsTo(m), 1, m)
	}

	// A video clean with dialogs blocks Kodi's reply until the clean finishes.
	assert.JSONEq(t, `{"showdialogs":false}`, string(s.CallsTo("VideoLibrary.Clean")[0].Params))

	require.Error(t, b.ScanLibrary(ctx, "pictures"))
	require.Error(t, b.CleanLibrary(ctx, "pictures"))
}
