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
	assert.Equal(t, "21:59", spokenTime("pt-BR", "pt", time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)))
	assert.Equal(t, "francês", languageName("pt-BR", "fre"))

	testCases := []struct {
		name     string
		have     actions.Message
		expected string
	}{
		{name: "ShouldSayTimeRemainingAndEndTime", have: actions.Message{ID: "player.remaining_until", Durations: map[string]time.Duration{"remaining": 72 * time.Minute}, Times: map[string]time.Time{"end": time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)}}, expected: "Tempo restante: 1 hora 12 minutos. Previsão de término: 21:59."},
		{name: "ShouldListNewMovies", have: actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"Duna", "Alien"}}}, expected: "Para assistir, você tem Duna e Alien."},
		{name: "ShouldNameSubtitleLanguage", have: actions.Message{ID: "streams.subtitles_language", Languages: map[string]string{"language": "eng"}}, expected: "Legendas em inglês."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.Render("pt-BR", tc.have))
		})
	}
}

func TestPortugueseSlotIDs(t *testing.T) {
	speech := portugueseSpeech(t)

	testCases := []struct {
		name     string
		typ      string
		have     string
		expected string
	}{
		{name: "ShouldResolvePraCimaAsUp", typ: "NAV_DIRECTION", have: "pra cima", expected: "up"},
		{name: "ShouldResolveBaixarAsDownload", typ: "SUBTITLE_ACTION", have: "baixar", expected: "download"},
		{name: "ShouldResolveSentidoAntiHorarioAsCounterclockwise", typ: "ROTATE_DIRECTION", have: "sentido anti-horário", expected: "counterclockwise"},
		{name: "ShouldResolveDesligadaAsOff", typ: "REPEAT_MODE", have: "desligada", expected: "off"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.SlotID("pt-BR", tc.typ, tc.have))
		})
	}
}

func TestPortugueseRoomSplits(t *testing.T) {
	speech := portugueseSpeech(t)

	testCases := []struct {
		name     string
		have     string
		expected []RoomSplit
	}{
		{name: "ShouldSplitRoomAfterNa", have: "tropa de elite na sala", expected: []RoomSplit{{"tropa de elite", "sala"}}},
		{name: "ShouldSplitMultiWordRoomAfterNo", have: "cidade de deus no quarto do casal", expected: []RoomSplit{{"cidade de deus", "quarto do casal"}}},
		{name: "ShouldOfferEverySplitWhenTitleHasNo", have: "o menino no espelho no quarto", expected: []RoomSplit{{"o menino", "espelho no quarto"}, {"o menino no espelho", "quarto"}}},
		{name: "ShouldNotSplitTitleWithoutRoom", have: "bacurau"},
		{name: "ShouldNotSplitRoomWithoutTitle", have: "na sala"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.RoomSplits("pt-BR", tc.have))
		})
	}
}

func TestPortugueseReinterpret(t *testing.T) {
	speech := portugueseSpeech(t)

	type want struct {
		intent string
		slots  map[string]string
	}

	testCases := []struct {
		name     string
		have     string
		expected want
	}{
		{name: "ShouldReadNextEpisode", have: "o próximo episódio de sintonia", expected: want{"WatchNextEpisode", map[string]string{"Show": "sintonia"}}},
		{name: "ShouldKeepRoomInShow", have: "o próximo episódio de sintonia na sala", expected: want{"WatchNextEpisode", map[string]string{"Show": "sintonia na sala"}}},
		{name: "ShouldReadFirstEpisodeFromPrimeiro", have: "o primeiro episódio de irmandade", expected: want{"WatchFirstEpisode", map[string]string{"Show": "irmandade"}}},
		{name: "ShouldReadFirstEpisodeFromOrdinalIndicator", have: "o 1º episódio de irmandade", expected: want{"WatchFirstEpisode", map[string]string{"Show": "irmandade"}}},
		{name: "ShouldReadFirstEpisodeFromDegreeSign", have: "o 1° episódio de irmandade", expected: want{"WatchFirstEpisode", map[string]string{"Show": "irmandade"}}},
		{name: "ShouldReadLatestEpisode", have: "o último episódio de os normais", expected: want{"WatchLatestEpisode", map[string]string{"Show": "os normais"}}},
		{name: "ShouldReadRandomEpisode", have: "um episódio aleatório de chiquititas", expected: want{"WatchRandomEpisode", map[string]string{"Show": "chiquititas"}}},
		{name: "ShouldReadEpisodeAndSeason", have: "o episódio 4 da temporada 2 de sintonia", expected: want{"WatchEpisode", map[string]string{"Show": "sintonia", "Season": "2", "Episode": "4"}}},
		{name: "ShouldReadWhereILeftOffAsNextEpisode", have: "cidade invisível de onde parei", expected: want{"WatchNextEpisode", map[string]string{"Show": "cidade invisível"}}},
		{name: "ShouldKeepPlainTitleAsPlayMedia", have: "cidade de deus", expected: want{"PlayMedia", map[string]string{"Query": "cidade de deus"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}}
			actual := speech.Reinterpret("pt-BR", in)[0]

			values := map[string]string{}
			for name, slot := range actual.Slots {
				values[name] = slot.Value
			}

			assert.Equal(t, tc.expected, want{actual.Name, values})
		})
	}
}
