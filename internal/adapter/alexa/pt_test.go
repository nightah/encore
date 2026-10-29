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

func portugueseSpeech(t *testing.T) *Speech {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return speech
}

func TestPortugueseSpeech(t *testing.T) {
	speech := portugueseSpeech(t)

	assert.Equal(t, "1 hora 12 minutos", spokenDuration("pt", 72*time.Minute))
	assert.Equal(t, "2 horas 1 minuto", spokenDuration("pt", 121*time.Minute))
	assert.Equal(t, "menos de um minuto", spokenDuration("pt", 20*time.Second))
	assert.Equal(t, "Duna, Alien e Fogo contra Fogo", spokenList("pt", []string{"Duna", "Alien", "Fogo contra Fogo"}))
	assert.Equal(t, "21:59", spokenTime("pt", time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)))
	assert.Equal(t, "francês", languageName("pt-BR", "fre"))

	msg := actions.Message{
		ID:        "player.remaining_until",
		Durations: map[string]time.Duration{"remaining": 72 * time.Minute},
		Times:     map[string]time.Time{"end": time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)},
	}
	assert.Equal(t, "Tempo restante: 1 hora 12 minutos. Previsão de término: 21:59.", speech.Render("pt-BR", msg))

	list := actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"Duna", "Alien"}}}
	assert.Equal(t, "Para assistir, você tem Duna e Alien.", speech.Render("pt-BR", list))

	sub := actions.Message{ID: "streams.subtitles_language", Languages: map[string]string{"language": "eng"}}
	assert.Equal(t, "Legendas em inglês.", speech.Render("pt-BR", sub))
}

func TestPortugueseSlotIDs(t *testing.T) {
	speech := portugueseSpeech(t)

	assert.Equal(t, "up", speech.SlotID("pt-BR", "NAV_DIRECTION", "pra cima"))
	assert.Equal(t, "download", speech.SlotID("pt-BR", "SUBTITLE_ACTION", "baixar"))
	assert.Equal(t, "counterclockwise", speech.SlotID("pt-BR", "ROTATE_DIRECTION", "sentido anti-horário"))
	assert.Equal(t, "off", speech.SlotID("pt-BR", "REPEAT_MODE", "desligada"))
}

func TestPortugueseRoomSplits(t *testing.T) {
	speech := portugueseSpeech(t)

	for _, tc := range []struct {
		query string
		want  []RoomSplit
	}{
		{"tropa de elite na sala", []RoomSplit{{"tropa de elite", "sala"}}},
		{"cidade de deus no quarto do casal", []RoomSplit{{"cidade de deus", "quarto do casal"}}},
		{"o menino no espelho no quarto", []RoomSplit{
			{"o menino", "espelho no quarto"},
			{"o menino no espelho", "quarto"},
		}},
		{"bacurau", nil},
		{"na sala", nil},
	} {
		assert.Equal(t, tc.want, speech.RoomSplits("pt-BR", tc.query), "%q", tc.query)
	}
}

func TestPortugueseReinterpret(t *testing.T) {
	speech := portugueseSpeech(t)

	type want struct {
		intent, title string
		slots         map[string]string
	}

	for _, tc := range []struct {
		query string
		want  want
	}{
		{"o próximo episódio de sintonia", want{"WatchNextEpisode", "Show", map[string]string{"Show": "sintonia"}}},
		{"o próximo episódio de sintonia na sala", want{"WatchNextEpisode", "Show", map[string]string{"Show": "sintonia na sala"}}},
		{"o primeiro episódio de irmandade", want{"WatchFirstEpisode", "Show", map[string]string{"Show": "irmandade"}}},
		{"o 1º episódio de irmandade", want{"WatchFirstEpisode", "Show", map[string]string{"Show": "irmandade"}}},
		{"o 1° episódio de irmandade", want{"WatchFirstEpisode", "Show", map[string]string{"Show": "irmandade"}}},
		{"o último episódio de os normais", want{"WatchLatestEpisode", "Show", map[string]string{"Show": "os normais"}}},
		{"um episódio aleatório de chiquititas", want{"WatchRandomEpisode", "Show", map[string]string{"Show": "chiquititas"}}},
		{"o episódio 4 da temporada 2 de sintonia", want{"WatchEpisode", "Show", map[string]string{"Show": "sintonia", "Season": "2", "Episode": "4"}}},
		{"cidade invisível de onde parei", want{"WatchNextEpisode", "Show", map[string]string{"Show": "cidade invisível"}}},
		{"cidade de deus", want{"PlayMedia", "Query", map[string]string{"Query": "cidade de deus"}}},
	} {
		in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.query}}}
		got, title := speech.Reinterpret("pt-BR", in)

		values := map[string]string{}
		for name, slot := range got.Slots {
			values[name] = slot.Value
		}

		assert.Equal(t, tc.want, want{got.Name, title, values}, "%q", tc.query)
	}
}
