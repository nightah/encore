package actions

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
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

func TestNextPlaysTheFollowingEpisodeWhenNothingIsQueued(t *testing.T) {
	ep := func(id, season, episode int) media.Item {
		return media.Item{Kind: media.KindEpisode, ID: id, ShowID: 7, ShowTitle: "Severance", Season: season, Episode: episode, Title: "Episode " + strconv.Itoa(id)}
	}
	special, s1e1, s1e2, s2e1 := ep(70, 0, 1), ep(71, 1, 1), ep(72, 1, 2), ep(73, 2, 1)
	movie := media.Item{Kind: media.KindMovie, ID: 4, Title: "Hot Fuzz"}
	stranger := ep(99, 1, 3)

	testCases := []struct {
		name     string
		have     *media.NowPlaying
		expected string
		calls    []string
	}{
		{name: "ShouldSayNothingIsPlaying", expected: "playback.nothing_playing", calls: []string{"NowPlaying", "Transport next"}},
		{name: "ShouldSkipInTheQueueForAMovie", have: &media.NowPlaying{Item: movie}, calls: []string{"NowPlaying", "Transport next"}},
		{name: "ShouldSkipInTheQueueForAQueuedEpisode", have: &media.NowPlaying{Item: s1e1, Queued: true}, calls: []string{"NowPlaying", "Transport next"}},
		{name: "ShouldPlayTheNextEpisode", have: &media.NowPlaying{Item: s1e1}, expected: "playback.playing_episode", calls: []string{"NowPlaying", "Episodes 7", "Play episode/72 resume"}},
		{name: "ShouldPlayIntoTheNextSeason", have: &media.NowPlaying{Item: s1e2}, expected: "playback.playing_episode", calls: []string{"NowPlaying", "Episodes 7", "Play episode/73 resume"}},
		{name: "ShouldPlayFromASpecialToTheFirstEpisode", have: &media.NowPlaying{Item: special}, expected: "playback.playing_episode", calls: []string{"NowPlaying", "Episodes 7", "Play episode/71 resume"}},
		{name: "ShouldSaySoAfterTheLastEpisode", have: &media.NowPlaying{Item: s2e1}, expected: "tv.last_episode", calls: []string{"NowPlaying", "Episodes 7"}},
		{name: "ShouldSkipInTheQueueForAnEpisodeOutsideTheLibrary", have: &media.NowPlaying{Item: stranger}, calls: []string{"NowPlaying", "Episodes 7", "Transport next"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget(movie, s2e1, s1e2, special, s1e1)
			backend.SetPlaying(tc.have)

			o := newService().Next(t.Context(), target)

			require.NoError(t, o.Err)
			assert.Equal(t, tc.expected, o.Say.ID)
			assert.Equal(t, tc.calls, backend.Calls())

			if tc.expected == "tv.last_episode" {
				assert.Equal(t, "Severance", o.Say.Params["show"])
			}
		})
	}
}

func TestNextFromACopyTheListDidNotKeep(t *testing.T) {
	ep := func(id, episode int, q media.Quality) media.Item {
		return media.Item{Kind: media.KindEpisode, ID: id, ShowID: 7, ShowTitle: "Severance", Season: 1, Episode: episode, Quality: q}
	}
	uhd1, fhd1, uhd2, fhd2 := ep(71, 1, media.Quality2160p), ep(81, 1, media.Quality1080p), ep(72, 2, media.Quality2160p), ep(82, 2, media.Quality1080p)

	target, backend, _ := newTarget(uhd1, fhd1, uhd2, fhd2)
	target.Backend = media.PreferCopies(target.Backend, media.Quality2160p, nil)
	backend.SetPlaying(&media.NowPlaying{Item: fhd1})

	o := newService().Next(t.Context(), target)

	require.NoError(t, o.Err)
	assert.Equal(t, "playback.playing_episode", o.Say.ID)
	assert.Equal(t, []string{"NowPlaying", "Episodes 7", "Play episode/72 resume 2160p"}, backend.Calls(), "the 1080p copy of 1x01 is playing; 1x02 follows in the preferred copy")
}

