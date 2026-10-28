package alexa

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/actions"
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
	assert.Equal(t, "up", h.Speech.SlotID("pt-BR", "NAV_DIRECTION", "up"), "unknown locales fall back")
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

	assert.Equal(t, "15:04", spokenTime("de", time.Date(1, 1, 1, 15, 4, 0, 0, time.UTC)))
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
