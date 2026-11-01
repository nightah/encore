package alexa

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"PT90S":     90 * time.Second,
		"PT10M":     10 * time.Minute,
		"PT1H30M":   90 * time.Minute,
		"pt2m":      2 * time.Minute,
		"PT1.5M":    90 * time.Second,
		"P1D":       24 * time.Hour,
		"P1DT2H":    26 * time.Hour,
		"P1W":       7 * 24 * time.Hour,
		"P1M":       30 * 24 * time.Hour,
		"P1MT1M":    30*24*time.Hour + time.Minute,
		"PT1H0M30S": time.Hour + 30*time.Second,
	} {
		got, ok := parseDuration(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"", "P", "PT", "10 minutes", "PT10X", "T10M", "PT0S", "?"} {
		_, ok := parseDuration(bad)
		assert.False(t, ok, bad)
	}
}

// A duration too long for time.Duration is unreadable, not a wrapped or platform-dependent value.
func TestParseDurationRejectsOverflow(t *testing.T) {
	for _, in := range []string{"P7000Y", "P300Y", "P250Y250Y250Y", "PT9999999999999H", "P1" + strings.Repeat("0", 400) + "D", "PT9223372036.854775808S"} {
		d, ok := parseDuration(in)
		assert.False(t, ok, in)
		assert.Zero(t, d, in)
	}

	d, ok := parseDuration("P290Y")
	assert.True(t, ok, "the longest durations that fit are still read")
	assert.Equal(t, 290*8766*time.Hour, d)
}

func TestSlotIDsResolveLocally(t *testing.T) {
	h, _, _ := newHandler(t)

	assert.Equal(t, "menu", h.Speech.SlotID("en-AU", "NAV_ACTION", "Context Menu"))
	assert.Equal(t, "pagedown", h.Speech.SlotID("en-AU", "NAV_ACTION", "show more"))
	assert.Equal(t, "recentepisodes", h.Speech.SlotID("en-GB", "LIBRARY_SECTION", "new episodes"))
	assert.Equal(t, "counterclockwise", h.Speech.SlotID("en-US", "ROTATE_DIRECTION", "anti clockwise"))
	assert.Equal(t, "bedroom", h.Speech.SlotID("en-AU", "KODI_ROOM", "bedroom"), "values without an ID are their own ID")
	assert.Equal(t, "up", h.Speech.SlotID("xx-XX", "NAV_DIRECTION", "up"), "unknown locales fall back")
	assert.Empty(t, h.Speech.SlotID("en-AU", "NAV_ACTION", "sideways"))
}

func TestSlotIDPrefersEntityResolution(t *testing.T) {
	h, _, _ := newHandler(t)

	var s Slot
	require.NoError(t, json.Unmarshal([]byte(`{"name":"NavAction","value":"go down","resolutions":{"resolutionsPerAuthority":[
		{"authority":"amzn1.er-authority.echo-sdk.skill.NAV_ACTION","status":{"code":"ER_SUCCESS_MATCH"},"values":[{"value":{"name":"down","id":"down"}}]}]}}`), &s))

	sl := slots{intent: Intent{Slots: map[string]Slot{"NavAction": s}}, locale: "en-AU", speech: h.Speech}
	assert.Equal(t, "down", sl.id("NavAction", "NAV_ACTION"))
}

func TestSpeechTimesLanguagesAndLists(t *testing.T) {
	h, _, _ := newHandler(t)

	msg := actions.Message{
		ID:        "player.remaining_until",
		Durations: map[string]time.Duration{"remaining": 42 * time.Minute},
		Times:     map[string]time.Time{"end": time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)},
	}
	assert.Equal(t, "There's 42 minutes left. It'll finish at 9:59 pm.", h.Speech.Render("en-GB", msg))

	sub := actions.Message{ID: "streams.subtitles_language", Languages: map[string]string{"language": "fre"}}
	assert.Equal(t, "Subtitles: French.", h.Speech.Render("en-US", sub), "ISO 639-2 codes are named")

	sub.Languages["language"] = "pt"
	assert.Equal(t, "Subtitles: Portuguese.", h.Speech.Render("en-AU", sub))

	list := actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"Dune", "Alien", "Heat"}}}
	assert.Equal(t, "You have Dune, Alien and Heat to watch.", h.Speech.Render("en-AU", list))

	assert.Equal(t, "15:04", spokenTime("de-DE", "de", time.Date(1, 1, 1, 15, 4, 0, 0, time.UTC)))
	assert.Equal(t, "Französisch", languageName("de-DE", "fre"))
	assert.Equal(t, "xx-nonsense", languageName("en-AU", "xx-nonsense"), "unknown codes are said as they are")
	assert.Equal(t, "A und B", spokenList("de", []string{"A", "B"}))
	assert.Equal(t, "A", spokenList("fr", []string{"A"}))
	assert.Empty(t, spokenList("en", nil))
}