func TestNextFailures(t *testing.T) {
	playing := &media.NowPlaying{Item: media.Item{Kind: media.KindEpisode, ID: 71, ShowID: 7, Season: 1, Episode: 1}}

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldReportAFailingNowPlaying", have: "NowPlaying"},
		{name: "ShouldReportFailingEpisodes", have: "Episodes"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()
			backend.SetPlaying(playing)
			target, _ = failing(target, backend, errBoom, tc.have)

			o := newService().Next(t.Context(), target)

			assert.Equal(t, "error.generic", o.Say.ID)
			require.ErrorIs(t, o.Err, errBoom)
		})
	}
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
	testCases := []struct {
		name     string
		have     int
		expected string
	}{
		{name: "ShouldTakeZeroAsSilent", expected: "Volume set 0"},
		{name: "ShouldTakeTenAsTheTopOfTheZeroToTenScale", have: 10, expected: "Volume set 100"},
		{name: "ShouldTakeElevenAsAPercentage", have: 11, expected: "Volume set 11"},
		{name: "ShouldClampANegativeLevelToZero", have: -3, expected: "Volume set 0"},
		{name: "ShouldClampALargeNegativeLevelToZero", have: -40, expected: "Volume set 0"},
		{name: "ShouldClampJustOverAHundred", have: 101, expected: "Volume set 100"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()

			o := newService().SetVolume(t.Context(), target, tc.have)

			require.NoError(t, o.Err)
			assert.Equal(t, "volume.set", o.Say.ID)
			assert.Equal(t, []string{tc.expected}, backend.Calls())
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

func TestPlayTitlePassesResolvedValues(t *testing.T) {
	var got match.Query

	target, _, _ := newTarget()
	target.Matcher = matcherFunc(func(q match.Query) match.Result {
		got = q

		return match.Result{Decision: match.DecisionNotFound}
	})

	resolved := []match.Resolved{{ID: "movie/7", Name: "千と千尋の神隠し"}}
	newService().PlayTitle(t.Context(), target, TitleRequest{Query: "せんとちひろのかみかくし", Resolved: resolved}, nil)

	assert.Equal(t, resolved, got.Resolved)
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
	t.Run("ShouldSearchFilmsAndTVWhenNoKindWasAskedFor", func(t *testing.T) {
		target, _, _ := newTarget()
		lib := &lookupLibrary{}
		target.Library = lib

		o := newService().PlayTitle(t.Context(), target, TitleRequest{Query: "conclave"}, nil)

		assert.Equal(t, "match.not_found", o.Say.ID)
		assert.Equal(t, []media.Kind{media.KindMovie, media.KindTVShow}, lib.kinds)
	})

	t.Run("ShouldSearchOnlyTheKindsAskedFor", func(t *testing.T) {
		target, _, _ := newTarget()
		lib := &lookupLibrary{}
		target.Library = lib

		newService().PlayTitle(t.Context(), target, TitleRequest{Query: "conclave", Kinds: []media.Kind{media.KindSong}}, nil)

		assert.Equal(t, []media.Kind{media.KindSong}, lib.kinds)
	})

	t.Run("ShouldMissWhenTheLookupFails", func(t *testing.T) {
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
	testCases := []struct {
		name     string
		have     media.Ref
		expected string
		params   map[string]string
		call     string
	}{
		{name: "ShouldSayAnAlbumPlays", have: media.Ref{Kind: media.KindAlbum, ID: 42}, expected: "music.playing_album", params: map[string]string{"title": "Abbey Road", "artist": "The Beatles"}, call: "Play album/42 resume"},
		{name: "ShouldSayASongPlays", have: media.Ref{Kind: media.KindSong, ID: 52}, expected: "music.playing_song", params: map[string]string{"title": "Come Together", "artist": "The Beatles"}, call: "Play song/52 resume"},
		{name: "ShouldSayAMusicVideoPlays", have: media.Ref{Kind: media.KindMusicVideo, ID: 60}, expected: "music.playing_video", params: map[string]string{"title": "Bohemian Rhapsody", "artist": "Queen"}, call: "Play musicvideo/60 resume"},
		{name: "ShouldSayAnArtistPlays", have: media.Ref{Kind: media.KindArtist, ID: 30}, expected: "music.playing_artist", params: map[string]string{"title": "Queen"}, call: "Play artist/30 shuffle limit 100"},
		{name: "ShouldSayAPlaylistPlays", have: media.Ref{Kind: media.KindPlaylist, ID: 70}, expected: "playlist.playing", params: map[string]string{"playlist": "Road Trip"}, call: "Play playlist/70 resume"},
		{name: "ShouldSayAnEpisodePlays", have: media.Ref{Kind: media.KindEpisode, ID: 102}, expected: "playback.playing_episode", params: map[string]string{"show": "Severance", "season": "2", "episode": "4", "title": "Woe's Hollow"}, call: "Play episode/102 resume"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()
			title := ""

			for _, it := range testkit.SampleLibrary() {
				if it.Ref() == tc.have {
					title = it.Title
				}
			}

			o := newService().ChooseFirst(t.Context(), target, []Choice{{DocID: "x", Ref: tc.have, Title: title}}, nil)

			require.NoError(t, o.Err)
			assert.Equal(t, tc.expected, o.Say.ID)
			assert.Equal(t, tc.params, o.Say.Params)
			assert.Equal(t, tc.call, lastCall(backend.Calls()))
			require.NotNil(t, o.Played)
			assert.Equal(t, tc.have, o.Played.Ref())
		})
	}
}

func TestSilentSuccessesAskForConfirmation(t *testing.T) {
	target, backend, _ := newTarget(testkit.SampleLibrary()...)
	backend.SetPlaying(&media.NowPlaying{Item: media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune"}})
	svc := NewService(slog.New(slog.DiscardHandler))

	testCases := []struct {
		name     string
		expected bool
		call     func(ctx context.Context) Outcome
	}{
		{name: "ShouldConfirmPause", expected: true, call: func(ctx context.Context) Outcome { return svc.Transport(ctx, target, media.TransportPause) }},
		{name: "ShouldConfirmVolumeUp", expected: true, call: func(ctx context.Context) Outcome { return svc.StepVolume(ctx, target, true) }},
		{name: "ShouldConfirmMute", expected: true, call: func(ctx context.Context) Outcome { return svc.ToggleMute(ctx, target) }},
		{name: "ShouldNotConfirmASetVolumeAsItSaysTheLevel", call: func(ctx context.Context) Outcome { return svc.SetVolume(ctx, target, 5) }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.call(t.Context())

			if !tc.expected {
				assert.False(t, o.Confirm, "says the level, so needs no sound")

				return
			}

			assert.True(t, o.Confirm)
			assert.Empty(t, o.Say.ID)
		})
	}

	backend.SetPlaying(nil)
	assert.False(t, svc.Transport(t.Context(), target, media.TransportPause).Confirm, "nothing playing is said, not confirmed")
}

func TestTargetAllows(t *testing.T) {
	all := Target{}
	noMovies := Target{Disabled: []media.Content{media.ContentMovies}}
	onlyMovies := Target{Disabled: []media.Content{media.ContentTV, media.ContentMusic}}

	testCases := []struct {
		name     string
		target   Target
		have     media.Kind
		expected bool
	}{
		{name: "ShouldAllowMoviesByDefault", target: all, have: media.KindMovie, expected: true},
		{name: "ShouldAllowTVShowsByDefault", target: all, have: media.KindTVShow, expected: true},
		{name: "ShouldAllowEpisodesByDefault", target: all, have: media.KindEpisode, expected: true},
		{name: "ShouldAllowSongsByDefault", target: all, have: media.KindSong, expected: true},
		{name: "ShouldAllowMusicVideosByDefault", target: all, have: media.KindMusicVideo, expected: true},
		{name: "ShouldAllowPlaylistsByDefault", target: all, have: media.KindPlaylist, expected: true},
		{name: "ShouldNotAllowDisabledMovies", target: noMovies, have: media.KindMovie},
		{name: "ShouldAllowEpisodesWhenMoviesAreDisabled", target: noMovies, have: media.KindEpisode, expected: true},
		{name: "ShouldNotAllowEpisodesAsTheyBelongToTV", target: onlyMovies, have: media.KindEpisode},
		{name: "ShouldNotAllowMusicVideosAsTheyBelongToMusic", target: onlyMovies, have: media.KindMusicVideo},
		{name: "ShouldAllowPlaylistsAsTheyBelongToNoGroup", target: onlyMovies, have: media.KindPlaylist, expected: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.target.Allows(tc.have))
		})
	}

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

	testCases := []struct {
		name    string
		methods []string
		playing bool
		setup   func(*testkit.Backend)
		call    call
	}{
		{name: "ShouldReportFailureInTransport", methods: []string{"Transport"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Transport(ctx, t, media.TransportPause)
		}},
		{name: "ShouldReportFailureInStepVolume", methods: []string{"Volume"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.StepVolume(ctx, t, true) }},
		{name: "ShouldReportFailureInToggleMute", methods: []string{"Volume"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.ToggleMute(ctx, t) }},
		{name: "ShouldReportFailureInNowPlaying", methods: []string{"NowPlaying"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.NowPlaying(ctx, t) }},
		{name: "ShouldReportFailureInSeek", methods: []string{"Seek"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Seek(ctx, t, SeekRequest{}) }},
		{name: "ShouldReportFailureInPicture", methods: []string{"Picture"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Picture(ctx, t, media.PictureZoomIn) }},
		{name: "ShouldReportFailureInToggle", methods: []string{"Input"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Toggle(ctx, t, media.InputFullscreen)
		}},
		{name: "ShouldReportFailureInTimeRemaining", methods: []string{"NowPlaying"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.TimeRemaining(ctx, t) }},
		{name: "ShouldReportFailureInNavigate", methods: []string{"Input"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Navigate(ctx, t, media.InputDown, 3) }},
		{name: "ShouldReportFailureInSendText", methods: []string{"SendText"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.SendText(ctx, t, "dune") }},
		{name: "ShouldReportFailureInSectionGenres", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ShowSection(ctx, t, media.SectionMovies, "comedy")
		}},
		{name: "ShouldReportFailureInSectionView", methods: []string{"ShowView"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ShowSection(ctx, t, media.SectionMovies, "")
		}},
		{name: "ShouldReportFailureInPlaylistView", methods: []string{"ShowView"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.ShowPlaylist(ctx, t, "road trip") }},
		{name: "ShouldReportFailureInFavourites", methods: []string{"Favourites"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.OpenFavourite(ctx, t, "radio") }},
		{name: "ShouldReportFailureInSubtitleStreams", methods: []string{"Streams"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Subtitles(ctx, t, StreamRequest{Language: "french"})
		}},
		{name: "ShouldReportFailureInSubtitleStreamsAfterTheChange", methods: []string{"Streams"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Subtitles(ctx, t, StreamRequest{Step: media.StreamNext})
		}},
		{name: "ShouldReportFailureInSetSubtitle", methods: []string{"SetSubtitle"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Subtitles(ctx, t, StreamRequest{Step: media.StreamOff})
		}},
		{name: "ShouldReportFailureInAudioStreams", methods: []string{"Streams"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.AudioStream(ctx, t, StreamRequest{Language: "french"})
		}},
		{name: "ShouldReportFailureInAudioStreamsAfterTheChange", methods: []string{"Streams"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.AudioStream(ctx, t, StreamRequest{Step: media.StreamNext})
		}},
		{name: "ShouldReportFailureInSetAudioStream", methods: []string{"SetAudioStream"}, playing: true, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.AudioStream(ctx, t, StreamRequest{Step: media.StreamNext})
		}},
		{name: "ShouldReportFailureInSubtitleSearch", methods: []string{"ShowView"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.DownloadSubtitles(ctx, t) }},
		{name: "ShouldReportFailureInPower", methods: []string{"System"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ConfirmPower(ctx, t, media.SystemReboot)
		}},
		{name: "ShouldReportFailureInEject", methods: []string{"System"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Eject(ctx, t) }},
		{name: "ShouldReportFailureInScan", methods: []string{"ScanLibrary"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.UpdateLibrary(ctx, t, false) }},
		{name: "ShouldReportFailureInClean", methods: []string{"CleanLibrary"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.UpdateLibrary(ctx, t, true) }},
		{name: "ShouldReportFailureInAddOns", methods: []string{"Addons"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.LaunchAddon(ctx, t, "youtube") }},
		{name: "ShouldReportFailureInLaunchAddOn", methods: []string{"ExecuteAddon"}, setup: withAddons, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.LaunchAddon(ctx, t, "youtube") }},
		{
			name:    "ShouldReportFailureInGlobalSearchAddOns",
			methods: []string{"Addons"},
			call: func(ctx context.Context, s *Service, t Target) Outcome {
				t.SearchMode = media.SearchModeGlobal

				return s.GlobalSearch(ctx, t, "dune", nil)
			},
		},
		{name: "ShouldReportFailureInLibrarySearch", methods: []string{"ShowView"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.GlobalSearch(ctx, t, "hot fuzz", nil)
		}},
		{name: "ShouldReportFailureInPlayTitle", methods: []string{"Play"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.PlayTitle(ctx, t, TitleRequest{Query: "hot fuzz"}, nil)
		}},
		{name: "ShouldReportFailureInNextEpisode", methods: []string{"NextEpisode"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.NextEpisode(ctx, t, "severance", nil, nil)
		}},
		{name: "ShouldReportFailureInAlbumDetails", methods: []string{"Item"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.PlayTitle(ctx, t, TitleRequest{Query: "abbey road", Kinds: []media.Kind{media.KindAlbum}}, nil)
		}},
		{name: "ShouldReportFailureInArtist", methods: []string{"Play"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Artist(ctx, t, MusicRequest{Artist: "queen"}, nil)
		}},
		{name: "ShouldReportFailureInArtistGenre", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Artist(ctx, t, MusicRequest{Artist: "queen", Genre: "rock"}, nil)
		}},
		{name: "ShouldReportFailureInArtistGenreQueue", methods: []string{"Queue"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Artist(ctx, t, MusicRequest{Artist: "queen", Genre: "rock"}, nil)
		}},
		{name: "ShouldReportFailureInAlbumLookup", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Album(ctx, t, MusicRequest{Album: "innuendo"}, nil)
		}},
		{name: "ShouldReportFailureInAlbum", methods: []string{"Play"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Album(ctx, t, MusicRequest{Album: "innuendo"}, nil)
		}},
		{name: "ShouldReportFailureInLatestAlbum", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.LatestAlbum(ctx, t, MusicRequest{Artist: "queen"}, nil)
		}},
		{name: "ShouldReportFailureInSongLookup", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Song(ctx, t, MusicRequest{Title: "come together"}, nil)
		}},
		{name: "ShouldReportFailureInSong", methods: []string{"Play"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Song(ctx, t, MusicRequest{Title: "come together"}, nil)
		}},
		{name: "ShouldReportFailureInSongOrAlbum", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.SongOrAlbum(ctx, t, MusicRequest{Title: "come together"}, nil)
		}},
		{name: "ShouldReportFailureInGenre", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Genre(ctx, t, "rock", nil) }},
		{name: "ShouldReportFailureInRecentSongs", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.RecentSongs(ctx, t, nil) }},
		{name: "ShouldReportFailureInPartyMode", methods: []string{"PartyMode"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.PartyMode(ctx, t, nil) }},
		{name: "ShouldReportFailureInMusicVideoLookup", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.MusicVideo(ctx, t, MusicRequest{Title: "bohemian rhapsody"}, nil)
		}},
		{name: "ShouldReportFailureInRandomMusicVideo", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.RandomMusicVideo(ctx, t, MusicRequest{}, nil)
		}},
		{name: "ShouldReportFailureInShuffleMusicVideos", methods: []string{"Queue"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ShuffleMusicVideos(ctx, t, MusicRequest{}, nil)
		}},
		{name: "ShouldReportFailureInPlaylist", methods: []string{"Play"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Playlist(ctx, t, "road trip", true, nil)
		}},
		{name: "ShouldReportFailureInRecommend", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Recommend(ctx, t, RecommendRequest{Kinds: []media.Kind{media.KindMovie}}, nil)
		}},
		{name: "ShouldReportFailureInRecommendEpisode", methods: []string{"NextEpisode"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Recommend(ctx, t, RecommendRequest{Kinds: []media.Kind{media.KindEpisode}}, nil)
		}},
		{name: "ShouldReportFailureInNewAlbums", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{Kind: media.KindAlbum})
		}},
		{name: "ShouldReportFailureInNewMovies", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.WhatsNew(ctx, t, WhatsNewRequest{}) }},
		{name: "ShouldReportFailureInUnwatchedEpisodes", methods: []string{"Episodes"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{Show: "severance"})
		}},
		{name: "ShouldReportFailureInNewEpisodes", methods: []string{"RecentlyAdded"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{Kind: media.KindEpisode})
		}},
		{name: "ShouldReportFailureInAlbumsBy", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.AlbumsBy(ctx, t, "queen") }},
		{name: "ShouldReportFailureInRandomMovie", methods: []string{"Play"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.RandomMovie(ctx, t, "", nil) }},
		{name: "ShouldReportFailureInRandomMoviePool", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.RandomMovie(ctx, t, "", nil) }},
		{name: "ShouldReportFailureInTrailerDetails", methods: []string{"Item"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Trailer(ctx, t, "hot fuzz", nil) }},
		{name: "ShouldReportFailureInTrailer", methods: []string{"PlayFile"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Trailer(ctx, t, "hot fuzz", nil) }},
		{name: "ShouldReportFailureInCollectionDetails", methods: []string{"Item"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.NextInCollection(ctx, t, "dune", nil)
		}},
		{name: "ShouldReportFailureInCollection", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.NextInCollection(ctx, t, "dune", nil)
		}},
		{name: "ShouldReportFailureInEpisode", methods: []string{"Episodes"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Episode(ctx, t, "severance", 2, 3, nil)
		}},
		{name: "ShouldReportFailureInFirstEpisode", methods: []string{"Episodes"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.FirstEpisode(ctx, t, "severance", nil)
		}},
		{name: "ShouldReportFailureInLatestEpisode", methods: []string{"Episodes"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.LatestEpisode(ctx, t, "severance", nil)
		}},
		{name: "ShouldReportFailureInContinueWatchingShows", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.ContinueWatching(ctx, t, nil) }},
		{name: "ShouldReportFailureInContinueWatching", methods: []string{"NextEpisode"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.ContinueWatching(ctx, t, nil) }},
		{name: "ShouldReportFailureInRandomEpisodeList", methods: []string{"Episodes"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.RandomEpisode(ctx, t, "bluey", nil) }},
		{name: "ShouldReportFailureInRandomEpisode", methods: []string{"Play"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.RandomEpisode(ctx, t, "bluey", nil) }},
		{name: "ShouldReportFailureInRandomShow", methods: []string{"NextEpisode"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.RandomShow(ctx, t, "", nil) }},
		{name: "ShouldReportFailureInRandomShowPool", methods: []string{"Library"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.RandomShow(ctx, t, "", nil) }},
		{name: "ShouldReportFailureInShuffleShow", methods: []string{"Play"}, call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.ShuffleShow(ctx, t, "severance", nil)
		}},
		{name: "ShouldReportFailureInBingeNext", methods: []string{"NextEpisode"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Binge(ctx, t, "severance", nil) }},
		{name: "ShouldReportFailureInBingeEpisodes", methods: []string{"Episodes"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Binge(ctx, t, "severance", nil) }},
		{name: "ShouldReportFailureInBingeQueue", methods: []string{"Queue"}, call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Binge(ctx, t, "severance", nil) }},
	}

	for _, tc := range testCases {
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

	testCases := []struct {
		name string
		call func(ctx context.Context) Outcome
	}{
		{name: "ShouldNameTheInstanceWhenSeeking", call: func(ctx context.Context) Outcome { return svc.Seek(ctx, target, SeekRequest{By: time.Minute}) }},
		{name: "ShouldNameTheInstanceWhenSettingTheVolume", call: func(ctx context.Context) Outcome { return svc.SetVolume(ctx, target, 3) }},
		{name: "ShouldNameTheInstanceWhenOpeningAFavourite", call: func(ctx context.Context) Outcome { return svc.OpenFavourite(ctx, target, "radio national") }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.call(t.Context())
			assert.Equal(t, "error.kodi_unreachable", o.Say.ID)
			assert.Equal(t, "Lounge", o.Say.Params["kodi"])
			require.ErrorIs(t, o.Err, media.ErrUnreachable)
		})
	}
}

