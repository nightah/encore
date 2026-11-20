package alexa

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
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
	testCases := []struct {
		name     string
		intent   string
		slots    map[string]slotSpec
		expected string
	}{
		{name: "ShouldPause", intent: "AMAZON.PauseIntent", expected: "Transport pause"},
		{name: "ShouldResume", intent: "AMAZON.ResumeIntent", expected: "Transport resume"},
		{name: "ShouldStop", intent: "AMAZON.StopIntent", expected: "Transport stop"},
		{name: "ShouldSkipToNext", intent: "AMAZON.NextIntent", expected: "Transport next"},
		{name: "ShouldGoToPrevious", intent: "AMAZON.PreviousIntent", expected: "Transport previous"},
		{name: "ShouldStartOver", intent: "AMAZON.StartOverIntent", expected: "Transport startover"},
		{name: "ShouldRunRoomControlByResolvedID", intent: "RoomControl", slots: map[string]slotSpec{"Command": {"halt", "stop"}}, expected: "Transport stop"},
		{name: "ShouldRunRoomControlBySynonym", intent: "RoomControl", slots: map[string]slotSpec{"Command": {value: "resume"}}, expected: "Transport resume"},
		{name: "ShouldGoHome", intent: "AMAZON.NavigateHomeIntent", expected: "Input home"},
		{name: "ShouldRunRoomControlNext", intent: "RoomControl", slots: map[string]slotSpec{"Command": {"next", "next"}}, expected: "Transport next"},
		{name: "ShouldTurnShuffleOff", intent: "AMAZON.ShuffleOffIntent", expected: "SetShuffle false"},
		{name: "ShouldTurnLoopOff", intent: "AMAZON.LoopOffIntent", expected: "SetRepeat off"},
		{name: "ShouldRepeatOne", intent: "Repeat", slots: map[string]slotSpec{"RepeatMode": {"this", "one"}}, expected: "SetRepeat one"},
		{name: "ShouldTurnRepeatOff", intent: "Repeat", slots: map[string]slotSpec{"RepeatMode": {"off", "off"}}, expected: "SetRepeat off"},
		{name: "ShouldCycleRepeatForUnknownMode", intent: "Repeat", slots: map[string]slotSpec{"RepeatMode": {"sideways", "sideways"}}, expected: "SetRepeat cycle"},
		{name: "ShouldCycleRepeatWithoutAMode", intent: "Repeat", expected: "SetRepeat cycle"},
		{name: "ShouldSeekForwardASmallStep", intent: "PlayerSeek", expected: "Seek smallforward"},
		{name: "ShouldSeekForwardABigStep", intent: "PlayerSeek", slots: map[string]slotSpec{"Size": {"a lot", "big"}}, expected: "Seek bigforward"},
		{name: "ShouldSeekBackASmallStep", intent: "PlayerSeek", slots: map[string]slotSpec{"SeekDirection": {"back", "back"}}, expected: "Seek smallbackward"},
		{name: "ShouldRewindASmallStep", intent: "PlayerRewind", expected: "Seek smallbackward"},
		{name: "ShouldRewindByADuration", intent: "PlayerRewind", slots: map[string]slotSpec{"Duration": {value: "PT2M"}}, expected: "Seek by -2m0s"},
		{name: "ShouldSeekToAPosition", intent: "PlayerSeekTo", slots: map[string]slotSpec{"Position": {value: "PT1H5M"}}, expected: "Seek to 1h5m0s"},
		{name: "ShouldSeekToAMinute", intent: "PlayerSeekTo", slots: map[string]slotSpec{"Minute": {value: "42"}}, expected: "Seek to 42m0s"},
		{name: "ShouldZoomOut", intent: "PlayerZoom", slots: map[string]slotSpec{"Zoom": {"out", "out"}}, expected: "Picture zoomout"},
		{name: "ShouldZoomInWithoutAnAction", intent: "PlayerZoom", expected: "Picture zoomin"},
		{name: "ShouldZoomInAndMove", intent: "PlayerZoom", slots: map[string]slotSpec{"Zoom": {"in", "in"}, "Direction": {"left", "left"}}, expected: "Picture moveleft"},
		{name: "ShouldRotateClockwise", intent: "PlayerRotate", slots: map[string]slotSpec{"Rotation": {"right", "clockwise"}}, expected: "Picture rotateclockwise"},
		{name: "ShouldRotateClockwiseWithoutADirection", intent: "PlayerRotate", expected: "Picture rotateclockwise"},
		{name: "ShouldMoveThePicture", intent: "PlayerMove", slots: map[string]slotSpec{"Direction": {"down", "down"}}, expected: "Picture movedown"},
		{name: "ShouldToggleStereoscopicMode", intent: "StereoscopicMode", expected: "Input stereoscopic"},
		{name: "ShouldTurnVolumeUp", intent: "VolumeUp", expected: "Volume step +"},
		{name: "ShouldTurnVolumeDown", intent: "VolumeDown", expected: "Volume step -"},
		{name: "ShouldToggleMute", intent: "Mute", expected: "Volume toggle mute"},
		{name: "ShouldSayWhatIsPlaying", intent: "CurrentPlayItemEnquiry", expected: "NowPlaying"},
		{name: "ShouldSwitchToPreviousAudioStream", intent: "AudioStream", slots: map[string]slotSpec{"Step": {"previous", "previous"}}, expected: "SetAudioStream previous"},
		{name: "ShouldTurnSubtitlesOff", intent: "Subtitles", slots: map[string]slotSpec{"SubtitleAction": {"off", "off"}}, expected: "SetSubtitle off"},
		{name: "ShouldTurnSubtitlesOffDespiteALanguage", intent: "Subtitles", slots: map[string]slotSpec{"SubtitleAction": {"off", "off"}, "Language": {value: "german"}}, expected: "SetSubtitle off"},
		{name: "ShouldSwitchToNextSubtitle", intent: "Subtitles", slots: map[string]slotSpec{"SubtitleAction": {"next", "next"}}, expected: "SetSubtitle next"},
		{name: "ShouldTurnSubtitlesOnWhenAskedAlone", intent: "Subtitles", expected: "SetSubtitle on"},
		{name: "ShouldShowDefaultLibraryView", intent: "ViewLibrary", expected: "ShowView"},
		{name: "ShouldUpdateTheMusicLibrary", intent: "UpdateLibrary", slots: map[string]slotSpec{"Library": {"music", "music"}}, expected: "ScanLibrary music"},
		{name: "ShouldUpdateEveryLibrary", intent: "UpdateLibrary", expected: "ScanLibrary"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, backend, _ := newHandler(t)
			playMovie(backend)

			res := serveBody(t, h, intentRequest(t, tc.intent, tc.slots))

			calls := backend.Calls()
			assert.True(t, containsPrefix(calls, tc.expected), "want a call starting %q, got %q; response %s", tc.expected, calls, res)
		})
	}
}

