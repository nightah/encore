package actions

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

func newTarget(items ...media.Item) (Target, *testkit.Backend, *testkit.Matcher) {
	if len(items) == 0 {
		items = testkit.SampleLibrary()
	}

	backend := testkit.NewBackend(items...)
	matcher := &testkit.Matcher{Docs: testkit.Docs(items...)}

	return Target{Name: "Lounge", Backend: backend, Matcher: matcher}, backend, matcher
}

func newService() *Service { return NewService(slog.New(slog.DiscardHandler)) }

func TestPlayTitleUniqueMatchPlays(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "hot fuzz", Kinds: []media.Kind{media.KindMovie}}, nil)

	require.NoError(t, o.Err)
	assert.Equal(t, "playback.playing_movie", o.Say.ID)
	assert.Equal(t, "Hot Fuzz", o.Say.Params["title"])
	assert.True(t, o.EndSession)
	require.NotNil(t, o.Played)
	assert.Equal(t, []string{"Play movie/4 resume"}, backend.Calls())
	require.NotNil(t, o.Match)
	assert.Equal(t, match.DecisionPlay, o.Match.Decision)
}

func TestPlayTitleAmbiguousAsks(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "dune", Kinds: []media.Kind{media.KindMovie}}, nil)

	assert.Equal(t, "match.did_you_mean", o.Say.ID)
	assert.Equal(t, say("match.title_year", map[string]string{"title": "Dune", "year": "1984"}), o.Say.Messages["first"], "the year is worded in the listener's language")
	assert.Equal(t, say("match.title_year", map[string]string{"title": "Dune", "year": "2021"}), o.Say.Messages["second"])
	assert.False(t, o.EndSession, "the question keeps the session open")
	require.NotNil(t, o.Reprompt)
	assert.Len(t, o.Pending, 2)
	assert.Empty(t, backend.Calls(), "nothing plays until the user answers")
}

func TestPlayTitlePendingResolvesAnswer(t *testing.T) {
	target, backend, _ := newTarget()
	pending := []Choice{{DocID: "movie/2", Ref: media.Ref{Kind: media.KindMovie, ID: 2}, Title: "Dune", Year: 2021}}

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "dune", Kinds: []media.Kind{media.KindMovie}, Pending: pending}, nil)

	require.NoError(t, o.Err)
	assert.Equal(t, "playback.playing_movie", o.Say.ID)
	assert.Equal(t, []string{"Play movie/2 resume"}, backend.Calls())
}

func TestChooseFirst(t *testing.T) {
	target, backend, _ := newTarget()
	pending := []Choice{{DocID: "movie/1", Ref: media.Ref{Kind: media.KindMovie, ID: 1}, Title: "Dune"}}

	o := newService().ChooseFirst(t.Context(), target, pending, nil)
	assert.Equal(t, "playback.playing_movie", o.Say.ID)
	assert.Equal(t, []string{"Play movie/1 resume"}, backend.Calls())

	o = newService().ChooseFirst(t.Context(), target, nil, nil)
	assert.Equal(t, "match.cancelled", o.Say.ID)
}

func TestPlayTitleShowPlaysNextEpisode(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().NextEpisode(t.Context(), target, "severance", nil, nil)

	require.NoError(t, o.Err)
	assert.Equal(t, "playback.playing_episode", o.Say.ID)
	assert.Equal(t, map[string]string{"show": "Severance", "season": "2", "episode": "4", "title": "Woe's Hollow"}, o.Say.Params)
	assert.Contains(t, backend.Calls(), "Play episode/102 resume")
}

func TestPlayTitleAllWatched(t *testing.T) {
	target, _, _ := newTarget()

	o := newService().NextEpisode(t.Context(), target, "bluey", nil, nil)

	assert.Equal(t, "playback.all_watched", o.Say.ID)
	assert.Equal(t, "Bluey", o.Say.Params["show"])
}

func TestPlayTitleNotFound(t *testing.T) {
	target, _, _ := newTarget()

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "conclave"}, nil)

	assert.Equal(t, "match.not_found", o.Say.ID)
	assert.Equal(t, "conclave", o.Say.Params["query"])
}

