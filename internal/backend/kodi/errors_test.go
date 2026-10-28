package kodi

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// backendOp is one Backend operation, for checking how it handles each Kodi method it calls failing.
type backendOp struct {
	name    string
	playing bool
	run     func(context.Context, *Backend) error
	// tolerated are methods whose failure the operation absorbs, still succeeding.
	tolerated []string
	// notFound are methods whose failure the operation reports as media.ErrNotFound rather than passing on.
	notFound []string
}

func backendOps() []backendOp {
	ref := func(kind media.Kind, id int) media.Ref { return media.Ref{Kind: kind, ID: id} }
	roadTrip := playlistID("special://profile/playlists/music/Road Trip.m3u")
	dirs := []string{kodirpc.MethodFilesGetDirectory}
	since := time.Date(2020, 1, 1, 0, 0, 0, 0, aest)
	seek := 90 * time.Second
	level := 40
	mute := true

	ops := []backendOp{
		{name: "info", tolerated: []string{kodirpc.MethodXBMCGetInfoLabels}, run: func(ctx context.Context, b *Backend) error { _, err := b.Info(ctx); return err }},
		{name: "playlists", tolerated: dirs, run: func(ctx context.Context, b *Backend) error { _, err := b.Library(ctx, media.KindPlaylist); return err }},
		{name: "episodes", run: func(ctx context.Context, b *Backend) error { _, err := b.Episodes(ctx, 1); return err }},
		{name: "next episode", run: func(ctx context.Context, b *Backend) error { _, err := b.NextEpisode(ctx, 1); return err }},
		{name: "recently added movies", run: func(ctx context.Context, b *Backend) error {
			_, err := b.RecentlyAdded(ctx, media.KindMovie, since)
			return err
		}},
		{name: "recently added artists", run: func(ctx context.Context, b *Backend) error {
			_, err := b.RecentlyAdded(ctx, media.KindArtist, since)
			return err
		}},
		{name: "search movies", run: func(ctx context.Context, b *Backend) error {
			_, err := b.Search(ctx, media.KindMovie, "dune")
			return err
		}},
		{name: "search playlists", tolerated: dirs, run: func(ctx context.Context, b *Backend) error {
			_, err := b.Search(ctx, media.KindPlaylist, "road")
			return err
		}},
		{name: "item playlist", notFound: dirs, run: func(ctx context.Context, b *Backend) error {
			_, err := b.Item(ctx, ref(media.KindPlaylist, roadTrip))
			return err
		}},
		{name: "play playlist", notFound: dirs, run: func(ctx context.Context, b *Backend) error {
			return b.Play(ctx, ref(media.KindPlaylist, roadTrip), media.PlayOptions{})
		}},
		{name: "play show", run: func(ctx context.Context, b *Backend) error {
			return b.Play(ctx, ref(media.KindTVShow, 1), media.PlayOptions{Resume: true})
		}},
		{name: "shuffle artist", run: func(ctx context.Context, b *Backend) error {
			return b.Play(ctx, ref(media.KindArtist, 1), media.PlayOptions{Shuffle: true})
		}},
		{name: "shuffle album", run: func(ctx context.Context, b *Backend) error {
			return b.Play(ctx, ref(media.KindAlbum, 1), media.PlayOptions{Shuffle: true})
		}},
		{name: "shuffle show", run: func(ctx context.Context, b *Backend) error {
			return b.Play(ctx, ref(media.KindTVShow, 1), media.PlayOptions{Shuffle: true})
		}},
		{name: "shuffle playlist", notFound: dirs, run: func(ctx context.Context, b *Backend) error {
			return b.Play(ctx, ref(media.KindPlaylist, roadTrip), media.PlayOptions{Shuffle: true})
		}},
		{name: "queue", run: func(ctx context.Context, b *Backend) error {
			return b.Queue(ctx, []media.Ref{ref(media.KindSong, 1), ref(media.KindSong, 2)})
		}},
		{name: "play file", run: func(ctx context.Context, b *Backend) error { return b.PlayFile(ctx, "/media/trailer.mkv") }},
		{name: "party mode", run: func(ctx context.Context, b *Backend) error { return b.PartyMode(ctx) }},
		{name: "seek to", playing: true, run: func(ctx context.Context, b *Backend) error { return b.Seek(ctx, media.Seek{To: &seek}) }},
		{name: "seek step", playing: true, run: func(ctx context.Context, b *Backend) error {
			return b.Seek(ctx, media.Seek{Step: media.SeekBigForward})
		}},
		{name: "seek by", playing: true, run: func(ctx context.Context, b *Backend) error { return b.Seek(ctx, media.Seek{By: -seek}) }},
		{name: "volume set", run: func(ctx context.Context, b *Backend) error {
			_, err := b.Volume(ctx, media.VolumeChange{Set: &level})
			return err
		}},
		{name: "volume mute", run: func(ctx context.Context, b *Backend) error {
			_, err := b.Volume(ctx, media.VolumeChange{Mute: &mute})
			return err
		}},
		{name: "volume toggle", run: func(ctx context.Context, b *Backend) error {
			_, err := b.Volume(ctx, media.VolumeChange{ToggleMute: true})
			return err
		}},
		{name: "volume step", run: func(ctx context.Context, b *Backend) error {
			_, err := b.Volume(ctx, media.VolumeChange{Step: -10})
			return err
		}},
		{name: "now playing", playing: true, run: func(ctx context.Context, b *Backend) error { _, err := b.NowPlaying(ctx); return err }},
		{name: "shuffle", playing: true, run: func(ctx context.Context, b *Backend) error { return b.SetShuffle(ctx, true) }},
		{name: "repeat", playing: true, run: func(ctx context.Context, b *Backend) error { _, err := b.SetRepeat(ctx, media.RepeatCycle); return err }},
		{name: "streams", playing: true, run: func(ctx context.Context, b *Backend) error { _, err := b.Streams(ctx); return err }},
		{name: "subtitle", playing: true, run: func(ctx context.Context, b *Backend) error { return b.SetSubtitle(ctx, media.StreamChange{Index: 1}) }},
		{name: "audio stream", playing: true, run: func(ctx context.Context, b *Backend) error {
			return b.SetAudioStream(ctx, media.StreamChange{Step: media.StreamNext})
		}},
		{name: "send text", run: func(ctx context.Context, b *Backend) error { return b.SendText(ctx, "dune") }},
		{name: "view playlist", notFound: dirs, run: func(ctx context.Context, b *Backend) error {
			return b.ShowView(ctx, media.View{Section: media.SectionPlaylist, Playlist: ref(media.KindPlaylist, roadTrip)})
		}},
		{name: "view movie genre", run: func(ctx context.Context, b *Backend) error {
			return b.ShowView(ctx, media.View{Section: media.SectionMovies, Genre: "drama"})
		}},
		{name: "view music genre", run: func(ctx context.Context, b *Backend) error {
			return b.ShowView(ctx, media.View{Section: media.SectionMusic, Genre: "rock"})
		}},
		{name: "favourites", run: func(ctx context.Context, b *Backend) error { _, err := b.Favourites(ctx); return err }},
		{name: "open media favourite", run: func(ctx context.Context, b *Backend) error {
			return b.OpenFavourite(ctx, media.Favourite{Title: "Radio National", Type: "media", Path: "http://radio/rn.mp3"})
		}},
		{name: "open window favourite", run: func(ctx context.Context, b *Backend) error {
			return b.OpenFavourite(ctx, media.Favourite{Title: "Kids", Type: "window", Window: "videos", WindowParameter: "videodb://movies/"})
		}},
		{name: "open script favourite", run: func(ctx context.Context, b *Backend) error {
			return b.OpenFavourite(ctx, media.Favourite{Title: "Weather", Type: "script", Path: "script.weather"})
		}},
		{name: "addons", run: func(ctx context.Context, b *Backend) error { _, err := b.Addons(ctx); return err }},
		{name: "execute addon", run: func(ctx context.Context, b *Backend) error {
			return b.ExecuteAddon(ctx, "script.globalsearch", map[string]string{"searchstring": "dune"})
		}},
		{name: "clock", run: func(ctx context.Context, b *Backend) error { _, err := b.Clock(ctx); return err }},
	}

	for _, kind := range []media.Kind{media.KindMovie, media.KindTVShow, media.KindMusicVideo, media.KindArtist, media.KindAlbum, media.KindSong} {
		ops = append(ops, backendOp{name: "library " + string(kind), run: func(ctx context.Context, b *Backend) error { _, err := b.Library(ctx, kind); return err }})
	}

	for kind, id := range map[media.Kind]int{
		media.KindMovie: 1, media.KindTVShow: 1, media.KindEpisode: 101, media.KindMusicVideo: 1, media.KindArtist: 1, media.KindAlbum: 1, media.KindSong: 1,
	} {
		ops = append(ops,
			backendOp{name: "item " + string(kind), run: func(ctx context.Context, b *Backend) error { _, err := b.Item(ctx, ref(kind, id)); return err }},
			backendOp{name: "play " + string(kind), run: func(ctx context.Context, b *Backend) error { return b.Play(ctx, ref(kind, id), media.PlayOptions{}) }},
		)
	}

	for _, cmd := range []media.Transport{media.TransportPause, media.TransportStop, media.TransportNext, media.TransportPrevious, media.TransportStartOver} {
		ops = append(ops, backendOp{name: "transport " + string(cmd), playing: true, run: func(ctx context.Context, b *Backend) error { return b.Transport(ctx, cmd) }})
	}

	for _, cmd := range []media.PictureCommand{media.PictureZoomIn, media.PictureRotateClockwise, media.PictureMoveLeft} {
		ops = append(ops, backendOp{name: "picture " + string(cmd), playing: true, run: func(ctx context.Context, b *Backend) error { return b.Picture(ctx, cmd) }})
	}

	for _, action := range []media.InputAction{
		media.InputUp, media.InputDown, media.InputLeft, media.InputRight, media.InputSelect, media.InputBack, media.InputHome, media.InputInfo,
		media.InputContextMenu, media.InputOSD, media.InputFullscreen, media.InputPageUp, media.InputPassthrough,
	} {
		ops = append(ops, backendOp{name: "input " + string(action), run: func(ctx context.Context, b *Backend) error { return b.Input(ctx, action) }})
	}

	for _, section := range []media.Section{media.SectionFavourites, media.SectionSubtitleSearch, media.SectionMovies} {
		ops = append(ops, backendOp{name: "view " + string(section), run: func(ctx context.Context, b *Backend) error { return b.ShowView(ctx, media.View{Section: section}) }})
	}

	for _, cmd := range []media.SystemCommand{media.SystemShutdown, media.SystemReboot, media.SystemHibernate, media.SystemSuspend, media.SystemQuit, media.SystemEject} {
		ops = append(ops, backendOp{name: "system " + string(cmd), run: func(ctx context.Context, b *Backend) error { return b.System(ctx, cmd) }})
	}

	for _, db := range []media.Database{media.DatabaseVideo, media.DatabaseMusic} {
		ops = append(ops,
			backendOp{name: "scan " + string(db), run: func(ctx context.Context, b *Backend) error { return b.ScanLibrary(ctx, db) }},
			backendOp{name: "clean " + string(db), run: func(ctx context.Context, b *Backend) error { return b.CleanLibrary(ctx, db) }},
		)
	}

	return ops
}

