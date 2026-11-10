package alexa

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

// slotSpec is a heard slot value and, when Alexa's entity resolution matched it, the value's ID.
type slotSpec struct {
	value, id string
}

// intentRequest builds an IntentRequest from the linked Echo in en-AU.
func intentRequest(t *testing.T, name string, slots map[string]slotSpec) []byte {
	t.Helper()

	var env RequestEnvelope
	env.Version = "1.0"
	env.Context.System.Device.DeviceID = linkedDevice
	env.Request = Request{Type: TypeIntent, RequestID: "amzn1.echo-api.request.1", Locale: "en-AU", Intent: Intent{Name: name, Slots: map[string]Slot{}}}

	for slotName, s := range slots {
		slot := Slot{Name: slotName, Value: s.value}

		if s.id != "" {
			var res Resolutions
			require.NoError(t, json.Unmarshal([]byte(`{"resolutionsPerAuthority":[{"status":{"code":"ER_SUCCESS_MATCH"},"values":[{"value":{"name":"`+s.value+`","id":"`+s.id+`"}}]}]}`), &res))
			slot.Resolutions = &res
		}

		env.Request.Intent.Slots[slotName] = slot
	}

	b, err := json.Marshal(env)
	require.NoError(t, err)

	return b
}

func playMovie(b *testkit.Backend) {
	for _, it := range testkit.SampleLibrary() {
		if it.Kind == media.KindMovie {
			b.SetPlaying(&media.NowPlaying{Item: it, Position: 10 * time.Minute, Duration: it.Runtime})

			return
		}
	}
}