func TestPlayTitleFindsRecentlyAddedViaLibrary(t *testing.T) {
	conclave := media.Item{Kind: media.KindMovie, ID: 7, Title: "Conclave", Year: 2024}

	// Kodi has the new title, but the index was built before it was added.
	target, _, matcher := newTarget(append(testkit.SampleLibrary(), conclave)...)
	matcher.Docs = testkit.Docs(testkit.SampleLibrary()...)
	target.Library = &testkit.Library{Matcher: matcher, Hidden: []media.Item{conclave}}

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "conclave"}, nil)

	require.NoError(t, o.Err)
	assert.Equal(t, "playback.playing_movie", o.Say.ID)
	assert.Equal(t, "Conclave", o.Say.Params["title"])
}

func TestPlayTitleWakesUnreachableBackend(t *testing.T) {
	target, backend, _ := newTarget()
	backend.Err = media.ErrUnreachable

	var progress []string

	target.Wake = func(context.Context) error {
		backend.Err = nil

		return nil
	}

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "hot fuzz"}, func(_ context.Context, m Message) {
		progress = append(progress, m.ID)
	})

	require.NoError(t, o.Err)
	assert.Equal(t, "playback.playing_movie", o.Say.ID)
	assert.Equal(t, []string{"kodi.waking"}, progress)
}

func TestUnreachableWithoutWake(t *testing.T) {
	target, backend, _ := newTarget()
	backend.Err = media.ErrUnreachable

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "hot fuzz"}, nil)

	assert.Equal(t, "error.kodi_unreachable", o.Say.ID)
	assert.Equal(t, "Lounge", o.Say.Params["kodi"])
	require.ErrorIs(t, o.Err, media.ErrUnreachable)
}

func TestOtherErrorsAreGeneric(t *testing.T) {
	target, backend, _ := newTarget()
	backend.Err = errors.New("boom")

	o := newService().SetVolume(t.Context(), target, 5)

	assert.Equal(t, "error.generic", o.Say.ID)
	require.Error(t, o.Err)
}

func TestTransport(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.Transport(t.Context(), target, media.TransportPause)
	assert.Equal(t, "playback.nothing_playing", o.Say.ID, "pausing with nothing playing says so")

	require.NoError(t, backend.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 4}, media.PlayOptions{}))

	o = svc.Transport(t.Context(), target, media.TransportPause)
	assert.Empty(t, o.Say.ID, "success is silent")
	assert.True(t, o.EndSession)
}

func TestVolume(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	assert.Equal(t, "70", svc.SetVolume(t.Context(), target, 7).Say.Params["level"], "0–10 scale")
	assert.Equal(t, "35", svc.SetVolume(t.Context(), target, 35).Say.Params["level"], "percentage")
	assert.Equal(t, "100", svc.SetVolume(t.Context(), target, 400).Say.Params["level"], "clamped")

	assert.Empty(t, svc.StepVolume(t.Context(), target, false).Say.ID)
	assert.Empty(t, svc.ToggleMute(t.Context(), target).Say.ID)
	assert.Equal(t, []string{"Volume set 70", "Volume set 35", "Volume set 100", "Volume step -10", "Volume toggle mute"}, backend.Calls())
}

func TestSetVolumeScaleBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level int
		set   string
	}{
		{"zero is silent", 0, "Volume set 0"},
		{"ten is the top of the 0–10 scale", 10, "Volume set 100"},
		{"eleven is a percentage", 11, "Volume set 11"},
		{"a negative level is clamped to zero", -3, "Volume set 0"},
		{"a large negative level is clamped to zero", -40, "Volume set 0"},
		{"just over a hundred is clamped", 101, "Volume set 100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()

			o := newService().SetVolume(t.Context(), target, tc.level)

			require.NoError(t, o.Err)
			assert.Equal(t, "volume.set", o.Say.ID)
			assert.Equal(t, []string{tc.set}, backend.Calls())
		})
	}
}

func TestPendingChoicesAddToTheCallersHints(t *testing.T) {
	var got match.Query

	target, _, _ := newTarget()
	target.Matcher = matcherFunc(func(q match.Query) match.Result {
		got = q

		return match.Result{Decision: match.DecisionNotFound}
	})

	newService().PlayTitle(t.Context(), target, TitleRequest{
		Query:   "dune",
		Hints:   map[string]float64{"movie/1": 0.1, "movie/2": 0.05},
		Pending: []Choice{{DocID: "movie/1"}, {DocID: "movie/3"}},
	}, nil)

	assert.InDeltaMapValues(t, map[string]float64{"movie/1": 0.1 + pendingBoost, "movie/2": 0.05, "movie/3": pendingBoost}, got.Hints, 1e-9)
}

