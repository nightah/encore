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
	for d, want := range map[time.Duration]string{
		72 * time.Minute:  "1 uur 12 minuten",
		2 * time.Hour:     "2 uur",
		time.Minute:       "1 minuut",
		45 * time.Minute:  "45 minuten",
		125 * time.Minute: "2 uur 5 minuten",
		20 * time.Second:  "minder dan een minuut",
	} {
		assert.Equal(t, want, spokenDuration("nl", d), "%v", d)
	}

	assert.Equal(t, "Dune, Alien en Heat", spokenList("nl", []string{"Dune", "Alien", "Heat"}))
	assert.Equal(t, "Dune en Alien", spokenList("nl", []string{"Dune", "Alien"}))
	assert.Equal(t, "21:59", spokenTime("nl-NL", "nl", time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)))
	assert.Equal(t, "Frans", languageName("nl-NL", "fre"))
}

func TestDutchRender(t *testing.T) {
	speech := dutchSpeech(t)

	msg := actions.Message{
		ID:        "player.remaining_until",
		Durations: map[string]time.Duration{"remaining": 72 * time.Minute},
		Times:     map[string]time.Time{"end": time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)},
	}
	assert.Equal(t, "Nog 1 uur 12 minuten te gaan. Het is om 21:59 afgelopen.", speech.Render("nl-NL", msg))

	list := actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"Dune", "Alien", "Heat"}}}
	assert.Equal(t, "Je kunt Dune, Alien en Heat kijken.", speech.Render("nl-NL", list))

	sub := actions.Message{ID: "streams.no_subtitles_in", Languages: map[string]string{"language": "ger"}}
	assert.Equal(t, "Er is hier geen ondertiteling in het Duits.", speech.Render("nl-NL", sub))
}

func TestDutchRoomSplits(t *testing.T) {
	speech := dutchSpeech(t)

	for _, tc := range []struct {
		query string
		want  []RoomSplit
	}{
		{"flodder in de woonkamer", []RoomSplit{{"flodder", "woonkamer"}}},
		{"flodder in het kantoor", []RoomSplit{{"flodder", "kantoor"}}},
		// The bare "op" reads "de zolder" as the room too; no room is called that, so the caller moves on.
		{"flodder op de zolder", []RoomSplit{{"flodder", "zolder"}, {"flodder", "de zolder"}}},
		{"flodder op zolder", []RoomSplit{{"flodder", "zolder"}}},
		{"flodder op kantoor", []RoomSplit{{"flodder", "kantoor"}}},
		{"flodder in amerika", nil},
		{"in de woonkamer", nil},
	} {
		assert.Equal(t, tc.want, speech.RoomSplits("nl-NL", tc.query), "%q", tc.query)
	}
}

func TestDutchReinterpret(t *testing.T) {
	speech := dutchSpeech(t)

	type want struct {
		intent, title string
		slots         map[string]string
	}

	for _, tc := range []struct {
		query string
		want  want
	}{
		{"de volgende aflevering van flikken", want{"WatchNextEpisode", "Show", map[string]string{"Show": "flikken"}}},
		{"de nieuwste aflevering van flikken in de woonkamer", want{"WatchLatestEpisode", "Show", map[string]string{"Show": "flikken in de woonkamer"}}},
		{"de 1e aflevering van baantjer", want{"WatchFirstEpisode", "Show", map[string]string{"Show": "baantjer"}}},
		{"de 1ste aflevering van baantjer", want{"WatchFirstEpisode", "Show", map[string]string{"Show": "baantjer"}}},
		{"de eerste aflevering van baantjer", want{"WatchFirstEpisode", "Show", map[string]string{"Show": "baantjer"}}},
		{"een willekeurige aflevering van penoza", want{"WatchRandomEpisode", "Show", map[string]string{"Show": "penoza"}}},
		{"seizoen 2 aflevering 4 van penoza", want{"WatchEpisode", "Show", map[string]string{"Show": "penoza", "Season": "2", "Episode": "4"}}},
		{"flikken vanaf het begin", want{"WatchFirstEpisode", "Show", map[string]string{"Show": "flikken"}}},
		{"flodder", want{"PlayMedia", "Query", map[string]string{"Query": "flodder"}}},
	} {
		in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.query}}}
		got, title := speech.Reinterpret("nl-NL", in)

		values := map[string]string{}
		for name, slot := range got.Slots {
			values[name] = slot.Value
		}

		assert.Equal(t, tc.want, want{got.Name, title, values}, "%q", tc.query)
	}
}