func TestMatcherFailuresAreReported(t *testing.T) {
	testCases := []struct {
		name string
		call func(context.Context, *Service, Target) Outcome
	}{
		{name: "ShouldReportFailureInPlayTitle", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.PlayTitle(ctx, t, TitleRequest{Query: "dune"}, nil)
		}},
		{name: "ShouldReportFailureInArtist", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Artist(ctx, t, MusicRequest{Artist: "queen"}, nil)
		}},
		{name: "ShouldReportFailureInAlbum", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Album(ctx, t, MusicRequest{Album: "innuendo"}, nil)
		}},
		{name: "ShouldReportFailureInSong", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Song(ctx, t, MusicRequest{Title: "x"}, nil)
		}},
		{name: "ShouldReportFailureInSongOrAlbum", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.SongOrAlbum(ctx, t, MusicRequest{Title: "x"}, nil)
		}},
		{name: "ShouldReportFailureInMusicVideo", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.MusicVideo(ctx, t, MusicRequest{Title: "x"}, nil)
		}},
		{name: "ShouldReportFailureInLatestAlbum", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.LatestAlbum(ctx, t, MusicRequest{Artist: "queen"}, nil)
		}},
		{name: "ShouldReportFailureInAlbumsBy", call: func(ctx context.Context, s *Service, t Target) Outcome { return s.AlbumsBy(ctx, t, "queen") }},
		{name: "ShouldReportFailureInPlaylist", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Playlist(ctx, t, "road trip", false, nil)
		}},
		{name: "ShouldReportFailureInShowPlaylist", call: func(ctx context.Context, s *Service, t Target) Outcome { return s.ShowPlaylist(ctx, t, "road trip") }},
		{name: "ShouldReportFailureInTrailer", call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Trailer(ctx, t, "hot fuzz", nil) }},
		{name: "ShouldReportFailureInCollection", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.NextInCollection(ctx, t, "dune", nil)
		}},
		{name: "ShouldReportFailureInEpisode", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.Episode(ctx, t, "severance", 1, 1, nil)
		}},
		{name: "ShouldReportFailureInBinge", call: func(ctx context.Context, s *Service, t Target) Outcome { return s.Binge(ctx, t, "severance", nil) }},
		{name: "ShouldReportFailureInUnwatched", call: func(ctx context.Context, s *Service, t Target) Outcome {
			return s.WhatsNew(ctx, t, WhatsNewRequest{Show: "severance"})
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("ShouldSendNothingToKodi", func(t *testing.T) {
				target, backend, _ := newTarget()
				target.Matcher = &brokenMatcher{}

				assertFailed(t, tc.call(t.Context(), newService(), target))
				assert.Empty(t, backend.Calls(), "nothing is sent to Kodi")
			})

			t.Run("ShouldSearchTheIndexAgainAfterALiveLookup", func(t *testing.T) {
				target, _, _ := newTarget()
				m := &brokenMatcher{ok: 1}
				target.Matcher = m
				target.Library = &lookupLibrary{found: []media.Item{{Kind: media.KindMovie, ID: 9, Title: "New"}}}

				assertFailed(t, tc.call(t.Context(), newService(), target))
				assert.Equal(t, 2, m.searches, "the index is searched again after the lookup found something")
			})
		})
	}
}

