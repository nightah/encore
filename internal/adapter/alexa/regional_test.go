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

func regionalSpeech(t *testing.T) *Speech {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return speech
}

func TestRegionalRoomSplits(t *testing.T) {
	speech := regionalSpeech(t)

	for _, tc := range []struct {
		locale, query string
		want          []RoomSplit
	}{
		{"es-MX", "coco en la recámara", []RoomSplit{{"coco", "recámara"}}},
		{"es-MX", "coco en mi cuarto", []RoomSplit{{"coco", "cuarto"}}},
		{"es-US", "coco en mi cuarto", []RoomSplit{{"coco", "cuarto"}}},
		{"es-ES", "coco en mi cuarto", nil},
		{"fr-CA", "les boys au sous-sol", []RoomSplit{{"les boys", "sous-sol"}}},
		{"fr-CA", "les boys dans le salon", []RoomSplit{{"les boys", "salon"}}},
	} {
		assert.Equal(t, tc.want, speech.RoomSplits(tc.locale, tc.query), "%s %q", tc.locale, tc.query)
	}
}

func TestRegionalReinterpret(t *testing.T) {
	speech := regionalSpeech(t)

	for _, tc := range []struct {
		locale, query, intent string
		slots                 map[string]string
	}{
		{"es-MX", "el capítulo nuevo de club de cuervos", "WatchLatestEpisode", map[string]string{"Show": "club de cuervos"}},
		{"es-US", "el capítulo nuevo de club de cuervos", "WatchLatestEpisode", map[string]string{"Show": "club de cuervos"}},
		{"es-ES", "el capítulo nuevo de club de cuervos", "PlayMedia", map[string]string{"Query": "el capítulo nuevo de club de cuervos"}},
		{"fr-CA", "le prochain épisode de district 31", "WatchNextEpisode", map[string]string{"Show": "district 31"}},
		{"fr-CA", "l'émission district 31", "WatchNextEpisode", map[string]string{"Show": "district 31"}},
		{"fr-CA", "le téléroman les belles histoires des pays d'en haut", "WatchNextEpisode", map[string]string{"Show": "les belles histoires des pays d'en haut"}},
		{"fr-FR", "le téléroman district 31", "PlayMedia", map[string]string{"Query": "le téléroman district 31"}},
	} {
		got, _ := speech.Reinterpret(tc.locale, Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.query}}})

		values := map[string]string{}
		for name, slot := range got.Slots {
			values[name] = slot.Value
		}

		assert.Equal(t, tc.intent, got.Name, "%s %q", tc.locale, tc.query)
		assert.Equal(t, tc.slots, values, "%s %q", tc.locale, tc.query)
	}
}

func TestRegionalSlotIDs(t *testing.T) {
	speech := regionalSpeech(t)

	for _, tc := range []struct {
		locale, typ, heard, want string
	}{
		{"es-MX", "MEDIA_TYPE", "rola", "song"},
		{"es-US", "MEDIA_TYPE", "rolas", "song"},
		{"es-MX", "NAV_ACTION", "okey", "select"},
		{"es-MX", "SEEK_SIZE", "un tantito", "small"},
		{"es-MX", "MEDIA_TYPE", "canción", "song"},
		{"es-ES", "MEDIA_TYPE", "rola", ""},
		{"fr-CA", "MEDIA_TYPE", "toune", "song"},
		{"fr-CA", "MEDIA_TYPE", "téléroman", "tvshow"},
		{"fr-CA", "SUBTITLE_ACTION", "ferme", "off"},
		{"fr-CA", "SUBTITLE_ACTION", "ouvre", "on"},
		{"fr-FR", "MEDIA_TYPE", "toune", ""},
	} {
		assert.Equal(t, tc.want, speech.SlotID(tc.locale, tc.typ, tc.heard), "%s %s %q", tc.locale, tc.typ, tc.heard)
	}
}

func TestRegionalResponses(t *testing.T) {
	speech := regionalSpeech(t)

	for _, tc := range []struct {
		locale string
		msg    actions.Message
		want   string
	}{
		{"es-ES", actions.Message{ID: "goodbye"}, "Vale."},
		{"es-MX", actions.Message{ID: "goodbye"}, "Está bien."},
		{"es-US", actions.Message{ID: "match.not_found", Params: map[string]string{"query": "Coco"}}, "No encontré Coco en tu biblioteca."},
		{"es-MX", actions.Message{ID: "player.remaining", Durations: map[string]time.Duration{"remaining": 72 * time.Minute}}, "Termina en 1 hora 12 minutos."},
		{"es-MX", actions.Message{ID: "whatsnew.shows", Lists: map[string][]string{"titles": {"Rebelde", "Narcos"}}}, "Hay episodios nuevos de Rebelde y Narcos."},
		{"fr-FR", actions.Message{ID: "playlist.playing", Params: map[string]string{"playlist": "Party"}}, "Je lance la playlist Party."},
		{"fr-CA", actions.Message{ID: "playlist.playing", Params: map[string]string{"playlist": "Party"}}, "Je lance la liste de lecture Party."},
		{"fr-CA", actions.Message{ID: "music.playing_video", Params: map[string]string{"title": "Dégénérations", "artist": "Mes Aïeux"}}, "Je lance le vidéoclip Dégénérations de Mes Aïeux."},
		{"fr-CA", actions.Message{ID: "streams.audio_language", Languages: map[string]string{"language": "fre"}}, "Audio : français."},
	} {
		assert.Equal(t, tc.want, speech.Render(tc.locale, tc.msg), "%s %s", tc.locale, tc.msg.ID)
	}
}
