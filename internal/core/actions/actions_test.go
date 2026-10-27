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
	assert.Equal(t, "Dune from 1984", o.Say.Params["first"])
	assert.Equal(t, "Dune from 2021", o.Say.Params["second"])
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