func TestAskKeepsMoreMatchesForNo(t *testing.T) {
	cands := func(scores ...float64) match.Result {
		res := match.Result{AskScore: match.DefaultConfig().AskScore}

		for i, s := range scores {
			id := strconv.Itoa(i + 1)
			res.Candidates = append(res.Candidates, match.Candidate{Doc: match.Doc{ID: "movie/" + id, Ref: media.Ref{Kind: media.KindMovie, ID: i + 1}, Title: "Title " + id}, Score: s})
		}

		return res
	}

	ids := func(choices []Choice) []string {
		var out []string
		for _, c := range choices {
			out = append(out, c.DocID)
		}

		return out
	}

	o := ask(cands(0.8, 0.8, 0.7, 0.7, 0.7, 0.7, 0.7, 0.7))
	assert.Equal(t, []string{"movie/1", "movie/2"}, ids(o.Pending))
	require.NotNil(t, o.Followup)
	assert.Equal(t, []string{"movie/3", "movie/4", "movie/5", "movie/6"}, ids(o.Followup.More), "a question keeps at most maxCandidates titles")

	o = ask(cands(0.65, 0.5, 0.7, 0.3))
	assert.Equal(t, []string{"movie/1", "movie/2"}, ids(o.Pending), "the titles asked about are offered whatever their score")
	require.NotNil(t, o.Followup)
	assert.Equal(t, []string{"movie/3"}, ids(o.Followup.More), "later titles stop at the first one too weak to ask about")

	o = ask(cands(0.7, 0.7, 0.3))
	assert.Nil(t, o.Followup, "nothing is kept back when the rest are too weak")

	// The bar is the one the search used: the matcher's configured AskScore, or the higher one a native alias winner needs.
	raised := cands(0.9, 0.9, 0.7)
	raised.AskScore = 0.75
	assert.Nil(t, ask(raised).Followup, "a title the search wouldn't have asked about isn't offered after no")

	raised.AskScore = 0.6
	require.NotNil(t, ask(raised).Followup)

	o = newService().OtherChoices(o.Pending)
	assert.Equal(t, "match.did_you_mean", o.Say.ID, "no offers the titles kept back")
	assert.False(t, o.EndSession)

	o = newService().OtherChoices(nil)
	assert.Equal(t, "match.no_more", o.Say.ID)
	assert.True(t, o.EndSession)
}