func TestDispatchControlIntents(t *testing.T) {
	tests := map[string]struct {
		intent string
		slots  map[string]slotSpec
		want   string
	}{
		"pause":                         {intent: "AMAZON.PauseIntent", want: "Transport pause"},
		"resume":                        {intent: "AMAZON.ResumeIntent", want: "Transport resume"},
		"stop":                          {intent: "AMAZON.StopIntent", want: "Transport stop"},
		"next":                          {intent: "AMAZON.NextIntent", want: "Transport next"},
		"previous":                      {intent: "AMAZON.PreviousIntent", want: "Transport previous"},
		"start over":                    {intent: "AMAZON.StartOverIntent", want: "Transport startover"},
		"room control by resolved ID":   {intent: "RoomControl", slots: map[string]slotSpec{"Command": {"halt", "stop"}}, want: "Transport stop"},
		"room control by synonym":       {intent: "RoomControl", slots: map[string]slotSpec{"Command": {value: "resume"}}, want: "Transport resume"},
		"go home":                       {intent: "AMAZON.NavigateHomeIntent", want: "Input home"},
		"room control next":             {intent: "RoomControl", slots: map[string]slotSpec{"Command": {"next", "next"}}, want: "Transport next"},
		"shuffle off":                   {intent: "AMAZON.ShuffleOffIntent", want: "SetShuffle false"},
		"loop off":                      {intent: "AMAZON.LoopOffIntent", want: "SetRepeat off"},
		"repeat one":                    {intent: "Repeat", slots: map[string]slotSpec{"RepeatMode": {"this", "one"}}, want: "SetRepeat one"},
		"repeat off":                    {intent: "Repeat", slots: map[string]slotSpec{"RepeatMode": {"off", "off"}}, want: "SetRepeat off"},
		"repeat unknown mode cycles":    {intent: "Repeat", slots: map[string]slotSpec{"RepeatMode": {"sideways", "sideways"}}, want: "SetRepeat cycle"},
		"repeat without a mode cycles":  {intent: "Repeat", want: "SetRepeat cycle"},
		"seek forward a small step":     {intent: "PlayerSeek", want: "Seek smallforward"},
		"seek forward a big step":       {intent: "PlayerSeek", slots: map[string]slotSpec{"Size": {"a lot", "big"}}, want: "Seek bigforward"},
		"seek back a small step":        {intent: "PlayerSeek", slots: map[string]slotSpec{"SeekDirection": {"back", "back"}}, want: "Seek smallbackward"},
		"rewind a small step":           {intent: "PlayerRewind", want: "Seek smallbackward"},
		"rewind by a duration":          {intent: "PlayerRewind", slots: map[string]slotSpec{"Duration": {value: "PT2M"}}, want: "Seek by -2m0s"},
		"seek to a position":            {intent: "PlayerSeekTo", slots: map[string]slotSpec{"Position": {value: "PT1H5M"}}, want: "Seek to 1h5m0s"},
		"seek to a minute":              {intent: "PlayerSeekTo", slots: map[string]slotSpec{"Minute": {value: "42"}}, want: "Seek to 42m0s"},
		"zoom out":                      {intent: "PlayerZoom", slots: map[string]slotSpec{"Zoom": {"out", "out"}}, want: "Picture zoomout"},
		"zoom without an action zooms":  {intent: "PlayerZoom", want: "Picture zoomin"},
		"zoom in and move":              {intent: "PlayerZoom", slots: map[string]slotSpec{"Zoom": {"in", "in"}, "Direction": {"left", "left"}}, want: "Picture moveleft"},
		"rotate clockwise":              {intent: "PlayerRotate", slots: map[string]slotSpec{"Rotation": {"right", "clockwise"}}, want: "Picture rotateclockwise"},
		"rotate without a direction":    {intent: "PlayerRotate", want: "Picture rotateclockwise"},
		"move the picture":              {intent: "PlayerMove", slots: map[string]slotSpec{"Direction": {"down", "down"}}, want: "Picture movedown"},
		"stereoscopic mode":             {intent: "StereoscopicMode", want: "Input stereoscopic"},
		"volume up":                     {intent: "VolumeUp", want: "Volume step +"},
		"volume down":                   {intent: "VolumeDown", want: "Volume step -"},
		"mute":                          {intent: "Mute", want: "Volume toggle mute"},
		"what's playing":                {intent: "CurrentPlayItemEnquiry", want: "NowPlaying"},
		"audio stream back":             {intent: "AudioStream", slots: map[string]slotSpec{"Step": {"previous", "previous"}}, want: "SetAudioStream previous"},
		"subtitles off":                 {intent: "Subtitles", slots: map[string]slotSpec{"SubtitleAction": {"off", "off"}}, want: "SetSubtitle off"},
		"subtitles off, despite a lang": {intent: "Subtitles", slots: map[string]slotSpec{"SubtitleAction": {"off", "off"}, "Language": {value: "german"}}, want: "SetSubtitle off"},
		"subtitles next":                {intent: "Subtitles", slots: map[string]slotSpec{"SubtitleAction": {"next", "next"}}, want: "SetSubtitle next"},
		"subtitles alone turn them on":  {intent: "Subtitles", want: "SetSubtitle on"},
		"view library default":          {intent: "ViewLibrary", want: "ShowView"},
		"update the music library":      {intent: "UpdateLibrary", slots: map[string]slotSpec{"Library": {"music", "music"}}, want: "ScanLibrary music"},
		"update every library":          {intent: "UpdateLibrary", want: "ScanLibrary"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h, backend, _ := newHandler(t)
			playMovie(backend)

			res := serveBody(t, h, intentRequest(t, tc.intent, tc.slots))

			calls := backend.Calls()
			assert.True(t, containsPrefix(calls, tc.want), "want a call starting %q, got %q; response %s", tc.want, calls, res)
		})
	}
}

