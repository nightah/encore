package actions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/livetv"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

func melbourneNow(t *testing.T) time.Time {
	t.Helper()

	loc, err := time.LoadLocation("Australia/Melbourne")
	require.NoError(t, err)

	return time.Date(2026, 11, 11, 21, 26, 0, 0, loc)
}

// liveTarget is a target whose Kodi instance has Melbourne's channels, numbered as Australian TV numbers them.
func liveTarget(t *testing.T) (Target, *testkit.Backend) {
	t.Helper()

	now := melbourneNow(t)
	slot := func(title string, from, to time.Duration) *media.Broadcast {
		return &media.Broadcast{Title: title, Start: now.Add(from), End: now.Add(to)}
	}

	target, backend, _ := newTarget()
	backend.SetChannels(
		media.Channel{ID: 10, Name: "10", Major: 10, Now: slot("Boston Blue", -10*time.Minute, 20*time.Minute)},
		media.Channel{ID: 21, Name: "ABC TV", Major: 21, Now: slot("Austin", -5*time.Minute, 25*time.Minute), Next: slot("Gardening Australia", 25*time.Minute, 55*time.Minute)},
		media.Channel{ID: 22, Name: "ABC Kids", Major: 22, Now: slot("Bluey", -5*time.Minute, 2*time.Minute)},
		media.Channel{ID: 24, Name: "ABC NEWS", Major: 24, Now: slot("The World", -10*time.Minute, 20*time.Minute), Next: slot("Planet America", 20*time.Minute, 80*time.Minute)},
		media.Channel{ID: 30, Name: "SBS", Major: 30},
		media.Channel{ID: 71, Name: "Seven", Major: 71, Now: slot("House Of Wellness", -10*time.Minute, 20*time.Minute)},
		media.Channel{ID: 93, Name: "9Go!", Major: 93},
		media.Channel{ID: 990, Name: "Late Night Movies", Major: 990, Locked: true},
	)
	target.Guide = livetv.New(backend, "en", livetv.Options{Now: func() time.Time { return now }})

	return target, backend
}

func TestWatchChannel(t *testing.T) {
	target, backend := liveTarget(t)

	o := newService().WatchChannel(t.Context(), target, "nine go", nil)

	require.NoError(t, o.Err)
	assert.Equal(t, say("livetv.switching", map[string]string{"channel": "9Go!"}), o.Say)
	assert.True(t, o.EndSession)
	require.NotNil(t, o.Played)
	assert.Equal(t, media.Item{Kind: media.KindChannel, ID: 93, Title: "9Go!"}, *o.Played)
	assert.Contains(t, backend.Calls(), "PlayChannel 93")
	require.NotNil(t, o.Match, "the name search is logged")
}

func TestWatchChannelNameBeforeNumber(t *testing.T) {
	target, backend := liveTarget(t)

	o := newService().WatchChannel(t.Context(), target, "ten", nil)
	assert.Equal(t, "10", o.Say.Params["channel"])

	o = newService().WatchChannel(t.Context(), target, "twenty four", nil)
	assert.Equal(t, "ABC NEWS", o.Say.Params["channel"], "no channel is named 24, so channel number 24")
	assert.Contains(t, backend.Calls(), "PlayChannel 24")
}

func TestWatchChannelAsksThenChooses(t *testing.T) {
	target, backend := liveTarget(t)

	o := newService().WatchChannel(t.Context(), target, "abc", nil)
	require.Equal(t, "match.did_you_mean", o.Say.ID)
	assert.False(t, o.EndSession)
	require.Len(t, o.Pending, 2)
	assert.Equal(t, Choice{DocID: "channel/21", Ref: media.Ref{Kind: media.KindChannel, ID: 21}, Title: "ABC TV"}, o.Pending[0])
	assert.NotContains(t, backend.Calls(), "PlayChannel 21", "nothing tunes until the user answers")

	o = newService().ChooseFirst(t.Context(), target, o.Pending, nil)
	assert.Equal(t, say("livetv.switching", map[string]string{"channel": "ABC TV"}), o.Say)
	assert.Contains(t, backend.Calls(), "PlayChannel 21")
}