func TestNowPlaying(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.NowPlaying(t.Context(), target)
	assert.Equal(t, "playback.nothing_playing", o.Say.ID)

	ep := testkit.SampleLibrary()[6]
	backend.SetPlaying(&media.NowPlaying{Item: ep, Position: 31 * time.Minute, Duration: 49 * time.Minute})

	o = svc.NowPlaying(t.Context(), target)
	assert.Equal(t, "playback.now_episode", o.Say.ID)
	assert.Equal(t, 18*time.Minute, o.Say.Durations["remaining"])
	assert.Equal(t, "Woe's Hollow", o.Say.Params["title"])

	backend.SetPlaying(&media.NowPlaying{Item: testkit.SampleLibrary()[3], Position: 100 * time.Minute, Duration: 121 * time.Minute})

	o = svc.NowPlaying(t.Context(), target)
	assert.Equal(t, "playback.now_movie", o.Say.ID)
	assert.Equal(t, map[string]string{"title": "Hot Fuzz"}, o.Say.Params)
	assert.Equal(t, 21*time.Minute, o.Say.Durations["remaining"])

	backend.SetPlaying(&media.NowPlaying{Item: media.Item{Kind: media.KindSong, Title: "Come Together"}, Position: time.Minute, Duration: 4 * time.Minute})

	o = svc.NowPlaying(t.Context(), target)
	assert.Equal(t, "playback.now_other", o.Say.ID)
	assert.Equal(t, map[string]string{"title": "Come Together"}, o.Say.Params)
	assert.Empty(t, o.Say.Durations, "only films and episodes say how long is left")
}

func TestPlayTitleHintsBreakTies(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.PlayTitle(t.Context(), target, TitleRequest{Query: "dune", Kinds: []media.Kind{media.KindMovie}, Hints: map[string]float64{"movie/2": 0.1}}, nil)
	assert.Equal(t, "playback.playing_movie", o.Say.ID)
	assert.Equal(t, []string{"Play movie/2 resume"}, backend.Calls())

	req := TitleRequest{Query: "dune", Kinds: []media.Kind{media.KindMovie}, Hints: map[string]float64{"movie/1": 0.1}, Pending: []Choice{{DocID: "movie/1"}}}
	svc.PlayTitle(t.Context(), target, req, nil)
	assert.Equal(t, map[string]float64{"movie/1": 0.1}, req.Hints, "the caller's hints are not changed by the pending boost")
}

func TestPlayTitleAsks(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.PlayTitle(t.Context(), target, TitleRequest{Query: "fuzz"}, nil)
	assert.Equal(t, "match.confirm", o.Say.ID, "one uncertain match is a yes/no question")
	assert.Equal(t, say("match.title_year", map[string]string{"title": "Hot Fuzz", "year": "2007"}), o.Say.Messages["title"])
	assert.False(t, o.EndSession)
	require.Len(t, o.Pending, 1)
	assert.Equal(t, Choice{DocID: "movie/4", Ref: media.Ref{Kind: media.KindMovie, ID: 4}, Title: "Hot Fuzz", Year: 2007}, o.Pending[0])

	o = svc.PlayTitle(t.Context(), target, TitleRequest{Query: "road", Kinds: []media.Kind{media.KindPlaylist}}, nil)
	assert.Equal(t, "Road Trip", o.Say.Params["title"], "a title without a year is said alone")

	o = svc.PlayTitle(t.Context(), target, TitleRequest{Query: "dun"}, nil)
	assert.Equal(t, "match.did_you_mean", o.Say.ID)
	assert.Len(t, o.Pending, maxChoices, "at most two choices are offered")
	assert.Empty(t, backend.Calls())
}

func TestPlayTitleAskWithoutCandidatesIsNotFound(t *testing.T) {
	target, _, _ := newTarget()
	target.Matcher = matcherFunc(func(match.Query) match.Result {
		return match.Result{Decision: match.DecisionAsk, Normalized: "dnue"}
	})

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "Dnue"}, nil)

	assert.Equal(t, "match.not_found", o.Say.ID)
	assert.Equal(t, "dnue", o.Say.Params["query"])
	assert.True(t, o.EndSession)
	assert.Empty(t, o.Pending)
}

