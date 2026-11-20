package alexa

import (
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/skillmodel"
)

func hindiSpeech(t *testing.T) *Speech {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return speech
}

func TestHindiSpeech(t *testing.T) {
	speech := hindiSpeech(t)

	testCases := []struct {
		name     string
		have     actions.Message
		expected string
	}{
		{name: "ShouldSayTimeRemainingAndEndTime", have: actions.Message{ID: "player.remaining_until", Durations: map[string]time.Duration{"remaining": 72 * time.Minute}, Times: map[string]time.Time{"end": time.Date(1, 1, 1, 21, 30, 0, 0, time.UTC)}}, expected: "अभी 1 घंटा 12 मिनट बाकी है। यह रात 9:30 बजे तक ख़त्म होगा।"},
		{name: "ShouldListNewMovies", have: actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"Dangal", "Sholay", "Lagaan"}}}, expected: "आपके पास देखने के लिए ये हैं: Dangal, Sholay और Lagaan।"},
		{name: "ShouldNameSubtitleLanguage", have: actions.Message{ID: "streams.subtitles_language", Languages: map[string]string{"language": "eng"}}, expected: "सबटाइटल: अंग्रेज़ी।"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.Render("hi-IN", tc.have))
		})
	}
}

func TestHindiDurations(t *testing.T) {
	testCases := []struct {
		name     string
		have     time.Duration
		expected string
	}{
		{name: "ShouldSayLessThanAMinuteFor20Seconds", have: 20 * time.Second, expected: "एक मिनट से कम"},
		{name: "ShouldSayOneMinute", have: time.Minute, expected: "1 मिनट"},
		{name: "ShouldSay45Minutes", have: 45 * time.Minute, expected: "45 मिनट"},
		{name: "ShouldSayOneHour", have: time.Hour, expected: "1 घंटा"},
		{name: "ShouldSayTwoHoursAnd30Minutes", have: 150 * time.Minute, expected: "2 घंटे 30 मिनट"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, spokenDuration("hi", tc.have))
		})
	}
}

func TestHindiTimes(t *testing.T) {
	testCases := []struct {
		name     string
		have     int
		expected string
	}{
		{name: "ShouldSayNightAtMidnight", have: 0, expected: "रात 12:05 बजे"},
		{name: "ShouldSayNightAt3", have: 3, expected: "रात 3:05 बजे"},
		{name: "ShouldSayMorningAt4", have: 4, expected: "सुबह 4:05 बजे"},
		{name: "ShouldSayMorningAt11", have: 11, expected: "सुबह 11:05 बजे"},
		{name: "ShouldSayAfternoonAtNoon", have: 12, expected: "दोपहर 12:05 बजे"},
		{name: "ShouldSayAfternoonAt15", have: 15, expected: "दोपहर 3:05 बजे"},
		{name: "ShouldSayEveningAt16", have: 16, expected: "शाम 4:05 बजे"},
		{name: "ShouldSayEveningAt19", have: 19, expected: "शाम 7:05 बजे"},
		{name: "ShouldSayNightAt20", have: 20, expected: "रात 8:05 बजे"},
		{name: "ShouldSayNightAt23", have: 23, expected: "रात 11:05 बजे"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, spokenTime("hi-IN", "hi", time.Date(1, 1, 1, tc.have, 5, 0, 0, time.UTC)))
		})
	}
}