func TestWatchChannelNotFound(t *testing.T) {
	target, _ := liveTarget(t)

	o := newService().WatchChannel(t.Context(), target, "zzz qqq", nil)
	assert.Equal(t, say("livetv.channel_not_found", map[string]string{"channel": "zzz qqq"}), o.Say)
}

func TestWatchChannelLocked(t *testing.T) {
	target, backend := liveTarget(t)

	o := newService().WatchChannel(t.Context(), target, "late night movies", nil)
	assert.Equal(t, say("livetv.locked", map[string]string{"channel": "Late Night Movies"}), o.Say)
	assert.NotContains(t, backend.Calls(), "PlayChannel 990")
	assert.Nil(t, o.Played)
}

func TestWatchChannelWithoutLiveTV(t *testing.T) {
	unavailable := say("livetv.unavailable", map[string]string{"kodi": "Lounge"})

	target, _, _ := newTarget()
	assert.Equal(t, unavailable, newService().WatchChannel(t.Context(), target, "seven", nil).Say, "no guide")

	target.Guide = livetv.New(target.Backend, "en", livetv.Options{})
	assert.Equal(t, unavailable, newService().WatchChannel(t.Context(), target, "seven", nil).Say, "Kodi has no PVR client")
	assert.Equal(t, unavailable, newService().OpenTVGuide(t.Context(), target, media.SectionTVGuide, nil).Say)
}

func TestWatchChannelUnreachable(t *testing.T) {
	target, backend := liveTarget(t)
	backend.Err = media.ErrUnreachable

	o := newService().WatchChannel(t.Context(), target, "seven", nil)
	assert.Equal(t, "error.kodi_unreachable", o.Say.ID)
	assert.ErrorIs(t, o.Err, media.ErrUnreachable)
}

func TestWatchChannelNumber(t *testing.T) {
	target, backend := liveTarget(t)

	testCases := []struct {
		name     string
		major    int
		minor    int
		expected Message
		call     string
	}{
		{name: "ShouldTuneToTheChannelNumbered", major: 71, minor: -1, expected: say("livetv.switching", map[string]string{"channel": "Seven"}), call: "PlayChannel 71"},
		{name: "ShouldNotTakeANumberForTheChannelNamedForIt", major: 7, minor: -1, expected: say("livetv.number_not_found", map[string]string{"number": "7"})},
		{name: "ShouldSayAMissingSubchannel", major: 7, minor: 2, expected: say("livetv.number_not_found", map[string]string{"number": "7.2"})},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := newService().WatchChannelNumber(t.Context(), target, tc.major, tc.minor, nil)
			assert.Equal(t, tc.expected, o.Say)

			if tc.call != "" {
				assert.Contains(t, backend.Calls(), tc.call)
			}
		})
	}
}

func TestOpenTVGuide(t *testing.T) {
	target, backend := liveTarget(t)

	o := newService().OpenTVGuide(t.Context(), target, media.SectionTVGuide, nil)
	assert.True(t, o.Confirm)
	assert.Contains(t, backend.Calls(), "ShowView tvguide")
}

func TestWhatsOn(t *testing.T) {
	target, _ := liveTarget(t)
	now := melbourneNow(t)

	o := newService().WhatsOn(t.Context(), target, "abc news", nil)
	want := say("livetv.now_next", map[string]string{"channel": "ABC NEWS", "title": "The World", "next": "Planet America"})
	want.Times = map[string]time.Time{"end": now.Add(20 * time.Minute)}
	assert.Equal(t, want, o.Say)

	o = newService().WhatsOn(t.Context(), target, "seven", nil)
	assert.Equal(t, "livetv.now", o.Say.ID, "the guide has nothing on next")

	o = newService().WhatsOn(t.Context(), target, "sbs", nil)
	assert.Equal(t, say("livetv.no_guide", map[string]string{"channel": "SBS"}), o.Say)

	o = newService().WhatsOn(t.Context(), target, "abc", nil)
	assert.Equal(t, "ABC TV", o.Say.Params["channel"], "a name that might be several channels is taken as the likeliest")
}