func TestPlayTitleLiveLookup(t *testing.T) {
	t.Run("searches films and TV when no kind was asked for", func(t *testing.T) {
		target, _, _ := newTarget()
		lib := &lookupLibrary{}
		target.Library = lib

		o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "conclave"}, nil)

		assert.Equal(t, "match.not_found", o.Say.ID)
		assert.Equal(t, []media.Kind{media.KindMovie, media.KindTVShow}, lib.kinds)
	})

	t.Run("searches only the kinds asked for", func(t *testing.T) {
		target, _, _ := newTarget()
		lib := &lookupLibrary{}
		target.Library = lib

		newService().PlayTitle(t.Context(), target, TitleRequest{Query: "conclave", Kinds: []media.Kind{media.KindSong}}, nil)

		assert.Equal(t, []media.Kind{media.KindSong}, lib.kinds)
	})

	t.Run("a failed lookup is a miss", func(t *testing.T) {
		target, _, _ := newTarget()
		m := &brokenMatcher{ok: 1}
		target.Matcher = m
		target.Library = &lookupLibrary{err: errBoom}

		o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "conclave"}, nil)

		assert.Equal(t, "match.not_found", o.Say.ID)
		require.NoError(t, o.Err)
		assert.Equal(t, 1, m.searches, "nothing new to search for")
	})
}

func TestChooseFirstSaysWhatPlays(t *testing.T) {
	for _, tc := range []struct {
		ref    media.Ref
		id     string
		params map[string]string
		call   string
	}{
		{media.Ref{Kind: media.KindAlbum, ID: 42}, "music.playing_album", map[string]string{"title": "Abbey Road", "artist": "The Beatles"}, "Play album/42 resume"},
		{media.Ref{Kind: media.KindSong, ID: 52}, "music.playing_song", map[string]string{"title": "Come Together", "artist": "The Beatles"}, "Play song/52 resume"},
		{media.Ref{Kind: media.KindMusicVideo, ID: 60}, "music.playing_video", map[string]string{"title": "Bohemian Rhapsody", "artist": "Queen"}, "Play musicvideo/60 resume"},
		{media.Ref{Kind: media.KindArtist, ID: 30}, "music.playing_artist", map[string]string{"title": "Queen"}, "Play artist/30 shuffle limit 100"},
		{media.Ref{Kind: media.KindPlaylist, ID: 70}, "playlist.playing", map[string]string{"playlist": "Road Trip"}, "Play playlist/70 resume"},
		{media.Ref{Kind: media.KindEpisode, ID: 102}, "playback.playing_episode", map[string]string{"show": "Severance", "season": "2", "episode": "4", "title": "Woe's Hollow"}, "Play episode/102 resume"},
	} {
		t.Run(string(tc.ref.Kind), func(t *testing.T) {
			target, backend, _ := newTarget()
			title := ""

			for _, it := range testkit.SampleLibrary() {
				if it.Ref() == tc.ref {
					title = it.Title
				}
			}

			o := newService().ChooseFirst(t.Context(), target, []Choice{{DocID: "x", Ref: tc.ref, Title: title}}, nil)

			require.NoError(t, o.Err)
			assert.Equal(t, tc.id, o.Say.ID)
			assert.Equal(t, tc.params, o.Say.Params)
			assert.Equal(t, tc.call, lastCall(backend.Calls()))
			require.NotNil(t, o.Played)
			assert.Equal(t, tc.ref, o.Played.Ref())
		})
	}
}

