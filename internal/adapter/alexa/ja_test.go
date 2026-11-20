package alexa

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/skillmodel"
)

func japaneseSpeech(t *testing.T) *Speech {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return speech
}

func TestJapaneseRoomSplits(t *testing.T) {
	speech := japaneseSpeech(t)

	testCases := []struct {
		name     string
		have     string
		expected []RoomSplit
	}{
		{name: "ShouldSplitRoomBeforeTitle", have: "リビングで千と千尋の神隠し", expected: []RoomSplit{{"千と千尋の神隠し", "リビング"}}},
		{name: "ShouldSplitRoomAfterTitle", have: "千と千尋の神隠しをリビングで", expected: []RoomSplit{{"千と千尋の神隠し", "リビング"}}},
		{name: "ShouldOfferEverySplitWhenTitleHasDe", have: "寝室でテレビで見るドラマ", expected: []RoomSplit{{"見るドラマ", "寝室でテレビ"}, {"テレビで見るドラマ", "寝室"}}},
		{name: "ShouldNotSplitTitleWithoutRoom", have: "千と千尋の神隠し"},
		{name: "ShouldNotSplitRoomWithoutTitle", have: "リビングで"},
		{name: "ShouldNotSplitParticleWithoutRoom", have: "でトトロ"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.RoomSplits("ja-JP", tc.have))
		})
	}
}

func TestJapaneseReinterpret(t *testing.T) {
	speech := japaneseSpeech(t)

	testCases := []struct {
		name   string
		have   string
		intent string
		slots  map[string]string
	}{
		{name: "ShouldReadNextEpisode", have: "ブルーイの次のエピソード", intent: "WatchNextEpisode", slots: map[string]string{"Show": "ブルーイ"}},
		{name: "ShouldKeepRoomInShow", have: "リビングでブルーイの次のエピソード", intent: "WatchNextEpisode", slots: map[string]string{"Show": "リビングでブルーイ"}},
		{name: "ShouldReadLatestEpisode", have: "ブルーイの最新話", intent: "WatchLatestEpisode", slots: map[string]string{"Show": "ブルーイ"}},
		{name: "ShouldReadSeasonAndEpisode", have: "ブルーイのシーズン2のエピソード12", intent: "WatchEpisode", slots: map[string]string{"Show": "ブルーイ", "Season": "2", "Episode": "12"}},
		{name: "ShouldKeepPlainTitleAsPlayMedia", have: "ブルーイ", intent: "PlayMedia", slots: map[string]string{"Query": "ブルーイ"}},
		{name: "ShouldKeepTitleWithNoAsPlayMedia", have: "千と千尋の神隠し", intent: "PlayMedia", slots: map[string]string{"Query": "千と千尋の神隠し"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}}
			actual := speech.Reinterpret("ja-JP", in)[0]

			values := map[string]string{}
			for name, slot := range actual.Slots {
				values[name] = slot.Value
			}

			assert.Equal(t, tc.intent, actual.Name)
			assert.Equal(t, tc.slots, values)
		})
	}
}

func TestJapaneseSpeech(t *testing.T) {
	speech := japaneseSpeech(t)

	msg := actions.Message{
		ID:        "player.remaining_until",
		Durations: map[string]time.Duration{"remaining": 72 * time.Minute},
		Times:     map[string]time.Time{"end": time.Date(1, 1, 1, 21, 5, 0, 0, time.UTC)},
	}
	assert.Equal(t, "残りは1時間12分です。21時5分に終わります。", speech.Render("ja-JP", msg))

	list := actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"デューン", "エイリアン", "ヒート"}}}
	assert.Equal(t, "見られる作品はデューン、エイリアンとヒートです。", speech.Render("ja-JP", list))

	assert.Equal(t, "AとB", spokenList("ja", []string{"A", "B"}))
	assert.Equal(t, "15時", spokenTime("ja-JP", "ja", time.Date(1, 1, 1, 15, 0, 0, 0, time.UTC)))
	assert.Equal(t, "9時30分", spokenTime("ja-JP", "ja", time.Date(1, 1, 1, 9, 30, 0, 0, time.UTC)))

	testCases := []struct {
		name     string
		have     time.Duration
		expected string
	}{
		{name: "ShouldSayLessThanAMinuteFor20Seconds", have: 20 * time.Second, expected: "1分未満"},
		{name: "ShouldSayOneMinute", have: time.Minute, expected: "1分"},
		{name: "ShouldSay12Minutes", have: 12 * time.Minute, expected: "12分"},
		{name: "ShouldSayOneHour", have: time.Hour, expected: "1時間"},
		{name: "ShouldSayTwoHours", have: 2 * time.Hour, expected: "2時間"},
		{name: "ShouldSayOneHourAnd12Minutes", have: 72 * time.Minute, expected: "1時間12分"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, spokenDuration("ja", tc.have))
		})
	}
}

