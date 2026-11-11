package alexa

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/livetv"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

// withLiveTV is a handler whose lounge Kodi instance has live TV, with the content groups disabled left out of the skill.
func withLiveTV(t *testing.T, disabled ...media.Content) (*Handler, *testkit.Backend) {
	t.Helper()

	loc, err := time.LoadLocation("Australia/Melbourne")
	require.NoError(t, err)

	now := time.Date(2026, 11, 11, 21, 26, 0, 0, loc)
	h, backend := withDisabled(t, false, disabled...)
	backend.SetChannels(
		media.Channel{ID: 21, Name: "ABC TV", Major: 21, Now: &media.Broadcast{Title: "Austin", Start: now.Add(-5 * time.Minute), End: now.Add(25 * time.Minute)}},
		media.Channel{ID: 24, Name: "ABC NEWS", Major: 24},
		media.Channel{ID: 32, Name: "SBS World Movies", Major: 32, SubNumber: 1},
		media.Channel{ID: 50, Name: "Bluey", Major: 50},
		media.Channel{ID: 71, Name: "Seven", Major: 71},
	)

	ft := h.Targets.(fakeTargets)
	ft.target.Guide = livetv.New(backend, "en", livetv.Options{Now: func() time.Time { return now }})
	h.Targets = ft

	return h, backend
}

func TestLiveTVIntents(t *testing.T) {
	for name, tc := range map[string]struct {
		intent string
		slots  map[string]slotSpec
		call   string
		say    string
	}{
		"channel by name":         {intent: "WatchChannel", slots: map[string]slotSpec{"Channel": {value: "abc news"}}, call: "PlayChannel 24", say: "Switching to ABC NEWS."},
		"channel by number":       {intent: "WatchChannelNumber", slots: map[string]slotSpec{"ChannelNumber": {value: "71"}}, call: "PlayChannel 71", say: "Switching to Seven."},
		"channel and sub-channel": {intent: "WatchChannelNumber", slots: map[string]slotSpec{"ChannelNumber": {value: "32"}, "SubNumber": {value: "1"}}, call: "PlayChannel 32", say: "Switching to SBS World Movies."},
		"the guide":               {intent: "OpenTVGuide", slots: map[string]slotSpec{"LiveView": {"tv guide", "guide"}}, call: "ShowView tvguide"},
		"the guide by default":    {intent: "OpenTVGuide", call: "ShowView tvguide"},
		"the channel list":        {intent: "OpenTVGuide", slots: map[string]slotSpec{"LiveView": {value: "channel list"}}, call: "ShowView tvchannels"},
		"a programme on now":      {intent: "WatchLive", slots: map[string]slotSpec{"Programme": {value: "austin"}}, call: "PlayChannel 21", say: "Switching to ABC TV for Austin."},
		"put on a channel":        {intent: "PlayMedia", slots: map[string]slotSpec{"Query": {value: "abc news"}}, call: "PlayChannel 24", say: "Switching to ABC NEWS."},
	} {
		t.Run(name, func(t *testing.T) {
			h, backend := withLiveTV(t)

			res := string(serveBody(t, h, intentRequest(t, tc.intent, tc.slots)))

			assert.Contains(t, backend.Calls(), tc.call)

			if tc.say != "" {
				assert.Contains(t, res, tc.say)
			}
		})
	}
}

func TestWatchChannelNumberAlexaCouldNotRead(t *testing.T) {
	h, backend := withLiveTV(t)

	res := string(serveBody(t, h, intentRequest(t, "WatchChannelNumber", map[string]slotSpec{"ChannelNumber": {value: "?"}})))
	assert.Contains(t, res, "Which channel would you like? Say, for example, switch to ABC News.")
	assert.Contains(t, res, `"shouldEndSession":false`)

	for _, call := range backend.Calls() {
		assert.NotContains(t, call, "PlayChannel", "nothing tuned")
	}
}

func TestWhatsOnSaysTheTime(t *testing.T) {
	h, _ := withLiveTV(t)

	res := string(serveBody(t, h, intentRequest(t, "WhatsOn", map[string]slotSpec{"Channel": {value: "abc tv"}})))
	assert.Contains(t, res, "On ABC TV now: Austin, until 9:51 pm.", "in Melbourne time")
}

func TestPlayMediaPrefersTheLibrary(t *testing.T) {
	h, backend := withLiveTV(t)

	serveBody(t, h, intentRequest(t, "PlayMedia", map[string]slotSpec{"Query": {value: "bluey"}}))
	assert.NotContains(t, backend.Calls(), "PlayChannel 50", "Bluey the show, not the channel: %q", backend.Calls())
	assert.Contains(t, backend.Calls(), "Episodes 20", "the show's next episode is looked up")
}

func TestLiveTVOff(t *testing.T) {
	h, backend := withLiveTV(t, media.ContentLiveTV)

	res := string(serveBody(t, h, intentRequest(t, "WatchChannel", map[string]slotSpec{"Channel": {value: "abc news"}})))
	assert.Contains(t, res, "set up without that kind of title")

	serveBody(t, h, intentRequest(t, "PlayMedia", map[string]slotSpec{"Query": {value: "abc news"}}))
	assert.NotContains(t, backend.Calls(), "PlayChannel 24", "play X doesn't tune with live TV off")
}

func TestPlayMediaOnlyLiveTV(t *testing.T) {
	h, backend := withLiveTV(t, media.ContentMovies, media.ContentTV, media.ContentMusic)

	serveBody(t, h, intentRequest(t, "PlayMedia", map[string]slotSpec{"Query": {value: "abc news"}}))
	assert.Contains(t, backend.Calls(), "PlayChannel 24", "a skill set up for live TV alone still puts on channels")
}

// With live TV off, as it is unless turned on, play X still searches every kind deep search reaches, playlists among them.
func TestPlayMediaDeepSearchWithLiveTVOff(t *testing.T) {
	h, backend := withDisabled(t, true, media.ContentLiveTV)

	serveBody(t, h, intentRequest(t, "PlayMedia", map[string]slotSpec{"Query": {value: "road trip"}}))
	assert.True(t, containsPrefix(backend.Calls(), "Play playlist/70"), "%q", backend.Calls())
}