func TestChoose(t *testing.T) {
	old := Choice{DocID: "movie/1", Ref: media.Ref{Kind: media.KindMovie, ID: 1}, Title: "Dune", Year: 1984}
	remake := Choice{DocID: "movie/2", Ref: media.Ref{Kind: media.KindMovie, ID: 2}, Title: "Dune", Year: 2021}
	sequel := Choice{DocID: "movie/3", Ref: media.Ref{Kind: media.KindMovie, ID: 3}, Title: "Dune: Part Two", Year: 2024}
	undated := Choice{DocID: "playlist/70", Ref: media.Ref{Kind: media.KindPlaylist, ID: 70}, Title: "Road Trip"}
	sameYear := Choice{DocID: "movie/4", Ref: media.Ref{Kind: media.KindMovie, ID: 4}, Title: "Hot Fuzz", Year: 2021}
	album := Choice{DocID: "album/80", Ref: media.Ref{Kind: media.KindAlbum, ID: 80}, Title: "Odyssey", Year: 2005}
	song := Choice{DocID: "song/81", Ref: media.Ref{Kind: media.KindSong, ID: 81}, Title: "Odyssey", Year: 2024}
	film := Choice{DocID: "movie/82", Ref: media.Ref{Kind: media.KindMovie, ID: 82}, Title: "The Odyssey", Year: 2026}
	orwell := Choice{DocID: "movie/83", Ref: media.Ref{Kind: media.KindMovie, ID: 83}, Title: "1984", Year: 1956}
	brazil := Choice{DocID: "movie/84", Ref: media.Ref{Kind: media.KindMovie, ID: 84}, Title: "Brazil", Year: 1984}

	testCases := []struct {
		name     string
		have     Answer
		pending  []Choice
		more     []Choice
		expected string
		again    bool
	}{
		{name: "ShouldPlayTheFirst", have: Answer{Which: AnswerFirst}, pending: []Choice{old, remake}, expected: "movie/1"},
		{name: "ShouldPlayTheSecond", have: Answer{Which: AnswerSecond}, pending: []Choice{old, remake}, expected: "movie/2"},
		{name: "ShouldAskAgainForTheSecondOfOne", have: Answer{Which: AnswerSecond}, pending: []Choice{old}, again: true},
		{name: "ShouldPlayTheLast", have: Answer{Which: AnswerLast}, pending: []Choice{old, remake}, expected: "movie/2"},
		{name: "ShouldPlayTheNewer", have: Answer{Which: AnswerNewer}, pending: []Choice{remake, old}, expected: "movie/2"},
		{name: "ShouldPlayTheOlder", have: Answer{Which: AnswerOlder}, pending: []Choice{remake, old}, expected: "movie/1"},
		{name: "ShouldAskAgainForTheNewerOfOne", have: Answer{Which: AnswerNewer}, pending: []Choice{old}, again: true},
		{name: "ShouldAskAgainForTheNewerOfTheSameYear", have: Answer{Which: AnswerNewer}, pending: []Choice{remake, sameYear}, again: true},
		{name: "ShouldAskAgainForTheOlderWithoutAYear", have: Answer{Which: AnswerOlder}, pending: []Choice{old, undated}, again: true},
		{name: "ShouldAskAgainForAnUnknownPosition", have: Answer{Which: "third"}, pending: []Choice{old, remake}, again: true},
		{name: "ShouldPreferThePositionToTheTitle", have: Answer{Which: AnswerFirst, Title: "hot fuzz"}, pending: []Choice{old, remake}, expected: "movie/1"},
		{name: "ShouldAskAgainForNothing", pending: []Choice{old, remake}, again: true},
		{name: "ShouldAskAgainWithoutChoices", have: Answer{Which: AnswerFirst}, again: true},
		{name: "ShouldPlayATitleWithItsYear", have: Answer{Title: "Dune 1984"}, pending: []Choice{old, remake}, expected: "movie/1"},
		{name: "ShouldPlayATitleKeptBack", have: Answer{Title: "dune part two"}, pending: []Choice{old, remake}, more: []Choice{sequel}, expected: "movie/3"},
		{name: "ShouldAskAgainForASharedTitle", have: Answer{Title: "dune"}, pending: []Choice{old, remake}, again: true},
		{name: "ShouldPlayAnotherTitle", have: Answer{Title: "hot fuzz"}, pending: []Choice{old, remake}, expected: "movie/4"},
		// An article said picks the title that starts with it, and a year may follow "from", as the matcher reads them.
		{name: "ShouldPlayATitleKeptBackStartingWithTheArticle", have: Answer{Title: "the odyssey"}, pending: []Choice{album, song}, more: []Choice{film}, expected: "movie/82"},
		{name: "ShouldPlayATitleKeptBackWithAYearAfterFrom", have: Answer{Title: "the odyssey from 2026"}, pending: []Choice{album, song}, more: []Choice{film}, expected: "movie/82"},
		{name: "ShouldPlayATitleOfferedWithAYearAfterFrom", have: Answer{Title: "dune from 1984"}, pending: []Choice{old, remake}, expected: "movie/1"},
		{name: "ShouldAskAgainForATitleSharedWithoutTheArticle", have: Answer{Title: "odyssey"}, pending: []Choice{album, song}, more: []Choice{film}, again: true},
		{name: "ShouldPlayAKindKeptBack", have: Answer{Title: "hot fuzz"}, pending: []Choice{album, song}, more: []Choice{film}, expected: "movie/4"},
		// A year said with the answer ("dune from 2021", "the one from 1984") picks the choice from that year.
		{name: "ShouldPlayTheTitleFromTheYear", have: Answer{Title: "dune", Year: 2021}, pending: []Choice{old, remake}, expected: "movie/2"},
		{name: "ShouldPlayTheChoiceFromTheYear", have: Answer{Year: 1984}, pending: []Choice{old, remake}, expected: "movie/1"},
		{name: "ShouldPlayTheChoiceKeptBackFromTheYear", have: Answer{Year: 2026}, pending: []Choice{album, song}, more: []Choice{film}, expected: "movie/82"},
		{name: "ShouldPlayTheTitleWithItsArticleFromTheYear", have: Answer{Title: "the odyssey", Year: 2026}, pending: []Choice{album, song}, more: []Choice{film}, expected: "movie/82"},
		{name: "ShouldPlayTheTitleWithoutItsArticleFromTheYear", have: Answer{Title: "odyssey", Year: 2026}, pending: []Choice{album, song}, more: []Choice{film}, expected: "movie/82"},
		{name: "ShouldAskAgainForASharedYear", have: Answer{Year: 2021}, pending: []Choice{remake, sameYear}, again: true},
		{name: "ShouldAskAgainForTheYearOfNone", have: Answer{Year: 1999}, pending: []Choice{old, remake}, again: true},
		{name: "ShouldPreferThePositionToTheYear", have: Answer{Which: AnswerSecond, Year: 1984}, pending: []Choice{old, remake}, expected: "movie/2"},
		// A year heard as the title ("die uit 1984" as the answer "1984") picks the choice from that year, unless a choice is called that.
		{name: "ShouldTakeAYearHeardAsTheTitleAsTheYear", have: Answer{Title: "1984"}, pending: []Choice{old, remake}, expected: "movie/1"},
		{name: "ShouldPlayATitleThatIsAYear", have: Answer{Title: "1984"}, pending: []Choice{orwell, brazil}, expected: "movie/83"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()

			o := newService().Choose(t.Context(), target, tc.have, tc.pending, tc.more, nil)

			assert.Equal(t, tc.again, o.AskAgain)

			if tc.again {
				assert.Equal(t, "match.which_one", o.Say.ID)
				assert.Empty(t, backend.Calls())

				return
			}

			assert.Equal(t, []string{"Play " + tc.expected + " resume"}, backend.Calls())
		})
	}
}