func TestRoomTargeting(t *testing.T) {
	h, lounge, bedroom, rec := newRoomHandler(t)

	serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-in-bedroom/request.json"))
	assert.Empty(t, lounge.Calls())
	assert.Equal(t, []string{"Play movie/4 resume"}, bedroom.Calls())

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "Bedroom", rec.entries[0].Target)
	assert.Equal(t, "bedroom", rec.entries[0].Slots["Room"])
}

func TestRoomInSearchQuery(t *testing.T) {
	h, lounge, bedroom, rec := newRoomHandler(t)

	serveBody(t, h, mustRead(t, "testdata/golden/play-media-in-bedroom/request.json"))
	assert.Empty(t, lounge.Calls())
	assert.Equal(t, []string{"Play movie/4 resume"}, bedroom.Calls())

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "Bedroom", rec.entries[0].Target)
	assert.Equal(t, "hot fuzz in the bedroom", rec.entries[0].Slots["Query"], "the log keeps what was heard")
}

func TestRoomInSearchQueryUnknownRoom(t *testing.T) {
	h, _, bedroom, rec := newRoomHandler(t)

	req := mustRead(t, "testdata/golden/play-media-in-bedroom/request.json")
	req = bytes.Replace(req, []byte(`"value": "hot fuzz in the bedroom"`), []byte(`"value": "hot fuzz in the attic"`), 1)

	serveBody(t, h, req)
	assert.Empty(t, bedroom.Calls())

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "Lounge", rec.entries[0].Target, "a phrase naming no room stays part of the title")
	assert.Contains(t, rec.entries[0].Speech, "hot fuzz in the attic")
}

func TestRoomInSearchQueryLookupFails(t *testing.T) {
	h, lounge, bedroom, rec := newRoomHandler(t)

	req := mustRead(t, "testdata/golden/play-media-in-bedroom/request.json")
	req = bytes.Replace(req, []byte(`"value": "hot fuzz in the bedroom"`), []byte(`"value": "hot fuzz in the cellar"`), 1)

	serveBody(t, h, req)
	assert.Empty(t, lounge.Calls(), "a room that can't be looked up isn't guessed at")
	assert.Empty(t, bedroom.Calls())

	require.Len(t, rec.entries, 1)
	require.ErrorIs(t, rec.entries[0].Err, errRoomLookup)
	assert.Empty(t, rec.entries[0].Target)
}

func TestRoomGroup(t *testing.T) {
	for _, tc := range []struct {
		pattern, want string
		ok            bool
	}{
		{"play {Movie} [(in|on) the {Room}]", "(in|on) the", true},
		{"play {Movie} [(in|on) [the] {Room}]", "(in|on) [the]", true},
		{"play [the] {Movie} [(im|in der) {Room}]", "(im|in der)", true},
		{"play {Movie} [{Room}]", "", false},
		{"{Command} (in|on) the {Room}", "", false},
		{"play {Movie} [in the {Room}] please", "", false},
		{"play {Movie} in the {Room}]", "", false},
	} {
		got, ok := roomGroup(tc.pattern)
		assert.Equal(t, tc.ok, ok, tc.pattern)
		assert.Equal(t, tc.want, got, tc.pattern)
	}
}

func TestRoomSplits(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	for _, tc := range []struct {
		locale, query string
		want          []RoomSplit
	}{
		{"en-AU", "frozen in the lounge room", []RoomSplit{{"frozen", "lounge room"}}},
		{"en-AU", "frozen on the TV", []RoomSplit{{"frozen", "TV"}}},
		{"en-AU", "the man in the iron mask in the lounge", []RoomSplit{
			{"the man", "iron mask in the lounge"},
			{"the man in the iron mask", "lounge"},
		}},
		{"en-AU", "frozen", nil},
		{"en-AU", "in the lounge", nil},
		{"en-AU", "frozen in the", nil},
		{"de-DE", "die eiskönigin im wohnzimmer", []RoomSplit{{"die eiskönigin", "wohnzimmer"}}},
		{"de-DE", "die eiskönigin in der küche", []RoomSplit{{"die eiskönigin", "küche"}}},
		{"fr-FR", "la reine des neiges au salon", []RoomSplit{{"la reine des neiges", "salon"}}},
		{"es-ES", "frozen en el salón", []RoomSplit{{"frozen", "salón"}}},
		{"it-IT", "frozen nel salotto", []RoomSplit{{"frozen", "salotto"}}},
		{"xx-XX", "frozen in the lounge", []RoomSplit{{"frozen", "lounge"}}},
	} {
		assert.Equal(t, tc.want, speech.RoomSplits(tc.locale, tc.query), "%s %q", tc.locale, tc.query)
	}
}

