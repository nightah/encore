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

	msg := actions.Message{
		ID:        "player.remaining_until",
		Durations: map[string]time.Duration{"remaining": 72 * time.Minute},
		Times:     map[string]time.Time{"end": time.Date(1, 1, 1, 21, 30, 0, 0, time.UTC)},
	}
	assert.Equal(t, "अभी 1 घंटा 12 मिनट बाकी है। यह रात 9:30 बजे तक ख़त्म होगा।", speech.Render("hi-IN", msg))

	list := actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"Dangal", "Sholay", "Lagaan"}}}
	assert.Equal(t, "आपके पास देखने के लिए ये हैं: Dangal, Sholay और Lagaan।", speech.Render("hi-IN", list))

	sub := actions.Message{ID: "streams.subtitles_language", Languages: map[string]string{"language": "eng"}}
	assert.Equal(t, "सबटाइटल: अंग्रेज़ी।", speech.Render("hi-IN", sub))
}

func TestHindiDurations(t *testing.T) {
	for d, want := range map[time.Duration]string{
		20 * time.Second:  "एक मिनट से कम",
		time.Minute:       "1 मिनट",
		45 * time.Minute:  "45 मिनट",
		time.Hour:         "1 घंटा",
		150 * time.Minute: "2 घंटे 30 मिनट",
	} {
		assert.Equal(t, want, spokenDuration("hi", d), d)
	}
}

func TestHindiTimes(t *testing.T) {
	for hour, want := range map[int]string{
		0:  "रात 12:05 बजे",
		3:  "रात 3:05 बजे",
		4:  "सुबह 4:05 बजे",
		11: "सुबह 11:05 बजे",
		12: "दोपहर 12:05 बजे",
		15: "दोपहर 3:05 बजे",
		16: "शाम 4:05 बजे",
		19: "शाम 7:05 बजे",
		20: "रात 8:05 बजे",
		23: "रात 11:05 बजे",
	} {
		assert.Equal(t, want, spokenTime("hi", time.Date(1, 1, 1, hour, 5, 0, 0, time.UTC)), hour)
	}
}

func TestRoomPostposition(t *testing.T) {
	for _, tc := range []struct {
		pattern, want string
		ok            bool
	}{
		{"[{Room} में] फ़िल्म {Movie} चलाओ", "में", true},
		{"[{Room} (में|पर)] {Movie} चलाओ", "(में|पर)", true},
		{"[{Room} [वाले] टीवी पर] {Movie} चलाओ", "[वाले] टीवी पर", true},
		{"[{Room} ] {Movie} चलाओ", "", false},
		{"[{Room} में {Movie} चलाओ", "", false},
		{"{Room} में {Command}", "", false},
		{"play {Movie} [(in|on) the {Room}]", "", false},
	} {
		got, ok := roomPostposition(tc.pattern)
		assert.Equal(t, tc.ok, ok, tc.pattern)
		assert.Equal(t, tc.want, got, tc.pattern)
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

	for _, tc := range []struct {
		query string
		want  []RoomSplit
	}{
		{"बेडरूम में दंगल", []RoomSplit{{"दंगल", "बेडरूम"}}},
		{"दंगल बेडरूम में", []RoomSplit{{"दंगल", "बेडरूम"}}},
		{"लिविंग रूम में शोले", []RoomSplit{{"शोले", "लिविंग रूम"}}},
		{"तारे ज़मीन पर लिविंग रूम में", []RoomSplit{
			{"तारे", "ज़मीन पर लिविंग रूम"},
			{"तारे ज़मीन", "पर लिविंग रूम"},
			{"तारे ज़मीन पर", "लिविंग रूम"},
			{"तारे ज़मीन पर लिविंग", "रूम"},
		}},
		{"दंगल", nil},
		{"बेडरूम में", nil},
		{"में दंगल", nil},
	} {
		assert.Equal(t, tc.want, speech.RoomSplits("hi-IN", tc.query), tc.query)
	}
}

func TestHindiReinterpret(t *testing.T) {
	speech := hindiSpeech(t)

	for _, tc := range []struct {
		query, intent, title string
		slots                map[string]string
	}{
		{"पंचायत का अगला एपिसोड", "WatchNextEpisode", "Show", map[string]string{"Show": "पंचायत"}},
		{"बेडरूम में पंचायत का अगला एपिसोड", "WatchNextEpisode", "Show", map[string]string{"Show": "बेडरूम में पंचायत"}},
		{"पंचायत का पहला एपिसोड", "WatchFirstEpisode", "Show", map[string]string{"Show": "पंचायत"}},
		{"पंचायत का कोई रैंडम एपिसोड", "WatchRandomEpisode", "Show", map[string]string{"Show": "पंचायत"}},
		{"पंचायत सीज़न 2 एपिसोड 4", "WatchEpisode", "Show", map[string]string{"Show": "पंचायत", "Season": "2", "Episode": "4"}},
		{"पंचायत", "PlayMedia", "Query", map[string]string{"Query": "पंचायत"}},
	} {
		got, title := speech.Reinterpret("hi-IN", Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.query}}})

		values := map[string]string{}
		for name, slot := range got.Slots {
			values[name] = slot.Value
		}

		assert.Equal(t, tc.intent, got.Name, tc.query)
		assert.Equal(t, tc.title, title, tc.query)
		assert.Equal(t, tc.slots, values, tc.query)
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

	for query, intent := range map[string]string{"die nächste folge von dark": "WatchNextEpisode", "dark": "PlayMedia", "dark abspielen": "PlayMedia"} {
		got, _ := speech.Reinterpret("de-DE", Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: query}}})
		assert.Equal(t, intent, got.Name, query)
	}
}