func TestDispatchNextFollowsAnEpisodeWithTheNextOne(t *testing.T) {
	testCases := []struct {
		name   string
		intent string
		slots  map[string]slotSpec
	}{
		{name: "ShouldPlayNextEpisodeOnNext", intent: "AMAZON.NextIntent"},
		{name: "ShouldPlayNextEpisodeOnNextOnTheTV", intent: "RoomControl", slots: map[string]slotSpec{"Command": {"next", "next"}}},
	}

	for _, tc := range testCases {
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
	testCases := []struct {
		name   string
		intent string
		slots  map[string]slotSpec
	}{
		{name: "ShouldAskAgainForRoomControlWithoutACommand", intent: "RoomControl", slots: map[string]slotSpec{"Command": {value: "dance"}}},
		{name: "ShouldAskAgainForSeekToWithoutAPosition", intent: "PlayerSeekTo"},
		{name: "ShouldAskAgainForSeekToAnUnreadableMinute", intent: "PlayerSeekTo", slots: map[string]slotSpec{"Minute": {value: "forty two"}}},
		{name: "ShouldAskAgainForMoveWithoutADirection", intent: "PlayerMove", slots: map[string]slotSpec{"Direction": {value: "diagonally"}}},
		{name: "ShouldAskAgainForVolumeWithoutALevel", intent: "VolumeSet", slots: map[string]slotSpec{"Level": {value: "loud"}}},
		{name: "ShouldAskAgainForNavigateWithoutAnAction", intent: "Navigate", slots: map[string]slotSpec{"NavAction": {value: "sideways"}}},
		{name: "ShouldAskAgainForPowerWithoutAnAction", intent: "Power", slots: map[string]slotSpec{"PowerAction": {value: "explode"}}},
		{name: "ShouldAskAgainForEpisodeWithoutASeason", intent: "WatchEpisode", slots: map[string]slotSpec{"Show": {value: "bluey"}, "Episode": {value: "3"}}},
		{name: "ShouldAskAgainForEpisodeWithANonNumber", intent: "WatchEpisode", slots: map[string]slotSpec{"Show": {value: "bluey"}, "Season": {value: "one"}, "Episode": {value: "3"}}},
		{name: "ShouldAskAgainForSeasonWithoutANumber", intent: "WatchSeason", slots: map[string]slotSpec{"Show": {value: "bluey"}}},
		{name: "ShouldAskAgainForSeasonWithAnUnknownOrdinal", intent: "WatchSeason", slots: map[string]slotSpec{"Show": {value: "bluey"}, "Ordinal": {value: "endless"}}},
		{name: "ShouldAskAgainForAnIntentEncoreDoesNotKnow", intent: "OrderPizza"},
		{name: "ShouldAskAgainForFallbackIntent", intent: "AMAZON.FallbackIntent"},
		{name: "ShouldAskAgainForSeekToNegativeZeroMinutes", intent: "PlayerSeekTo", slots: map[string]slotSpec{"Position": {value: "PT0S"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

	testCases := []struct {
		name     string
		disabled []media.Content
		slots    map[string]slotSpec
		expected string
	}{
		{name: "ShouldListFilmsByDefault", expected: "Library movie"},
		{name: "ShouldListEpisodesWithoutFilms", disabled: []media.Content{media.ContentMovies}, expected: "RecentlyAdded episode"},
		{name: "ShouldListAlbumsWithOnlyMusic", disabled: []media.Content{media.ContentMovies, media.ContentTV}, expected: "Library album"},
		{name: "ShouldDeclineWithEveryGroupSwitchedOff", disabled: media.Contents},
		{name: "ShouldDeclineAnAskedForTypeThatIsSwitchedOff", disabled: []media.Content{media.ContentTV}, slots: map[string]slotSpec{"MediaType": {"shows", "tvshow"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, backend := withDisabled(t, false, tc.disabled...)

			res := serveBody(t, h, intentRequest(t, "WhatsNew", tc.slots))

			if tc.expected == "" {
				assert.Contains(t, string(res), declined)
				assert.Empty(t, backend.Calls())

				return
			}

			assert.NotContains(t, string(res), declined)
			assert.True(t, containsPrefix(backend.Calls(), tc.expected), "want %q, got %q", tc.expected, backend.Calls())
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

	testCases := []struct {
		name   string
		intent string
		slot   string
	}{
		{name: "ShouldPlayNothingForShuffleAlbum", intent: "ShuffleAlbum", slot: "Album"},
		{name: "ShouldPlayNothingForShuffleShow", intent: "ShuffleShow", slot: "Show"},
		{name: "ShouldPlayNothingForListenToAlbum", intent: "ListenToAlbum", slot: "Album"},
		{name: "ShouldPlayNothingForListenToArtist", intent: "ListenToArtist", slot: "Artist"},
		{name: "ShouldPlayNothingForWatchNextEpisode", intent: "WatchNextEpisode", slot: "Show"},
		{name: "ShouldPlayNothingForWatchMovie", intent: "WatchMovie", slot: "Movie"},
		{name: "ShouldPlayNothingForWhatAlbums", intent: "WhatAlbums", slot: "Artist"},
		{name: "ShouldPlayNothingForPlayMedia", intent: "PlayMedia", slot: "Query"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, backend, _ := newHandler(t)

			res := string(serveBody(t, h, intentRequest(t, tc.intent, map[string]slotSpec{tc.slot: {value: nonsense}})))

			assert.False(t, containsPrefix(backend.Calls(), "Play"), "nothing plays: %q", backend.Calls())
			assert.Contains(t, res, `"outputSpeech"`, "and Encore says why: %s", res)
		})
	}
}

// TestSearchWithTheKindInTheQuery checks that a plain search Amazon heard with the kind of title in its query searches that kind for the title alone, as the search of that kind would.
func TestSearchWithTheKindInTheQuery(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldSearchMoviesForTheFilm", have: "the film hot fuzz", expected: "ShowView movies title hot fuzz"},
		{name: "ShouldSearchTVShowsForTheShow", have: "the show bluey", expected: "ShowView tvshows title bluey"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, backend, _ := newHandler(t)

			serveBody(t, h, intentRequest(t, "GlobalSearch", map[string]slotSpec{"Query": {value: tc.have}}))
			assert.Equal(t, []string{tc.expected}, backend.Calls())
		})
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

// TestAnswerHeardAsAnotherIntent covers an answer to "did you mean" that Amazon sends as a new request: after "odyssey" asks about the album and the song Odyssey, "the odyssey from 2026" came as WatchNextEpisode, nearest "play {Show} from where i left off", and "i mean odyssey from 2005" as a seek to minute 2005. Whichever intent it arrives as, a title or year that names one of the question's choices answers it.
func TestAnswerHeardAsAnotherIntent(t *testing.T) {
	const film, album = "Play movie/82 resume", "Play album/80"

	testCases := []struct {
		name     string
		intent   string
		slots    map[string]slotSpec
		expected string
	}{
		{name: "ShouldAnswerWithNextEpisode", intent: "WatchNextEpisode", slots: map[string]slotSpec{"Show": {value: "the odyssey from 2026"}}, expected: film},
		{name: "ShouldAnswerWithMovie", intent: "WatchMovie", slots: map[string]slotSpec{"Movie": {value: "the odyssey from 2026"}}, expected: film},
		{name: "ShouldAnswerWithPlay", intent: "PlayMedia", slots: map[string]slotSpec{"Query": {value: "the odyssey from 2026"}}, expected: film},
		{name: "ShouldAnswerWithFrom", intent: chooseIntent, slots: map[string]slotSpec{"Answer": {value: "the odyssey from 2026"}}, expected: film},
		{name: "ShouldAnswerWithAnArticle", intent: chooseIntent, slots: map[string]slotSpec{"Answer": {value: "the odyssey"}}, expected: film},
		{name: "ShouldAnswerWithNextEpisodeOfOfferedChoice", intent: "WatchNextEpisode", slots: map[string]slotSpec{"Show": {value: "odyssey from 2005"}}, expected: album},
		{name: "ShouldAnswerWithAnswerAndYear", intent: chooseIntent, slots: map[string]slotSpec{"Answer": {value: "odyssey"}, "Year": {value: "2005"}}, expected: album},
		{name: "ShouldAnswerWithYearAlone", intent: chooseIntent, slots: map[string]slotSpec{"Year": {value: "2026"}}, expected: film},
		{name: "ShouldAnswerWithSeekToTheYear", intent: "PlayerSeekTo", slots: map[string]slotSpec{"Minute": {value: "2005"}}, expected: album},
		// Any title slot answers: "la de 1984" came as WatchNextInCollection and "die uit 1984" as WatchMovie, with the year as the title.
		{name: "ShouldAnswerWithCollectionsYear", intent: "WatchNextInCollection", slots: map[string]slotSpec{"Movie": {value: "2005"}}, expected: album},
		{name: "ShouldAnswerWithMoviesYear", intent: "WatchMovie", slots: map[string]slotSpec{"Movie": {value: "2026"}}, expected: film},
		{name: "ShouldAnswerWithAnswerThatIsAYear", intent: chooseIntent, slots: map[string]slotSpec{"Answer": {value: "2005"}}, expected: album},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, backend, resp := askOdyssey(t)

			serveAnswer(t, h, resp, tc.intent, tc.slots)
			assert.True(t, containsPrefix(backend.Calls(), tc.expected), "want %q, got %q", tc.expected, backend.Calls())
		})
	}
}

// TestNewRequestWhileAsking checks that a title intent naming none of the question's choices is a new request, not an answer: asked about Odyssey, "watch hot fuzz" plays Hot Fuzz.
func TestNewRequestWhileAsking(t *testing.T) {
	h, backend, resp := askOdyssey(t)

	serveAnswer(t, h, resp, "WatchMovie", map[string]slotSpec{"Movie": {value: "hot fuzz"}})
	assert.Equal(t, []string{"Play movie/4 resume"}, backend.Calls())
}

// TestSearchWhileAsking checks that a search heard while a question waits searches, as it would at any other time, rather than answering the question with a title it names.
func TestSearchWhileAsking(t *testing.T) {
	testCases := []struct {
		name   string
		intent string
	}{
		{name: "ShouldSearchWithGlobalSearch", intent: "GlobalSearch"},
		{name: "ShouldSearchWithSearchMovies", intent: "SearchMovies"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, backend, resp := askOdyssey(t)

			serveAnswer(t, h, resp, tc.intent, map[string]slotSpec{"Query": {value: "the odyssey from 2026"}})
			assert.False(t, containsPrefix(backend.Calls(), "Play"), "plays nothing: %q", backend.Calls())
			assert.True(t, containsPrefix(backend.Calls(), "ShowView"), "shows the search: %q", backend.Calls())
		})
	}
}

// TestSeekWhileAskingAsksAgain checks that a seek to a minute no choice was released in, heard while a question waits, asks the question again rather than moving the playhead: it was an answer Amazon heard as a seek.
func TestSeekWhileAskingAsksAgain(t *testing.T) {
	h, backend, resp := askOdyssey(t)

	res := serveAnswer(t, h, resp, "PlayerSeekTo", map[string]slotSpec{"Minute": {value: "10"}})
	assert.False(t, containsPrefix(backend.Calls(), "Seek"), "nothing seeks: %q", backend.Calls())
	assert.Contains(t, string(res), "Odyssey from 2005", "the question is asked again: %s", res)

	h, backend, _ = newHandler(t)
	serveBody(t, h, intentRequest(t, "PlayerSeekTo", map[string]slotSpec{"Minute": {value: "10"}}))
	assert.True(t, containsPrefix(backend.Calls(), "Seek"), "without a question, a seek seeks: %q", backend.Calls())
}

// askOdyssey asks "play odyssey" of a library holding the album and song Odyssey and the film The Odyssey, searched with the real matcher and deep search, and returns the response that asks which one.
func askOdyssey(t *testing.T) (*Handler, *testkit.Backend, ResponseEnvelope) {
	t.Helper()

	odyssey := []media.Item{
		{Kind: media.KindAlbum, ID: 80, Title: "Odyssey", Year: 2005},
		{Kind: media.KindSong, ID: 81, Title: "Odyssey", Year: 2024},
		{Kind: media.KindMovie, ID: 82, Title: "The Odyssey", Year: 2026},
	}

	h, backend, _, _ := newRoomHandlerWith(t, odyssey...)

	ix, err := match.New("en", match.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ix.Close() })
	require.NoError(t, ix.Upsert(testkit.Docs(append(testkit.SampleLibrary(), odyssey...)...)...))

	ft := h.Targets.(fakeTargets)
	ft.target.Matcher, ft.target.DeepSearch = ix, true
	h.Targets = ft

	var resp ResponseEnvelope
	require.NoError(t, json.Unmarshal(serveBody(t, h, intentRequest(t, "PlayMedia", map[string]slotSpec{"Query": {value: "odyssey"}})), &resp))
	require.NotNil(t, resp.SessionAttributes[sessionPending], "Alexa asks which one")
	require.Empty(t, backend.Calls())

	return h, backend, resp
}

// serveAnswer sends intent with slots in the session the question resp asked in.
func serveAnswer(t *testing.T, h *Handler, resp ResponseEnvelope, intent string, slots map[string]slotSpec) []byte {
	t.Helper()

	answer := requestOf(t, intentRequest(t, intent, slots))
	answer.Session.Attributes = resp.SessionAttributes

	body, err := json.Marshal(answer)
	require.NoError(t, err)

	return serveBody(t, h, body)
}