func TestAnswerTitle(t *testing.T) {
	old := Choice{DocID: "movie/1", Ref: media.Ref{Kind: media.KindMovie, ID: 1}, Title: "Dune", Year: 1984}
	remake := Choice{DocID: "movie/2", Ref: media.Ref{Kind: media.KindMovie, ID: 2}, Title: "Dune", Year: 2021}
	sequel := Choice{DocID: "movie/3", Ref: media.Ref{Kind: media.KindMovie, ID: 3}, Title: "Dune: Part Two", Year: 2024}

	testCases := []struct {
		name     string
		have     string
		pending  []Choice
		more     []Choice
		expected string
	}{
		{name: "ShouldNotAnswerWithoutAQuestion", have: "dune 1984"},
		{name: "ShouldNotAnswerWithoutATitle", pending: []Choice{old, remake}},
		{name: "ShouldPlayAChoiceWithItsYear", have: "dune from 2021", pending: []Choice{old, remake}, expected: "movie/2"},
		{name: "ShouldPlayAChoiceKeptBack", have: "dune part two", pending: []Choice{old, remake}, more: []Choice{sequel}, expected: "movie/3"},
		{name: "ShouldPlayAChoiceByOnlyItsYear", have: "1984", pending: []Choice{old, remake}, expected: "movie/1"},
		{name: "ShouldNotAnswerWithATitleSeveralShare", have: "dune", pending: []Choice{old, remake}},
		{name: "ShouldNotAnswerWithAnotherTitle", have: "hot fuzz", pending: []Choice{old, remake}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()

			o, ok := newService().AnswerTitle(t.Context(), target, tc.have, tc.pending, tc.more, nil)

			if tc.expected == "" {
				assert.False(t, ok, "not an answer: the intent searches as it would")
				assert.Empty(t, backend.Calls())

				return
			}

			require.True(t, ok)
			assert.Equal(t, "playback.playing_movie", o.Say.ID)
			assert.Equal(t, []string{"Play " + tc.expected + " resume"}, backend.Calls())
		})
	}
}