func failWith(s *fakekodi.Server, method string) {
	s.Handle(method, func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: CodeInternalError, Message: "Internal error."}
	})
}

// TestEveryFailingCallIsReported fails, one at a time, each Kodi method an operation calls, and checks the operation reports that method's error, unless the failure is one it deliberately absorbs.
func TestEveryFailingCallIsReported(t *testing.T) {
	for _, op := range backendOps() {
		t.Run(op.name, func(t *testing.T) {
			b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
			s.SetClock("09:42:17 PM")

			if op.playing {
				playing(t, b)
			}

			before := len(s.Calls())
			require.NoError(t, op.run(t.Context(), b), "the operation works when Kodi does")

			var methods []string

			for _, c := range s.Calls()[before:] {
				// The version check only gates calls; failing it lets them through.
				if c.Method != kodirpc.MethodJSONRPCVersion && !slices.Contains(methods, c.Method) {
					methods = append(methods, c.Method)
				}
			}

			require.NotEmpty(t, methods)

			for _, m := range methods {
				t.Run(m, func(t *testing.T) {
					b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
					s.SetClock("09:42:17 PM")

					if op.playing {
						playing(t, b)
					}

					failWith(s, m)

					err := op.run(t.Context(), b)

					switch {
					case slices.Contains(op.tolerated, m):
						require.NoError(t, err)
					case slices.Contains(op.notFound, m):
						require.ErrorIs(t, err, media.ErrNotFound)
					default:
						var rpcErr *RPCError

						require.ErrorAs(t, err, &rpcErr)
						assert.Equal(t, m, rpcErr.Method)
						assert.Equal(t, CodeInternalError, rpcErr.Code)
						require.NotErrorIs(t, err, media.ErrNotFound, "an internal error isn't a missing item")
						require.NotErrorIs(t, err, media.ErrNothingPlaying)
					}
				})
			}
		})
	}
}