func TestWhatsOnPlaying(t *testing.T) {
	target, backend := liveTarget(t)

	o := newService().WhatsOn(t.Context(), target, "", nil)
	assert.Equal(t, "livetv.which_channel", o.Say.ID, "not watching TV")
	assert.False(t, o.EndSession)
	require.NotNil(t, o.Reprompt)

	require.NoError(t, backend.PlayChannel(t.Context(), 21))

	o = newService().WhatsOn(t.Context(), target, "", nil)
	assert.Equal(t, "livetv.now_next", o.Say.ID)
	assert.Equal(t, "ABC TV", o.Say.Params["channel"], "the channel playing")
}

func TestWatchLive(t *testing.T) {
	target, backend := liveTarget(t)
	now := melbourneNow(t)

	later := say("livetv.programme_later", map[string]string{"channel": "ABC TV", "title": "Gardening Australia"})
	later.Times = map[string]time.Time{"start": now.Add(25 * time.Minute)}

	testCases := []struct {
		name     string
		have     string
		expected Message
		call     string
	}{
		{name: "ShouldTuneToTheProgrammeOnNow", have: "house of wellness", expected: say("livetv.switching_programme", map[string]string{"channel": "Seven", "title": "House Of Wellness"}), call: "PlayChannel 71"},
		{name: "ShouldSayWhenAProgrammeStartsLater", have: "gardening australia", expected: later},
		{name: "ShouldTuneToAChannelsNameWhenNoProgrammeHasIt", have: "abc news", expected: say("livetv.switching", map[string]string{"channel": "ABC NEWS"})},
		{name: "ShouldSayAProgrammeIsntOn", have: "neighbours", expected: say("livetv.programme_not_found", map[string]string{"title": "neighbours"})},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := newService().WatchLive(t.Context(), target, tc.have, nil)
			assert.Equal(t, tc.expected, o.Say)

			if tc.call != "" {
				assert.Contains(t, backend.Calls(), tc.call)
			}
		})
	}
}

func TestTuneIfChannel(t *testing.T) {
	target, backend := liveTarget(t)

	o, ok := newService().TuneIfChannel(t.Context(), target, "abc news", nil)
	require.True(t, ok)
	assert.Equal(t, "livetv.switching", o.Say.ID)
	assert.Contains(t, backend.Calls(), "PlayChannel 24")

	_, ok = newService().TuneIfChannel(t.Context(), target, "abc", nil)
	assert.False(t, ok, "never asks")

	target.Disabled = []media.Content{media.ContentLiveTV}
	_, ok = newService().TuneIfChannel(t.Context(), target, "abc news", nil)
	assert.False(t, ok, "the skill was set up without live TV")

	target, _, _ = newTarget()
	_, ok = newService().TuneIfChannel(t.Context(), target, "abc news", nil)
	assert.False(t, ok, "no guide")
}

func TestTuneStaleChannel(t *testing.T) {
	target, backend := liveTarget(t)

	o := newService().playChoice(t.Context(), target, Choice{DocID: "channel/5", Ref: media.Ref{Kind: media.KindChannel, ID: 5}, Title: "Gone"}, nil)
	assert.Equal(t, say("livetv.channel_not_found", map[string]string{"channel": "Gone"}), o.Say, "a channel removed since the question")
	assert.Contains(t, backend.Calls(), "PlayChannel 5")
	assert.ErrorIs(t, backend.PlayChannel(t.Context(), 5), media.ErrNotFound)
}

func TestLibraryLimited(t *testing.T) {
	testCases := []struct {
		name     string
		have     Target
		expected bool
	}{
		{name: "ShouldNotBeLimitedWithNothingDisabled"},
		{name: "ShouldNotBeLimitedByLiveTVAsItIsntTheLibrary", have: Target{Disabled: []media.Content{media.ContentLiveTV}}},
		{name: "ShouldBeLimitedByMusic", have: Target{Disabled: []media.Content{media.ContentMusic, media.ContentLiveTV}}, expected: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.LibraryLimited())
		})
	}
}

