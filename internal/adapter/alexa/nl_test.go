package alexa

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/skillmodel"
)

func dutchSpeech(t *testing.T) *Speech {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return speech
}

func TestDutchSpokenDurationsListsAndTimes(t *testing.T) {
	assert.Equal(t, "Dune, Alien en Heat", spokenList("nl", []string{"Dune", "Alien", "Heat"}))
	assert.Equal(t, "Dune en Alien", spokenList("nl", []string{"Dune", "Alien"}))
	assert.Equal(t, "21:59", spokenTime("nl-NL", "nl", time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)))
	assert.Equal(t, "Frans", languageName("nl-NL", "fre"))

	testCases := []struct {
		name     string
		have     time.Duration
		expected string
	}{
		{name: "ShouldSayOneHourAnd12Minutes", have: 72 * time.Minute, expected: "1 uur 12 minuten"},
		{name: "ShouldSayTwoHours", have: 2 * time.Hour, expected: "2 uur"},
		{name: "ShouldSayOneMinute", have: time.Minute, expected: "1 minuut"},
		{name: "ShouldSay45Minutes", have: 45 * time.Minute, expected: "45 minuten"},
		{name: "ShouldSayTwoHoursAnd5Minutes", have: 125 * time.Minute, expected: "2 uur 5 minuten"},
		{name: "ShouldSayLessThanAMinuteFor20Seconds", have: 20 * time.Second, expected: "minder dan een minuut"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, spokenDuration("nl", tc.have))
		})
	}
}

func TestDutchRender(t *testing.T) {
	speech := dutchSpeech(t)

	testCases := []struct {
		name     string
		have     actions.Message
		expected string
	}{
		{name: "ShouldSayTimeRemainingAndEndTime", have: actions.Message{ID: "player.remaining_until", Durations: map[string]time.Duration{"remaining": 72 * time.Minute}, Times: map[string]time.Time{"end": time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)}}, expected: "Nog 1 uur 12 minuten te gaan. Het is om 21:59 afgelopen."},
		{name: "ShouldListNewMovies", have: actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"Dune", "Alien", "Heat"}}}, expected: "Je kunt Dune, Alien en Heat kijken."},
		{name: "ShouldNameMissingSubtitleLanguage", have: actions.Message{ID: "streams.no_subtitles_in", Languages: map[string]string{"language": "ger"}}, expected: "Er is hier geen ondertiteling in het Duits."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.Render("nl-NL", tc.have))
		})
	}
}

func TestDutchRoomSplits(t *testing.T) {
	speech := dutchSpeech(t)

	testCases := []struct {
		name     string
		have     string
		expected []RoomSplit
	}{
		{name: "ShouldSplitRoomAfterInDe", have: "flodder in de woonkamer", expected: []RoomSplit{{"flodder", "woonkamer"}}},
		{name: "ShouldSplitRoomAfterInHet", have: "flodder in het kantoor", expected: []RoomSplit{{"flodder", "kantoor"}}},
		// The bare "op" reads "de zolder" as the room too; no room is called that, so the caller moves on.
		{name: "ShouldSplitRoomAfterOpDe", have: "flodder op de zolder", expected: []RoomSplit{{"flodder", "zolder"}, {"flodder", "de zolder"}}},
		{name: "ShouldSplitRoomAfterBareOp", have: "flodder op zolder", expected: []RoomSplit{{"flodder", "zolder"}}},
		{name: "ShouldSplitOfficeAfterBareOp", have: "flodder op kantoor", expected: []RoomSplit{{"flodder", "kantoor"}}},
		{name: "ShouldNotSplitPlaceWithoutArticle", have: "flodder in amerika"},
		{name: "ShouldNotSplitRoomWithoutTitle", have: "in de woonkamer"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.RoomSplits("nl-NL", tc.have))
		})
	}
}

func TestDutchReinterpret(t *testing.T) {
	speech := dutchSpeech(t)

	type want struct {
		intent string
		slots  map[string]string
	}

	testCases := []struct {
		name     string
		have     string
		expected want
	}{
		{name: "ShouldReadNextEpisode", have: "de volgende aflevering van flikken", expected: want{"WatchNextEpisode", map[string]string{"Show": "flikken"}}},
		{name: "ShouldKeepRoomInShowOfLatestEpisode", have: "de nieuwste aflevering van flikken in de woonkamer", expected: want{"WatchLatestEpisode", map[string]string{"Show": "flikken in de woonkamer"}}},
		{name: "ShouldReadFirstEpisodeFrom1e", have: "de 1e aflevering van baantjer", expected: want{"WatchFirstEpisode", map[string]string{"Show": "baantjer"}}},
		{name: "ShouldReadFirstEpisodeFrom1ste", have: "de 1ste aflevering van baantjer", expected: want{"WatchFirstEpisode", map[string]string{"Show": "baantjer"}}},
		{name: "ShouldReadFirstEpisodeFromEerste", have: "de eerste aflevering van baantjer", expected: want{"WatchFirstEpisode", map[string]string{"Show": "baantjer"}}},
		{name: "ShouldReadRandomEpisode", have: "een willekeurige aflevering van penoza", expected: want{"WatchRandomEpisode", map[string]string{"Show": "penoza"}}},
		{name: "ShouldReadSeasonAndEpisode", have: "seizoen 2 aflevering 4 van penoza", expected: want{"WatchEpisode", map[string]string{"Show": "penoza", "Season": "2", "Episode": "4"}}},
		{name: "ShouldReadFromTheStartAsFirstEpisode", have: "flikken vanaf het begin", expected: want{"WatchFirstEpisode", map[string]string{"Show": "flikken"}}},
		{name: "ShouldKeepPlainTitleAsPlayMedia", have: "flodder", expected: want{"PlayMedia", map[string]string{"Query": "flodder"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}}
			actual := speech.Reinterpret("nl-NL", in)[0]

			values := map[string]string{}
			for name, slot := range actual.Slots {
				values[name] = slot.Value
			}

			assert.Equal(t, tc.expected, want{actual.Name, values})
		})
	}
}