func TestUnknownRequestsAreRejectedLocally(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	playing(t, b)

	ctx := t.Context()
	sent := len(s.Calls())
	ref := func(kind media.Kind) media.Ref { return media.Ref{Kind: kind, ID: 1} }

	for name, err := range map[string]error{
		"repeat":        second(b.SetRepeat(ctx, "sometimes")),
		"subtitle":      b.SetSubtitle(ctx, media.StreamChange{Step: "sideways"}),
		"audio stream":  b.SetAudioStream(ctx, media.StreamChange{Step: media.StreamOn}),
		"transport":     b.Transport(ctx, "rewind"),
		"seek":          b.Seek(ctx, media.Seek{Step: "leap"}),
		"picture":       b.Picture(ctx, "spin"),
		"input":         b.Input(ctx, "jump"),
		"system":        b.System(ctx, "explode"),
		"scan":          b.ScanLibrary(ctx, "pictures"),
		"clean":         b.CleanLibrary(ctx, "pictures"),
		"section":       b.ShowView(ctx, media.View{Section: "garden"}),
		"genre section": b.ShowView(ctx, media.View{Section: media.SectionArtists, Genre: "rock"}),
	} {
		require.Error(t, err, name)
		require.NotErrorIs(t, err, media.ErrUnreachable, name)
	}

	for name, err := range map[string]error{
		"item":          second(b.Item(ctx, ref("channel"))),
		"play":          b.Play(ctx, ref("channel"), media.PlayOptions{}),
		"shuffle movie": b.Play(ctx, ref(media.KindMovie), media.PlayOptions{Shuffle: true}),
		"queue album":   b.Queue(ctx, []media.Ref{ref(media.KindAlbum)}),
		"queue later":   b.Queue(ctx, []media.Ref{ref(media.KindSong), ref(media.KindPlaylist)}),
		"library":       second(b.Library(ctx, media.KindEpisode)),
		"search":        second(b.Search(ctx, "channel", "news")),
		"recent":        second(b.RecentlyAdded(ctx, "channel", time.Time{})),
	} {
		if name == "shuffle movie" {
			require.NoError(t, err, "a movie has nothing to shuffle, so it just plays")

			continue
		}

		require.ErrorIs(t, err, errUnsupportedKind, name)
	}

	for _, c := range s.Calls()[sent:] {
		assert.Contains(t, []string{kodirpc.MethodPlayerGetActivePlayers, kodirpc.MethodPlayerOpen, kodirpc.MethodXBMCGetInfoLabels}, c.Method,
			"only shuffle movie reaches Kodi, and invalid commands may check for a player first")
	}

	items, err := b.RecentlyAdded(ctx, media.KindPlaylist, time.Time{})
	require.NoError(t, err)
	assert.Empty(t, items, "playlists have no date added, so nothing is recent")
}