func TestReinterpret(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	type want struct {
		intent string
		slots  map[string]string
	}

	for _, tc := range []struct {
		locale, query string
		want          want
	}{
		{"en-AU", "the next episode of bluey", want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
		{"en-AU", "next episode of bluey in the lounge", want{"WatchNextEpisode", map[string]string{"Show": "bluey in the lounge"}}},
		{"en-AU", "bluey from where I left off", want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
		{"en-AU", "The Latest Episode of Bluey", want{"WatchLatestEpisode", map[string]string{"Show": "Bluey"}}},
		{"en-AU", "the 1st episode of bluey", want{"WatchFirstEpisode", map[string]string{"Show": "bluey"}}},
		{"en-AU", "the first episode of bluey", want{"WatchFirstEpisode", map[string]string{"Show": "bluey"}}},
		{"en-AU", "a random episode of bluey", want{"WatchRandomEpisode", map[string]string{"Show": "bluey"}}},
		{"en-AU", "season 2 episode 4 of severance", want{"WatchEpisode", map[string]string{"Show": "severance", "Season": "2", "Episode": "4"}}},
		{"en-AU", "season two episode four of severance", want{"PlayMedia", map[string]string{"Query": "season two episode four of severance"}}},
		{"en-AU", "bluey", want{"PlayMedia", map[string]string{"Query": "bluey"}}},
		{"en-AU", "the next episode of", want{"PlayMedia", map[string]string{"Query": "the next episode of"}}},
		{"en-AU", "the man in the iron mask", want{"PlayMedia", map[string]string{"Query": "the man in the iron mask"}}},
		{"de-DE", "die nächste folge von bluey", want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
		{"xx-XX", "the next episode of bluey", want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
	} {
		in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.query}}}
		got := speech.Reinterpret(tc.locale, in)[0]

		values := map[string]string{}
		for name, slot := range got.Slots {
			values[name] = slot.Value
		}

		assert.Equal(t, tc.want, want{got.Name, values}, "%s %q", tc.locale, tc.query)
	}
}

func TestEpisodeInSearchQuery(t *testing.T) {
	t.Run("an unknown room stays part of the show", func(t *testing.T) {
		h, lounge, bedroom, rec := newRoomHandler(t)

		req := mustRead(t, "testdata/golden/play-media-latest-episode-in-bedroom/request.json")
		req = bytes.Replace(req, []byte(`in the bedroom"`), []byte(`in the attic"`), 1)

		serveBody(t, h, req)
		assert.Empty(t, bedroom.Calls())
		assert.Empty(t, lounge.Calls(), "no show is called \"severance in the attic\"")

		require.Len(t, rec.entries, 1)
		assert.Equal(t, "Lounge", rec.entries[0].Target)
		assert.Equal(t, "the latest episode of severance in the attic", rec.entries[0].Slots["Query"], "the log keeps what was heard")
	})

	t.Run("declined without TV", func(t *testing.T) {
		h, backend := withDisabled(t, false, media.ContentTV)
		res := serveBody(t, h, mustRead(t, "testdata/golden/play-media-next-episode/request.json"))
		assert.Contains(t, string(res), "This skill is set up without that kind of title.")
		assert.Empty(t, backend.Calls())
	})
}

func TestFollowupRoundTrip(t *testing.T) {
	h, backend, rec := newHandler(t)

	ask := serveBody(t, h, mustRead(t, "testdata/golden/shutdown-asks-first/request.json"))

	var resp ResponseEnvelope
	require.NoError(t, json.Unmarshal(ask, &resp))
	require.NotNil(t, resp.SessionAttributes[sessionFollowup])
	assert.Empty(t, backend.Calls(), "nothing happens until the user confirms")

	var yes RequestEnvelope
	require.NoError(t, json.Unmarshal(mustRead(t, "testdata/golden/shutdown-confirmed/request.json"), &yes))
	yes.Session.Attributes = resp.SessionAttributes

	body, err := json.Marshal(yes)
	require.NoError(t, err)

	serveBody(t, h, body)
	assert.Equal(t, []string{"System shutdown"}, backend.Calls())

	require.Len(t, rec.entries, 2)
	assert.Equal(t, "asked", rec.entries[0].Outcome)
}