func TestUnits(t *testing.T) {
	assert.Equal(t, []string{"play", "{Show}", "now"}, units("en", " play {Show}  now"))
	assert.Equal(t, []string{"{Show}", "の", "第", "12", "話"}, units("ja", "{Show}の第12話"))
	assert.Equal(t, []string{"a", " ", "b", "{", "x"}, units("ja", "a b{x"), "an unclosed brace is a character")
	assert.Equal(t, "ab c", joinUnits("ja", []string{" ", "a", "b", " ", "c", " "}))
	assert.Equal(t, "a b", joinUnits("en", []string{"a", "b"}))
}

func TestRoomAround(t *testing.T) {
	testCases := []struct {
		name   string
		have   string
		before string
		after  string
		place  roomPlace
	}{
		{name: "ShouldFindRoomLast", have: "play {Movie} [(in|on) the {Room}]", before: "(in|on) the ", place: roomLast},
		{name: "ShouldFindRoomFirst", have: "[{Room}で]映画{Movie}を再生して", after: "で", place: roomFirst},
		{name: "ShouldFindRoomInTheMiddle", have: "映画{Movie}を[{Room}で]再生して", before: "を", after: "で", place: roomMiddle},
		{name: "ShouldKeepOptionalWordsAfterRoom", have: "[{Room}[の]テレビで]{Movie}を再生して", after: "[の]テレビで", place: roomFirst},
		{name: "ShouldIgnoreRoomWithoutASlotBeside", have: "映画を[{Room}で]再生して", place: roomNone},
		{name: "ShouldIgnoreRoomOutsideOptionalPart", have: "{Room}で{Command}", place: roomNone},
		{name: "ShouldIgnoreUnclosedBracket", have: "[{Room}で", place: roomNone},
		{name: "ShouldIgnorePatternWithoutRoom", have: "{Movie}を再生して", place: roomNone},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			before, after, place := roomAround(tc.have)
			assert.Equal(t, tc.place, place)
			assert.Equal(t, tc.before, before)
			assert.Equal(t, tc.after, after)
		})
	}
}

func TestRoomPhrasesFromEveryPlace(t *testing.T) {
	phrases, err := roomPhrases("ja", map[string][]string{
		"WatchMovie": {"[{Room}で]映画{Movie}を再生して", "映画{Movie}を[{Room}(で|の)]再生して", "{Room}で{Command}"},
		"PlayMedia":  {"{Query}を再生して"},
	})
	require.NoError(t, err)
	assert.Equal(t, []roomPhrase{
		{before: nil, after: []string{"で"}, first: true},
		{before: []string{"を"}, after: []string{"で"}},
		{before: []string{"を"}, after: []string{"の"}},
	}, phrases)

	_, err = roomPhrases("ja", map[string][]string{"WatchMovie": {"[{Room}(で]映画{Movie}"}})
	require.Error(t, err, "a broken phrase after the room")

	_, err = roomPhrases("ja", map[string][]string{"WatchMovie": {"{Movie}(を[{Room}で]再生"}})
	require.Error(t, err, "a broken phrase before the room")

	phrases, err = roomPhrases("ja", map[string][]string{"WatchMovie": {"{Movie}[{Room}]"}})
	require.NoError(t, err)
	assert.Empty(t, phrases, "a room with nothing around it gives no phrase")
}

// TestJapaneseRoomInSearchQuery checks that a room named before the title, as Japanese puts it, or between the title and the verb, picks that room's Kodi instance and leaves the title.
func TestJapaneseRoomInSearchQuery(t *testing.T) {
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldPickRoomNamedBeforeTitle", have: "bedroomでhot fuzz"},
		{name: "ShouldPickRoomNamedBetweenTitleAndVerb", have: "hot fuzzをbedroomで"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, lounge, bedroom, rec := newRoomHandler(t)

			req := mustRead(t, "testdata/golden/play-media-in-bedroom/request.json")
			req = bytes.Replace(req, []byte(`"locale": "en-AU"`), []byte(`"locale": "ja-JP"`), 1)
			req = bytes.Replace(req, []byte(`"value": "hot fuzz in the bedroom"`), []byte(`"value": "`+tc.have+`"`), 1)

			serveBody(t, h, req)
			assert.Empty(t, lounge.Calls())
			assert.Equal(t, []string{"Play movie/4 resume"}, bedroom.Calls())

			require.Len(t, rec.entries, 1)
			assert.Equal(t, "Bedroom", rec.entries[0].Target)
		})
	}
}