func second[T any](_ T, err error) error { return err }

func TestInfoNeedsTheAPIVersion(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	failWith(s, kodirpc.MethodJSONRPCVersion)

	_, err := b.Info(t.Context())

	var rpcErr *RPCError

	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, kodirpc.MethodJSONRPCVersion, rpcErr.Method)
}

func TestInfoFallsBackToTheApplicationName(t *testing.T) {
	for name, labels := range map[string]fakekodi.HandlerFunc{
		"labels fail": func(json.RawMessage) (any, *fakekodi.Error) {
			return nil, &fakekodi.Error{Code: CodeInternalError, Message: "x"}
		},
		"name is blank": func(json.RawMessage) (any, *fakekodi.Error) { return map[string]string{"System.FriendlyName": ""}, nil },
	} {
		t.Run(name, func(t *testing.T) {
			b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
			s.Handle(kodirpc.MethodXBMCGetInfoLabels, labels)

			info, err := b.Info(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "Kodi", info.Name, "the application's own name")

			b, s = newBackend(t, fakekodi.Options{}, BackendOptions{Name: "Lounge"})
			s.Handle(kodirpc.MethodXBMCGetInfoLabels, labels)

			info, err = b.Info(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "Lounge", info.Name, "the configured name beats Kodi's")
		})
	}
}

func TestRPCErrorIs(t *testing.T) {
	for _, tc := range []struct {
		err         RPCError
		notFound    bool
		unavailable bool
	}{
		{RPCError{Code: CodeNotFound, Method: kodirpc.MethodPlayerStop}, true, false},
		{RPCError{Code: CodeInvalidParams, Method: kodirpc.MethodVideoLibraryGetMovieDetails}, true, false},
		{RPCError{Code: CodeInvalidParams, Method: kodirpc.MethodPlayerOpen}, true, false},
		{RPCError{Code: CodeInvalidParams, Method: kodirpc.MethodPlayerSeek}, false, false},
		{RPCError{Code: CodeUnavailable, Method: kodirpc.MethodPlayerOpen}, false, true},
		{RPCError{Code: CodeInternalError, Method: kodirpc.MethodPlayerOpen}, false, false},
	} {
		err := &tc.err
		assert.Equal(t, tc.notFound, err.Is(media.ErrNotFound), "%d %s", tc.err.Code, tc.err.Method)
		assert.Equal(t, tc.unavailable, err.Is(media.ErrUnavailable), "%d %s", tc.err.Code, tc.err.Method)
		assert.False(t, err.Is(media.ErrUnreachable), "a reply is never unreachable")
	}

	assert.Equal(t, "kodi: Player.Open: error -32603: Internal error.", (&RPCError{Code: CodeInternalError, Message: "Internal error.", Method: "Player.Open"}).Error())
}
