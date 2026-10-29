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

	for _, tc := range []struct {
		query string
		want  []RoomSplit
	}{
		{"リビングで千と千尋の神隠し", []RoomSplit{{"千と千尋の神隠し", "リビング"}}},
		{"千と千尋の神隠しをリビングで", []RoomSplit{{"千と千尋の神隠し", "リビング"}}},
		{"寝室でテレビで見るドラマ", []RoomSplit{{"見るドラマ", "寝室でテレビ"}, {"テレビで見るドラマ", "寝室"}}},
		{"千と千尋の神隠し", nil},
		{"リビングで", nil},
		{"でトトロ", nil},
	} {
		assert.Equal(t, tc.want, speech.RoomSplits("ja-JP", tc.query), tc.query)
	}
}

func TestJapaneseReinterpret(t *testing.T) {
	speech := japaneseSpeech(t)

	for _, tc := range []struct {
		query, intent string
		slots         map[string]string
	}{
		{"ブルーイの次のエピソード", "WatchNextEpisode", map[string]string{"Show": "ブルーイ"}},
		{"リビングでブルーイの次のエピソード", "WatchNextEpisode", map[string]string{"Show": "リビングでブルーイ"}},
		{"ブルーイの最新話", "WatchLatestEpisode", map[string]string{"Show": "ブルーイ"}},
		{"ブルーイのシーズン2のエピソード12", "WatchEpisode", map[string]string{"Show": "ブルーイ", "Season": "2", "Episode": "12"}},
		{"ブルーイ", "PlayMedia", map[string]string{"Query": "ブルーイ"}},
		{"千と千尋の神隠し", "PlayMedia", map[string]string{"Query": "千と千尋の神隠し"}},
	} {
		in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.query}}}
		got, _ := speech.Reinterpret("ja-JP", in)

		values := map[string]string{}
		for name, slot := range got.Slots {
			values[name] = slot.Value
		}

		assert.Equal(t, tc.intent, got.Name, tc.query)
		assert.Equal(t, tc.slots, values, tc.query)
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

	for d, want := range map[time.Duration]string{
		20 * time.Second: "1分未満",
		time.Minute:      "1分",
		12 * time.Minute: "12分",
		time.Hour:        "1時間",
		2 * time.Hour:    "2時間",
		72 * time.Minute: "1時間12分",
	} {
		assert.Equal(t, want, spokenDuration("ja", d), d)
	}

	assert.Equal(t, "AとB", spokenList("ja", []string{"A", "B"}))
	assert.Equal(t, "15時", spokenTime("ja-JP", "ja", time.Date(1, 1, 1, 15, 0, 0, 0, time.UTC)))
	assert.Equal(t, "9時30分", spokenTime("ja-JP", "ja", time.Date(1, 1, 1, 9, 30, 0, 0, time.UTC)))
}

func TestUnits(t *testing.T) {
	assert.Equal(t, []string{"play", "{Show}", "now"}, units("en", " play {Show}  now"))
	assert.Equal(t, []string{"{Show}", "の", "第", "12", "話"}, units("ja", "{Show}の第12話"))
	assert.Equal(t, []string{"a", " ", "b", "{", "x"}, units("ja", "a b{x"), "an unclosed brace is a character")
	assert.Equal(t, "ab c", joinUnits("ja", []string{" ", "a", "b", " ", "c", " "}))
	assert.Equal(t, "a b", joinUnits("en", []string{"a", "b"}))
}

func TestRoomAround(t *testing.T) {
	for _, tc := range []struct {
		pattern, before, after string
		place                  roomPlace
	}{
		{"play {Movie} [(in|on) the {Room}]", "(in|on) the ", "", roomLast},
		{"[{Room}で]映画{Movie}を再生して", "", "で", roomFirst},
		{"映画{Movie}を[{Room}で]再生して", "を", "で", roomMiddle},
		{"[{Room}[の]テレビで]{Movie}を再生して", "", "[の]テレビで", roomFirst},
		{"映画を[{Room}で]再生して", "", "", roomNone},
		{"{Room}で{Command}", "", "", roomNone},
		{"[{Room}で", "", "", roomNone},
		{"{Movie}を再生して", "", "", roomNone},
	} {
		before, after, place := roomAround(tc.pattern)
		assert.Equal(t, tc.place, place, tc.pattern)
		assert.Equal(t, tc.before, before, tc.pattern)
		assert.Equal(t, tc.after, after, tc.pattern)
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
	for _, query := range []string{"bedroomでhot fuzz", "hot fuzzをbedroomで"} {
		h, lounge, bedroom, rec := newRoomHandler(t)

		req := mustRead(t, "testdata/golden/play-media-in-bedroom/request.json")
		req = bytes.Replace(req, []byte(`"locale": "en-AU"`), []byte(`"locale": "ja-JP"`), 1)
		req = bytes.Replace(req, []byte(`"value": "hot fuzz in the bedroom"`), []byte(`"value": "`+query+`"`), 1)

		serveBody(t, h, req)
		assert.Empty(t, lounge.Calls(), query)
		assert.Equal(t, []string{"Play movie/4 resume"}, bedroom.Calls(), query)

		require.Len(t, rec.entries, 1)
		assert.Equal(t, "Bedroom", rec.entries[0].Target, query)
	}
}
