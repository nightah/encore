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
		"an intent Encore does not know":  {intent: "OrderPizza"},
		"fallback intent":                 {intent: "AMAZON.FallbackIntent"},
		"navigate home is not supported":  {intent: "AMAZON.NavigateHomeIntent"},
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