func TestRoomPostposition(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
		ok       bool
	}{
		{name: "ShouldReadMeinAfterRoom", have: "[{Room} में] फ़िल्म {Movie} चलाओ", expected: "में", ok: true},
		{name: "ShouldReadAlternativesAfterRoom", have: "[{Room} (में|पर)] {Movie} चलाओ", expected: "(में|पर)", ok: true},
		{name: "ShouldReadOptionalWordsAfterRoom", have: "[{Room} [वाले] टीवी पर] {Movie} चलाओ", expected: "[वाले] टीवी पर", ok: true},
		{name: "ShouldRejectRoomWithNothingAfter", have: "[{Room} ] {Movie} चलाओ"},
		{name: "ShouldRejectUnclosedBracket", have: "[{Room} में {Movie} चलाओ"},
		{name: "ShouldRejectRoomOutsideOptionalPart", have: "{Room} में {Command}"},
		{name: "ShouldRejectRoomLast", have: "play {Movie} [(in|on) the {Room}]"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, after, place := roomAround(tc.have)
			actual := strings.TrimSpace(after)
			assert.Equal(t, tc.ok, place != roomNone && actual != "")
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestNewSpeechRejectsBrokenRoomPostposition(t *testing.T) {
	_, err := NewSpeech(speechSources(t, map[string]string{
		"en-US": "language: en\nutterances:\n  WatchMovie:\n    - \"[{Room} (में] {Movie} चलाओ\"\n",
	}), slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "locale en-US")
}

func TestHindiRoomSplits(t *testing.T) {
	speech := hindiSpeech(t)

	testCases := []struct {
		name     string
		have     string
		expected []RoomSplit
	}{
		{name: "ShouldSplitRoomBeforeTitle", have: "बेडरूम में दंगल", expected: []RoomSplit{{"दंगल", "बेडरूम"}}},
		{name: "ShouldSplitRoomAfterTitle", have: "दंगल बेडरूम में", expected: []RoomSplit{{"दंगल", "बेडरूम"}}},
		{name: "ShouldSplitMultiWordRoom", have: "लिविंग रूम में शोले", expected: []RoomSplit{{"शोले", "लिविंग रूम"}}},
		{name: "ShouldOfferEverySplitWhenTitleHasPar", have: "तारे ज़मीन पर लिविंग रूम में", expected: []RoomSplit{{"तारे", "ज़मीन पर लिविंग रूम"}, {"तारे ज़मीन", "पर लिविंग रूम"}, {"तारे ज़मीन पर", "लिविंग रूम"}, {"तारे ज़मीन पर लिविंग", "रूम"}}},
		{name: "ShouldNotSplitTitleWithoutRoom", have: "दंगल"},
		{name: "ShouldNotSplitRoomWithoutTitle", have: "बेडरूम में"},
		{name: "ShouldNotSplitPostpositionWithoutRoom", have: "में दंगल"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.RoomSplits("hi-IN", tc.have))
		})
	}
}

func TestHindiReinterpret(t *testing.T) {
	speech := hindiSpeech(t)

	testCases := []struct {
		name   string
		have   string
		intent string
		slots  map[string]string
	}{
		{name: "ShouldReadNextEpisode", have: "पंचायत का अगला एपिसोड", intent: "WatchNextEpisode", slots: map[string]string{"Show": "पंचायत"}},
		{name: "ShouldKeepRoomInShow", have: "बेडरूम में पंचायत का अगला एपिसोड", intent: "WatchNextEpisode", slots: map[string]string{"Show": "बेडरूम में पंचायत"}},
		{name: "ShouldReadFirstEpisode", have: "पंचायत का पहला एपिसोड", intent: "WatchFirstEpisode", slots: map[string]string{"Show": "पंचायत"}},
		{name: "ShouldReadRandomEpisode", have: "पंचायत का कोई रैंडम एपिसोड", intent: "WatchRandomEpisode", slots: map[string]string{"Show": "पंचायत"}},
		{name: "ShouldReadSeasonAndEpisode", have: "पंचायत सीज़न 2 एपिसोड 4", intent: "WatchEpisode", slots: map[string]string{"Show": "पंचायत", "Season": "2", "Episode": "4"}},
		{name: "ShouldKeepPlainTitleAsPlayMedia", have: "पंचायत", intent: "PlayMedia", slots: map[string]string{"Query": "पंचायत"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := speech.Reinterpret("hi-IN", Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}})[0]

			values := map[string]string{}
			for name, slot := range actual.Slots {
				values[name] = slot.Value
			}

			assert.Equal(t, tc.intent, actual.Name)
			assert.Equal(t, tc.slots, values)
		})
	}
}

func TestTrailingCarrierLeavesGermanFormsAlone(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	utterances, err := src.Utterances("de-DE")
	require.NoError(t, err)

	got, err := queryForms(src.Model, utterances)
	require.NoError(t, err)

	leading := maps.Clone(utterances)
	leading["PlayMedia"] = slices.DeleteFunc(slices.Clone(utterances["PlayMedia"]), func(p string) bool { return strings.Contains(p, "{Query} abspielen") })
	require.Len(t, leading["PlayMedia"], len(utterances["PlayMedia"])-1)

	want, err := queryForms(src.Model, leading)
	require.NoError(t, err)

	// No German TV sample ends in "abspielen", so "{Query} abspielen" adds no form.
	assert.Equal(t, want, got)

	speech := hindiSpeech(t)

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldReadGermanNextEpisode", have: "die nächste folge von dark", expected: "WatchNextEpisode"},
		{name: "ShouldKeepGermanTitleAsPlayMedia", have: "dark", expected: "PlayMedia"},
		{name: "ShouldKeepTrailingAbspielenAsPlayMedia", have: "dark abspielen", expected: "PlayMedia"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := speech.Reinterpret("de-DE", Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}})[0]
			assert.Equal(t, tc.expected, actual.Name)
		})
	}
}