func TestSilentSuccessesAskForConfirmation(t *testing.T) {
	target, backend, _ := newTarget(testkit.SampleLibrary()...)
	backend.SetPlaying(&media.NowPlaying{Item: media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune"}})
	svc := NewService(slog.New(slog.DiscardHandler))

	for name, o := range map[string]Outcome{
		"pause":       svc.Transport(t.Context(), target, media.TransportPause),
		"volume up":   svc.StepVolume(t.Context(), target, true),
		"mute":        svc.ToggleMute(t.Context(), target),
		"volume to 5": svc.SetVolume(t.Context(), target, 5),
	} {
		if name == "volume to 5" {
			assert.False(t, o.Confirm, "%s says the level, so needs no sound", name)

			continue
		}

		assert.True(t, o.Confirm, name)
		assert.Empty(t, o.Say.ID, name)
	}

	backend.SetPlaying(nil)
	assert.False(t, svc.Transport(t.Context(), target, media.TransportPause).Confirm, "nothing playing is said, not confirmed")
}

func TestTargetAllows(t *testing.T) {
	all := Target{}
	noMovies := Target{Disabled: []media.Content{media.ContentMovies}}
	onlyMovies := Target{Disabled: []media.Content{media.ContentTV, media.ContentMusic}}

	for _, k := range []media.Kind{media.KindMovie, media.KindTVShow, media.KindEpisode, media.KindSong, media.KindMusicVideo, media.KindPlaylist} {
		assert.True(t, all.Allows(k), "the zero value allows %s", k)
	}

	assert.False(t, noMovies.Allows(media.KindMovie))
	assert.True(t, noMovies.Allows(media.KindEpisode))
	assert.False(t, onlyMovies.Allows(media.KindEpisode), "episodes belong to TV")
	assert.False(t, onlyMovies.Allows(media.KindMusicVideo), "music videos belong to music")
	assert.True(t, onlyMovies.Allows(media.KindPlaylist), "playlists belong to no group, so are never disabled")

	kinds := []media.Kind{media.KindSong, media.KindMovie, media.KindTVShow, media.KindPlaylist, media.KindMovie}
	assert.Equal(t, []media.Kind{media.KindMovie, media.KindPlaylist, media.KindMovie}, onlyMovies.Allowed(kinds), "order and duplicates are kept")
	assert.Equal(t, kinds, all.Allowed(kinds))
	assert.Empty(t, noMovies.Allowed(nil))
	assert.NotNil(t, noMovies.Allowed(nil), "an empty list, not nil")
}

func TestContentDisabled(t *testing.T) {
	o := ContentDisabled()

	assert.Equal(t, "content.disabled", o.Say.ID)
	assert.True(t, o.EndSession)
	assert.NoError(t, o.Err)
}

func TestTargetLanguageComparesNames(t *testing.T) {
	ärzte := []media.Item{
		{Kind: media.KindArtist, ID: 1, Title: "Die Ärzte", Artist: "Die Ärzte"},
		{Kind: media.KindAlbum, ID: 2, Title: "Jazz ist anders", Artist: "Die Ärzte"},
	}
	req := MusicRequest{Album: "jazz ist anders", Artist: "ärzte"}

	target, backend, _ := newTarget(ärzte...)
	assert.Equal(t, "music.not_found_by", newService().Album(t.Context(), target, req, nil).Say.ID, "English keeps the German article")

	target.Language = "de"
	o := newService().Album(t.Context(), target, req, nil)
	assert.Equal(t, "music.playing_album", o.Say.ID, "German drops its article")
	assert.Equal(t, []string{"Library album", "Library album", "Play album/2"}, backend.Calls())
}

func TestWakeFailureIsReported(t *testing.T) {
	target, backend, _ := newTarget()
	backend.Err = media.ErrUnreachable
	wakeErr := errors.New("no magic packet")

	var progress []string

	target.Wake = func(context.Context) error { return wakeErr }

	o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "hot fuzz"}, func(_ context.Context, m Message) {
		progress = append(progress, m.ID)
	})

	assert.Equal(t, "error.kodi_unreachable", o.Say.ID)
	require.ErrorIs(t, o.Err, media.ErrUnreachable)
	require.ErrorIs(t, o.Err, wakeErr)
	assert.Equal(t, []string{"kodi.waking"}, progress)
	assert.Equal(t, []string{"Play movie/4 resume"}, backend.Calls(), "no retry after the wake failed")
}

func TestWakeOnlyForUnreachable(t *testing.T) {
	target, _, _ := newTarget()
	target, _ = failing(target, testkit.NewBackend(testkit.SampleLibrary()...), errBoom, "Play")
	woken := false
	target.Wake = func(context.Context) error {
		woken = true

		return nil
	}

	assertFailed(t, newService().PlayTitle(t.Context(), target, TitleRequest{Query: "hot fuzz"}, nil))
	assert.False(t, woken, "a reachable backend that fails is not woken")
}