// TestLiveTVFailsPartWay has Kodi fail after the guide already knows what it was asked first, so each step's failure is the one met.
func TestLiveTVFailsPartWay(t *testing.T) {
	boom := errors.New("kodi said no")

	testCases := []struct {
		name  string
		known func(*testing.T, Target) // what the guide already holds
		ask   func(context.Context, *Service, Target) Outcome
	}{
		{name: "ShouldFailTuning", known: knowList, ask: func(ctx context.Context, s *Service, tg Target) Outcome {
			return s.WatchChannel(ctx, tg, "abc news", nil)
		}},
		{name: "ShouldFailOpeningTheGuide", known: knowLive, ask: func(ctx context.Context, s *Service, tg Target) Outcome {
			return s.OpenTVGuide(ctx, tg, media.SectionTVGuide, nil)
		}},
		{name: "ShouldFailListingTheChannelsByNumber", known: knowLive, ask: func(ctx context.Context, s *Service, tg Target) Outcome {
			return s.WatchChannelNumber(ctx, tg, 71, -1, nil)
		}},
		{name: "ShouldFailListingTheChannelsForWhatsOn", known: knowLive, ask: func(ctx context.Context, s *Service, tg Target) Outcome { return s.WhatsOn(ctx, tg, "abc news", nil) }},
		{name: "ShouldFailWhatsOnOnceTheChannelIsKnown", known: knowList, ask: func(ctx context.Context, s *Service, tg Target) Outcome { return s.WhatsOn(ctx, tg, "abc news", nil) }},
		{name: "ShouldFailWhatsOnForAProgramme", known: knowLive, ask: func(ctx context.Context, s *Service, tg Target) Outcome { return s.WatchLive(ctx, tg, "austin", nil) }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend := liveTarget(t)
			tc.known(t, target)

			backend.Err = boom

			o := tc.ask(t.Context(), newService(), target)
			assert.Equal(t, "error.generic", o.Say.ID)
			require.ErrorIs(t, o.Err, boom)
		})
	}
}

func knowLive(t *testing.T, target Target) {
	t.Helper()

	live, err := target.Guide.Available(t.Context())
	require.NoError(t, err)
	require.True(t, live)
}

func knowList(t *testing.T, target Target) {
	t.Helper()

	knowLive(t, target)

	_, err := target.Guide.Find(t.Context(), "abc news")
	require.NoError(t, err)
}

func TestLiveTVGoneAfterTheCheck(t *testing.T) {
	target, backend := liveTarget(t)
	knowLive(t, target)

	backend.SetChannels() // the TV service stopped since Kodi said it had live TV

	o := newService().WatchChannel(t.Context(), target, "abc news", nil)
	assert.Equal(t, say("livetv.unavailable", map[string]string{"kodi": "Lounge"}), o.Say)
	assert.NoError(t, o.Err)
}

func TestTuneIfChannelWithoutLiveTV(t *testing.T) {
	target, _, _ := newTarget()
	target.Guide = livetv.New(target.Backend, "en", livetv.Options{})

	_, ok := newService().TuneIfChannel(t.Context(), target, "abc news", nil)
	assert.False(t, ok, "Kodi has no PVR client")
}

func TestWhatsOnNoSuchChannel(t *testing.T) {
	target, _ := liveTarget(t)

	o := newService().WhatsOn(t.Context(), target, "zzz qqq", nil)
	assert.Equal(t, say("livetv.channel_not_found", map[string]string{"channel": "zzz qqq"}), o.Say)
}

func TestWhatsOnChannelGoneFromTheGuide(t *testing.T) {
	target, backend := liveTarget(t)
	knowList(t, target)

	backend.SetChannels(media.Channel{ID: 10, Name: "10", Major: 10})

	o := newService().WhatsOn(t.Context(), target, "abc news", nil)
	assert.Equal(t, say("livetv.no_guide", map[string]string{"channel": "abc news"}), o.Say, "the channel list still has ABC NEWS, but the guide doesn't, so it is named as it was said")
}

func TestWatchChannelAsksWhichWhenNoneWasSaid(t *testing.T) {
	target, backend := liveTarget(t)

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldAskWhenAlexaHeardNoName"},
		{name: "ShouldAskWhenAlexaHeardANumberItCouldntRead", have: "?"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := newService().WatchChannel(t.Context(), target, tc.have, nil)
			assert.Equal(t, say("livetv.which_channel_to_watch", nil), o.Say)
			assert.False(t, o.EndSession)
			require.NotNil(t, o.Reprompt)
		})
	}

	for _, call := range backend.Calls() {
		assert.NotContains(t, call, "Channels", "nothing to look for")
	}
}