func TestChooseSearchesTheKindsOffered(t *testing.T) {
	target, backend, _ := newTarget()
	show := Choice{DocID: "tvshow/20", Ref: media.Ref{Kind: media.KindTVShow, ID: 20}, Title: "Bluey", Year: 2018}

	o := newService().Choose(t.Context(), target, Answer{Title: "hot fuzz"}, []Choice{show, show}, nil, nil)

	assert.Equal(t, "match.not_found", o.Say.ID, "a film isn't found among shows")
	assert.Empty(t, backend.Calls())
}

func TestOutcomeMissed(t *testing.T) {
	svc := NewService(slog.New(slog.DiscardHandler))
	tgt, _, _ := newTarget()
	ctx := context.Background()

	testCases := []struct {
		name     string
		expected bool
		call     func() Outcome
	}{
		{name: "ShouldMissNoTitleByThatName", expected: true, call: func() Outcome { return svc.PlayTitle(ctx, tgt, TitleRequest{Query: "conclave"}, nil) }},
		{name: "ShouldMissASongNotByThatArtist", expected: true, call: func() Outcome { return svc.Song(ctx, tgt, MusicRequest{Title: "come together", Artist: "queen"}, nil) }},
		{name: "ShouldMissNoSuchGenreByTheArtist", expected: true, call: func() Outcome { return svc.Artist(ctx, tgt, MusicRequest{Artist: "queen", Genre: "jazz"}, nil) }},
		{name: "ShouldMissDisabledContent", expected: true, call: ContentDisabled},
		{name: "ShouldNotMissATitleThatPlays", call: func() Outcome { return svc.PlayTitle(ctx, tgt, TitleRequest{Query: "hot fuzz"}, nil) }},
		{name: "ShouldNotMissAQuestionAsItIsAnAnswer", call: func() Outcome { return svc.PlayTitle(ctx, tgt, TitleRequest{Query: "dune"}, nil) }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.call().Missed())
		})
	}
}

func TestRewindOrBackWhenKodiIsUnreachable(t *testing.T) {
	target, backend, _ := newTarget()
	backend.Err = media.ErrUnreachable

	o := newService().RewindOrBack(t.Context(), target)

	assert.Equal(t, "error.kodi_unreachable", o.Say.ID)
	require.ErrorIs(t, o.Err, media.ErrUnreachable)
	assert.Equal(t, []string{"NowPlaying"}, backend.Calls(), "nothing is pressed or sought")
}

// A title Kodi names with its year already isn't given the year twice: "Avatar: The Last Airbender (2024)", not "… (2024) from 2024".
func TestDescribeDoesntRepeatAYearInTheTitle(t *testing.T) {
	m := describe(Message{}, "title", Choice{Title: "Avatar: The Last Airbender (2024)", Year: 2024})
	assert.Equal(t, "Avatar: The Last Airbender (2024)", m.Params["title"])
	assert.Empty(t, m.Messages)

	m = describe(Message{}, "title", Choice{Title: "Avatar: The Last Airbender", Year: 2005})
	assert.Equal(t, say("match.title_year", map[string]string{"title": "Avatar: The Last Airbender", "year": "2005"}), m.Messages["title"], "a title without one gets it")
}