func TestDispatchNextFollowsAnEpisodeWithTheNextOne(t *testing.T) {
	for _, tc := range []struct {
		name   string
		intent string
		slots  map[string]slotSpec
	}{
		{"next", "AMAZON.NextIntent", nil},
		{"next on the TV", "RoomControl", map[string]slotSpec{"Command": {"next", "next"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, backend, _ := newHandler(t)
			backend.SetPlaying(&media.NowPlaying{Item: media.Item{Kind: media.KindEpisode, ID: 102, ShowID: 10, ShowTitle: "Severance", Season: 2, Episode: 4}})

			res := serveBody(t, h, intentRequest(t, tc.intent, tc.slots))

			assert.Contains(t, backend.Calls(), "Play episode/103 resume", "response %s", res)
		})
	}
}

func containsPrefix(calls []string, prefix string) bool {
	for _, c := range calls {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

func TestDispatchSeekToMinuteIsExact(t *testing.T) {
	h, backend, _ := newHandler(t)
	playMovie(backend)

	serveBody(t, h, intentRequest(t, "PlayerSeekTo", map[string]slotSpec{"Position": {value: "nonsense"}, "Minute": {value: "7"}}))
	assert.Contains(t, backend.Calls(), "Seek to 7m0s", "an unreadable position falls back to the minute")
}

func TestDispatchUnrecognisedSlotsAskAgain(t *testing.T) {
	tests := map[string]struct {
		intent string
		slots  map[string]slotSpec
	}{
		"room control without a command":  {intent: "RoomControl", slots: map[string]slotSpec{"Command": {value: "dance"}}},
		"seek to without a position":      {intent: "PlayerSeekTo"},
		"seek to an unreadable minute":    {intent: "PlayerSeekTo", slots: map[string]slotSpec{"Minute": {value: "forty two"}}},
		"move without a direction":        {intent: "PlayerMove", slots: map[string]slotSpec{"Direction": {value: "diagonally"}}},
		"volume without a level":          {intent: "VolumeSet", slots: map[string]slotSpec{"Level": {value: "loud"}}},
		"navigate without an action":      {intent: "Navigate", slots: map[string]slotSpec{"NavAction": {value: "sideways"}}},
		"power without an action":         {intent: "Power", slots: map[string]slotSpec{"PowerAction": {value: "explode"}}},
		"episode without a season":        {intent: "WatchEpisode", slots: map[string]slotSpec{"Show": {value: "bluey"}, "Episode": {value: "3"}}},
		"episode with a non-number":       {intent: "WatchEpisode", slots: map[string]slotSpec{"Show": {value: "bluey"}, "Season": {value: "one"}, "Episode": {value: "3"}}},
		"season without a number":         {intent: "WatchSeason", slots: map[string]slotSpec{"Show": {value: "bluey"}}},
		"season with an unknown ordinal":  {intent: "WatchSeason", slots: map[string]slotSpec{"Show": {value: "bluey"}, "Ordinal": {value: "endless"}}},
		"an intent Encore does not know":  {intent: "OrderPizza"},
		"fallback intent":                 {intent: "AMAZON.FallbackIntent"},
		"seek to a negative zero minutes": {intent: "PlayerSeekTo", slots: map[string]slotSpec{"Position": {value: "PT0S"}}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h, backend, rec := newHandler(t)
			playMovie(backend)

			var resp ResponseEnvelope
			require.NoError(t, json.Unmarshal(serveBody(t, h, intentRequest(t, tc.intent, tc.slots)), &resp))

			assert.Empty(t, backend.Calls(), "nothing is sent to Kodi for a request Encore can't read")
			require.NotNil(t, resp.Response.OutputSpeech)
			require.NotNil(t, resp.Response.Reprompt, "the listener is asked again")
			assert.Contains(t, resp.Response.OutputSpeech.Text, exampleTitle)
			assert.False(t, *resp.Response.ShouldEndSession)
			require.Len(t, rec.entries, 1)
			assert.Equal(t, "ok", rec.entries[0].Outcome)
		})
	}
}

func TestDispatchCancelEndsSession(t *testing.T) {
	h, backend, _ := newHandler(t)
	playMovie(backend)

	var resp ResponseEnvelope
	require.NoError(t, json.Unmarshal(serveBody(t, h, intentRequest(t, "AMAZON.CancelIntent", nil)), &resp))

	assert.True(t, *resp.Response.ShouldEndSession)
	assert.Nil(t, resp.Response.Reprompt)
	assert.Empty(t, backend.Calls(), "cancelling leaves playback alone, unlike stop")
}

func TestDispatchNoWithoutQuestionCancels(t *testing.T) {
	h, backend, _ := newHandler(t)

	var resp ResponseEnvelope
	require.NoError(t, json.Unmarshal(serveBody(t, h, intentRequest(t, "AMAZON.NoIntent", nil)), &resp))

	assert.True(t, *resp.Response.ShouldEndSession)
	assert.Empty(t, backend.Calls())
}

func TestSeekRequest(t *testing.T) {
	sl := func(slots map[string]slotSpec) slots {
		h, _, _ := newHandler(t)

		var env RequestEnvelope
		require.NoError(t, json.Unmarshal(intentRequest(t, "PlayerSeek", slots), &env))

		return slotsFor(env.Request.Intent, h.Speech)
	}

	assert.Equal(t, media.SeekBigBackward, seekRequest(true, sl(map[string]slotSpec{"Size": {"a lot", "big"}})).Step)
	assert.Equal(t, media.SeekBigBackward, seekRequest(false, sl(map[string]slotSpec{"Size": {"a lot", "big"}, "SeekDirection": {"back", "back"}})).Step)
	assert.Equal(t, 90*time.Second, seekRequest(false, sl(map[string]slotSpec{"Duration": {value: "PT1M30S"}})).By)
	assert.Equal(t, -90*time.Second, seekRequest(false, sl(map[string]slotSpec{"Duration": {value: "PT1M30S"}, "SeekDirection": {"back", "back"}})).By)
	assert.Equal(t, media.SeekSmallForward, seekRequest(false, sl(map[string]slotSpec{"Duration": {value: "PT0S"}})).Step, "a zero duration is a plain step")
}

func slotsFor(intent Intent, speech *Speech) slots {
	return slots{intent: intent, locale: "en-AU", speech: speech}
}

func TestBlankRoomMeansThisEcho(t *testing.T) {
	h, backend, bedroom, rec := newRoomHandler(t)

	serveBody(t, h, intentRequest(t, "WatchMovie", map[string]slotSpec{"Movie": {value: "hot fuzz"}, "Room": {value: " \t "}}))

	assert.Equal(t, []string{"Play movie/4 resume"}, backend.Calls(), "a room slot of only whitespace is no room at all")
	assert.Empty(t, bedroom.Calls())
	require.Len(t, rec.entries, 1)
	assert.Equal(t, "Lounge", rec.entries[0].Target)
}

func TestWhatsNewRespectsContentGroups(t *testing.T) {
	const declined = "This skill is set up without that kind of title."

	tests := map[string]struct {
		disabled []media.Content
		slots    map[string]slotSpec
		want     string
	}{
		"films by default":                      {want: "Library movie"},
		"episodes without films":                {disabled: []media.Content{media.ContentMovies}, want: "RecentlyAdded episode"},
		"albums with only music":                {disabled: []media.Content{media.ContentMovies, media.ContentTV}, want: "Library album"},
		"nothing with every group switched off": {disabled: media.Contents},
		"an asked-for type that is switched off": {
			disabled: []media.Content{media.ContentTV},
			slots:    map[string]slotSpec{"MediaType": {"shows", "tvshow"}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h, backend := withDisabled(t, false, tc.disabled...)

			res := serveBody(t, h, intentRequest(t, "WhatsNew", tc.slots))

			if tc.want == "" {
				assert.Contains(t, string(res), declined)
				assert.Empty(t, backend.Calls())

				return
			}

			assert.NotContains(t, string(res), declined)
			assert.True(t, containsPrefix(backend.Calls(), tc.want), "want %q, got %q", tc.want, backend.Calls())
		})
	}
}

// "Go back" reaches PlayerRewind, or PlayerSeek back, with no duration or size. While something plays it rewinds a little; with nothing playing there is nothing to rewind, so it presses the remote's Back, as "go back" means on a menu.
func TestRewindWithoutAnAmountGoesBackWhenNothingPlays(t *testing.T) {
	h, backend, _ := newHandler(t)

	serveBody(t, h, intentRequest(t, "PlayerRewind", nil))
	assert.Contains(t, backend.Calls(), "Input back")
	assert.False(t, containsPrefix(backend.Calls(), "Seek"), "nothing to seek in: %q", backend.Calls())

	h, backend, _ = newHandler(t)
	serveBody(t, h, intentRequest(t, "PlayerSeek", map[string]slotSpec{"SeekDirection": {"indietro", "back"}}))
	assert.Contains(t, backend.Calls(), "Input back", "a seek back without an amount is a rewind without one")

	h, backend, _ = newHandler(t)
	serveBody(t, h, intentRequest(t, "PlayerRewind", map[string]slotSpec{"Duration": {value: "PT2M"}}))
	assert.True(t, containsPrefix(backend.Calls(), "Seek by -2m0s"), "a rewind by an amount still seeks, which says nothing is playing: %q", backend.Calls())
}

// TestNonsenseHeardAsATitlePlaysNothing checks what the NLU checks can't: Amazon sends nonsense ("purple elephants dancing on the moon") to whichever intent is closest, a shuffle, an artist or the next episode, with the words as the title, and Encore must then play nothing and say so.
func TestNonsenseHeardAsATitlePlaysNothing(t *testing.T) {
	const nonsense = "purple elephants dancing on the moon"

	for intent, slot := range map[string]string{
		"ShuffleAlbum":     "Album",
		"ShuffleShow":      "Show",
		"ListenToAlbum":    "Album",
		"ListenToArtist":   "Artist",
		"WatchNextEpisode": "Show",
		"WatchMovie":       "Movie",
		"WhatAlbums":       "Artist",
		"PlayMedia":        "Query",
	} {
		t.Run(intent, func(t *testing.T) {
			h, backend, _ := newHandler(t)

			res := string(serveBody(t, h, intentRequest(t, intent, map[string]slotSpec{slot: {value: nonsense}})))

			assert.False(t, containsPrefix(backend.Calls(), "Play"), "nothing plays: %q", backend.Calls())
			assert.Contains(t, res, `"outputSpeech"`, "and Encore says why: %s", res)
		})
	}
}

// TestSearchWithTheKindInTheQuery checks that a plain search Amazon heard with the kind of title in its query searches that kind for the title alone, as the search of that kind would.
func TestSearchWithTheKindInTheQuery(t *testing.T) {
	for query, want := range map[string]string{
		"the film hot fuzz": "ShowView movies title hot fuzz",
		"the show bluey":    "ShowView tvshows title bluey",
	} {
		h, backend, _ := newHandler(t)

		serveBody(t, h, intentRequest(t, "GlobalSearch", map[string]slotSpec{"Query": {value: query}}))
		assert.Equal(t, []string{want}, backend.Calls(), query)
	}
}

// TestNextEpisodeOfAFilmPlaysTheFilm covers Amazon sending a film to WatchNextEpisode: "play the odyssey from 2026" is closest to "play {Show} from where i left off". A title no show has is tried as a film, but a show still wins its own name.
func TestNextEpisodeOfAFilmPlaysTheFilm(t *testing.T) {
	h, backend, _ := newHandler(t)

	serveBody(t, h, intentRequest(t, "WatchNextEpisode", map[string]slotSpec{"Show": {value: "hot fuzz"}}))
	assert.Equal(t, []string{"Play movie/4 resume"}, backend.Calls(), "no show is called that, so the film plays")

	h, backend, _, _ = newRoomHandlerWith(t, media.Item{Kind: media.KindMovie, ID: 90, Title: "Severance", Year: 2024})

	serveBody(t, h, intentRequest(t, "WatchNextEpisode", map[string]slotSpec{"Show": {value: "severance"}}))
	assert.NotContains(t, backend.Calls(), "Play movie/90 resume", "the show comes first: %q", backend.Calls())
	assert.True(t, containsPrefix(backend.Calls(), "Play episode/"), "its next episode plays: %q", backend.Calls())

	h, backend = withDisabled(t, false, media.ContentMovies)

	serveBody(t, h, intentRequest(t, "WatchNextEpisode", map[string]slotSpec{"Show": {value: "hot fuzz"}}))
	assert.False(t, containsPrefix(backend.Calls(), "Play"), "films switched off stay off: %q", backend.Calls())
}