func TestWakeRetriesOnceWithoutProgress(t *testing.T) {
	target, backend, _ := newTarget()
	target, f := failing(target, backend, media.ErrUnreachable, "Play")
	wakes := 0
	target.Wake = func(context.Context) error {
		wakes++

		delete(f.fail, "Play")

		return nil
	}

	o := newService().Playlist(t.Context(), target, "road trip", false, nil)

	assert.Equal(t, "playlist.playing", o.Say.ID, "a nil progress is allowed")
	assert.Equal(t, 1, wakes)
}

func TestBackendFailuresAreReported(t *testing.T) {
	type call func(context.Context, *Service, Target) Outcome

	for _, tc := range []struct {
		name    string
		methods []string
		playing bool
		setup   func(*testkit.Backend)
		call    call
	}{
		{"transport", []string{"Transport"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Transport(ctx, t, media.TransportPause)
		}},
		{"step volume", []string{"Volume"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.StepVolume(ctx, t, true) }},
		{"toggle mute", []string{"Volume"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.ToggleMute(ctx, t) }},
		{"now playing", []string{"NowPlaying"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.NowPlaying(ctx, t) }},
		{"seek", []string{"Seek"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Seek(ctx, t, SeekRequest{}) }},
		{"picture", []string{"Picture"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Picture(ctx, t, media.PictureZoomIn)
		}},
		{"toggle", []string{"Input"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Toggle(ctx, t, media.InputFullscreen)
		}},
		{"time remaining", []string{"NowPlaying"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.TimeRemaining(ctx, t) }},
		{"navigate", []string{"Input"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Navigate(ctx, t, media.InputDown, 3) }},
		{"send text", []string{"SendText"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.SendText(ctx, t, "dune") }},
		{"section genres", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ShowSection(ctx, t, media.SectionMovies, "comedy")
		}},
		{"section view", []string{"ShowView"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ShowSection(ctx, t, media.SectionMovies, "")
		}},
		{"playlist view", []string{"ShowView"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.ShowPlaylist(ctx, t, "road trip") }},
		{"favourites", []string{"Favourites"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.OpenFavourite(ctx, t, "radio") }},
		{"subtitle streams", []string{"Streams"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Subtitles(ctx, t, StreamRequest{Language: "french"})
		}},
		{"subtitle streams after the change", []string{"Streams"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Subtitles(ctx, t, StreamRequest{Step: media.StreamNext})
		}},
		{"set subtitle", []string{"SetSubtitle"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Subtitles(ctx, t, StreamRequest{Step: media.StreamOff})
		}},
		{"audio streams", []string{"Streams"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.AudioStream(ctx, t, StreamRequest{Language: "french"})
		}},
		{"audio streams after the change", []string{"Streams"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.AudioStream(ctx, t, StreamRequest{Step: media.StreamNext})
		}},
		{"set audio stream", []string{"SetAudioStream"}, true, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.AudioStream(ctx, t, StreamRequest{Step: media.StreamNext})
		}},
		{"subtitle search", []string{"ShowView"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.DownloadSubtitles(ctx, t) }},
		{"power", []string{"System"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ConfirmPower(ctx, t, media.SystemReboot)
		}},
		{"eject", []string{"System"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Eject(ctx, t) }},
		{"scan", []string{"ScanLibrary"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.UpdateLibrary(ctx, t, false) }},
		{"clean", []string{"CleanLibrary"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.UpdateLibrary(ctx, t, true) }},
		{"addons", []string{"Addons"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.LaunchAddon(ctx, t, "youtube") }},
		{"launch addon", []string{"ExecuteAddon"}, false, withAddons, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.LaunchAddon(ctx, t, "youtube")
		}},
		{"global search addons", []string{"Addons"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.GlobalSearch(ctx, t, "dune") }},
		{"global search", []string{"ExecuteAddon"}, false, withAddons, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.GlobalSearch(ctx, t, "dune")
		}},
		{"play title", []string{"Play"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.PlayTitle(ctx, t, TitleRequest{Query: "hot fuzz"}, nil)
		}},
		{"next episode", []string{"NextEpisode"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.NextEpisode(ctx, t, "severance", nil, nil)
		}},
		{"album details", []string{"Item"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.PlayTitle(ctx, t, TitleRequest{Query: "abbey road", Kinds: []media.Kind{media.KindAlbum}}, nil)
		}},
		{"artist", []string{"Play"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Artist(ctx, t, MusicRequest{Artist: "queen"}, nil)
		}},
		{"artist genre", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Artist(ctx, t, MusicRequest{Artist: "queen", Genre: "rock"}, nil)
		}},
		{"artist genre queue", []string{"Queue"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Artist(ctx, t, MusicRequest{Artist: "queen", Genre: "rock"}, nil)
		}},
		{"album lookup", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Album(ctx, t, MusicRequest{Album: "innuendo"}, nil)
		}},
		{"album", []string{"Play"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Album(ctx, t, MusicRequest{Album: "innuendo"}, nil)
		}},
		{"latest album", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.LatestAlbum(ctx, t, MusicRequest{Artist: "queen"}, nil)
		}},
		{"song lookup", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Song(ctx, t, MusicRequest{Title: "come together"}, nil)
		}},
		{"song", []string{"Play"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Song(ctx, t, MusicRequest{Title: "come together"}, nil)
		}},
		{"song or album", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.SongOrAlbum(ctx, t, MusicRequest{Title: "come together"}, nil)
		}},
		{"genre", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Genre(ctx, t, "rock", nil) }},
		{"recent songs", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.RecentSongs(ctx, t, nil) }},
		{"party mode", []string{"PartyMode"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.PartyMode(ctx, t, nil) }},
		{"music video lookup", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.MusicVideo(ctx, t, MusicRequest{Title: "bohemian rhapsody"}, nil)
		}},
		{"random music video", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.RandomMusicVideo(ctx, t, MusicRequest{}, nil)
		}},
		{"shuffle music videos", []string{"Queue"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ShuffleMusicVideos(ctx, t, MusicRequest{}, nil)
		}},
		{"playlist", []string{"Play"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Playlist(ctx, t, "road trip", true, nil)
		}},
		{"recommend", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Recommend(ctx, t, RecommendRequest{Kinds: []media.Kind{media.KindMovie}}, nil)
		}},
		{"recommend episode", []string{"NextEpisode"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Recommend(ctx, t, RecommendRequest{Kinds: []media.Kind{media.KindEpisode}}, nil)
		}},
		{"new albums", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{Kind: media.KindAlbum})
		}},
		{"new movies", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{})
		}},
		{"unwatched episodes", []string{"Episodes"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{Show: "severance"})
		}},
		{"new episodes", []string{"RecentlyAdded"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{Kind: media.KindEpisode})
		}},
		{"albums by", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.AlbumsBy(ctx, t, "queen") }},
		{"random movie", []string{"Play"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.RandomMovie(ctx, t, "", nil) }},
		{"random movie pool", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.RandomMovie(ctx, t, "", nil) }},
		{"trailer details", []string{"Item"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Trailer(ctx, t, "hot fuzz", nil) }},
		{"trailer", []string{"PlayFile"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Trailer(ctx, t, "hot fuzz", nil) }},
		{"collection details", []string{"Item"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.NextInCollection(ctx, t, "dune", nil)
		}},
		{"collection", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.NextInCollection(ctx, t, "dune", nil)
		}},
		{"episode", []string{"Episodes"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Episode(ctx, t, "severance", 2, 3, nil)
		}},
		{"first episode", []string{"Episodes"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.FirstEpisode(ctx, t, "severance", nil)
		}},
		{"latest episode", []string{"Episodes"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.LatestEpisode(ctx, t, "severance", nil)
		}},
		{"continue watching shows", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ContinueWatching(ctx, t, nil)
		}},
		{"continue watching", []string{"NextEpisode"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ContinueWatching(ctx, t, nil)
		}},
		{"random episode list", []string{"Episodes"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.RandomEpisode(ctx, t, "bluey", nil)
		}},
		{"random episode", []string{"Play"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.RandomEpisode(ctx, t, "bluey", nil)
		}},
		{"random show", []string{"NextEpisode"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.RandomShow(ctx, t, "", nil)
		}},
		{"random show pool", []string{"Library"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.RandomShow(ctx, t, "", nil)
		}},
		{"shuffle show", []string{"Play"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ShuffleShow(ctx, t, "severance", nil)
		}},
		{"binge next", []string{"NextEpisode"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Binge(ctx, t, "severance", nil) }},
		{"binge episodes", []string{"Episodes"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Binge(ctx, t, "severance", nil) }},
		{"binge queue", []string{"Queue"}, false, nil, func(ctx context.Context, s *Service, t Target) Outcome { return s.Binge(ctx, t, "severance", nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()
			if tc.playing {
				require.NoError(t, backend.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 4}, media.PlayOptions{}))
			}

			if tc.setup != nil {
				tc.setup(backend)
			}

			target, _ = failing(target, backend, errBoom, tc.methods...)
			svc := newService()
			svc.SetSeed(1)

			o := tc.call(t.Context(), svc, target)

			assertFailed(t, o)
			assert.Nil(t, o.Played, "nothing is reported as playing")
			assert.Empty(t, o.Pending, "nothing is offered")
		})
	}
}

func withAddons(b *testkit.Backend) {
	b.SetAddons(media.Addon{ID: "plugin.video.youtube", Name: "YouTube"}, media.Addon{ID: globalSearchAddon, Name: "Global Search"})
}

func TestUnreachableBackendNamesTheInstance(t *testing.T) {
	target, backend := playingTarget(t)
	target, _ = failing(target, backend, media.ErrUnreachable, "Seek", "Volume", "OpenFavourite")
	backend.SetFavourites(media.Favourite{Title: "Radio National"})
	svc := newService()

	for name, o := range map[string]Outcome{
		"seek":      svc.Seek(t.Context(), target, SeekRequest{By: time.Minute}),
		"volume":    svc.SetVolume(t.Context(), target, 3),
		"favourite": svc.OpenFavourite(t.Context(), target, "radio national"),
	} {
		assert.Equal(t, "error.kodi_unreachable", o.Say.ID, name)
		assert.Equal(t, "Lounge", o.Say.Params["kodi"], name)
		require.ErrorIs(t, o.Err, media.ErrUnreachable, name)
	}
}

func TestMatcherFailuresAreReported(t *testing.T) {
	for name, call := range map[string]func(context.Context, *Service, Target) Outcome{
		"play title": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.PlayTitle(ctx, t, TitleRequest{Query: "dune"}, nil)
		},
		"artist": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Artist(ctx, t, MusicRequest{Artist: "queen"}, nil)
		},
		"album": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Album(ctx, t, MusicRequest{Album: "innuendo"}, nil)
		},
		"song": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Song(ctx, t, MusicRequest{Title: "x"}, nil)
		},
		"song or album": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.SongOrAlbum(ctx, t, MusicRequest{Title: "x"}, nil)
		},
		"music video": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.MusicVideo(ctx, t, MusicRequest{Title: "x"}, nil)
		},
		"latest album": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.LatestAlbum(ctx, t, MusicRequest{Artist: "queen"}, nil)
		},
		"albums by": func(ctx context.Context, s *Service, t Target) Outcome { return s.AlbumsBy(ctx, t, "queen") },
		"playlist": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Playlist(ctx, t, "road trip", false, nil)
		},
		"show playlist": func(ctx context.Context, s *Service, t Target) Outcome { return s.ShowPlaylist(ctx, t, "road trip") },
		"trailer":       func(ctx context.Context, s *Service, t Target) Outcome { return s.Trailer(ctx, t, "hot fuzz", nil) },
		"collection": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.NextInCollection(ctx, t, "dune", nil)
		},
		"episode": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Episode(ctx, t, "severance", 1, 1, nil)
		},
		"binge": func(ctx context.Context, s *Service, t Target) Outcome { return s.Binge(ctx, t, "severance", nil) },
		"unwatched": func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{Show: "severance"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			target, backend, _ := newTarget()
			target.Matcher = &brokenMatcher{}

			assertFailed(t, call(t.Context(), newService(), target))
			assert.Empty(t, backend.Calls(), "nothing is sent to Kodi")
		})

		t.Run(name+" after a live lookup", func(t *testing.T) {
			target, _, _ := newTarget()
			m := &brokenMatcher{ok: 1}
			target.Matcher = m
			target.Library = &lookupLibrary{found: []media.Item{{Kind: media.KindMovie, ID: 9, Title: "New"}}}

			assertFailed(t, call(t.Context(), newService(), target))
			assert.Equal(t, 2, m.searches, "the index is searched again after the lookup found something")
		})
	}
}
