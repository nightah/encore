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

func ends(hour, minute int) actions.Message {
	return actions.Message{
		ID:        "player.remaining_until",
		Durations: map[string]time.Duration{"remaining": 5 * time.Minute},
		Times:     map[string]time.Time{"end": time.Date(1, 1, 1, hour, minute, 0, 0, time.UTC)},
	}
}

func TestRegionalRoomSplits(t *testing.T) {
	speech := regionalSpeech(t)

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected []RoomSplit
	}{
		{name: "ShouldSplitRecamaraInEsMX", locale: "es-MX", have: "coco en la recámara", expected: []RoomSplit{{"coco", "recámara"}}},
		{name: "ShouldSplitEnMiInEsMX", locale: "es-MX", have: "coco en mi cuarto", expected: []RoomSplit{{"coco", "cuarto"}}},
		{name: "ShouldSplitEnMiInEsUS", locale: "es-US", have: "coco en mi cuarto", expected: []RoomSplit{{"coco", "cuarto"}}},
		{name: "ShouldNotSplitEnMiInEsES", locale: "es-ES", have: "coco en mi cuarto"},
		{name: "ShouldSplitAuInFrCA", locale: "fr-CA", have: "les boys au sous-sol", expected: []RoomSplit{{"les boys", "sous-sol"}}},
		{name: "ShouldSplitDansLeInFrCA", locale: "fr-CA", have: "les boys dans le salon", expected: []RoomSplit{{"les boys", "salon"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.RoomSplits(tc.locale, tc.have))
		})
	}
}

func TestRegionalReinterpret(t *testing.T) {
	speech := regionalSpeech(t)

	testCases := []struct {
		name   string
		locale string
		have   string
		intent string
		slots  map[string]string
	}{
		{name: "ShouldReadCapituloNuevoAsLatestEpisodeInEsMX", locale: "es-MX", have: "el capítulo nuevo de club de cuervos", intent: "WatchLatestEpisode", slots: map[string]string{"Show": "club de cuervos"}},
		{name: "ShouldReadCapituloNuevoAsLatestEpisodeInEsUS", locale: "es-US", have: "el capítulo nuevo de club de cuervos", intent: "WatchLatestEpisode", slots: map[string]string{"Show": "club de cuervos"}},
		{name: "ShouldKeepCapituloNuevoAsPlayMediaInEsES", locale: "es-ES", have: "el capítulo nuevo de club de cuervos", intent: "PlayMedia", slots: map[string]string{"Query": "el capítulo nuevo de club de cuervos"}},
		{name: "ShouldReadNextEpisodeInFrCA", locale: "fr-CA", have: "le prochain épisode de district 31", intent: "WatchNextEpisode", slots: map[string]string{"Show": "district 31"}},
		{name: "ShouldReadEmissionAsNextEpisodeInFrCA", locale: "fr-CA", have: "l'émission district 31", intent: "WatchNextEpisode", slots: map[string]string{"Show": "district 31"}},
		{name: "ShouldReadTeleromanAsNextEpisodeInFrCA", locale: "fr-CA", have: "le téléroman les belles histoires des pays d'en haut", intent: "WatchNextEpisode", slots: map[string]string{"Show": "les belles histoires des pays d'en haut"}},
		{name: "ShouldKeepTeleromanAsPlayMediaInFrFR", locale: "fr-FR", have: "le téléroman district 31", intent: "PlayMedia", slots: map[string]string{"Query": "le téléroman district 31"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := speech.Reinterpret(tc.locale, Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}})[0]

			values := map[string]string{}
			for name, slot := range actual.Slots {
				values[name] = slot.Value
			}

			assert.Equal(t, tc.intent, actual.Name)
			assert.Equal(t, tc.slots, values)
		})
	}
}

func TestRegionalSlotIDs(t *testing.T) {
	speech := regionalSpeech(t)

	testCases := []struct {
		name     string
		locale   string
		typ      string
		have     string
		expected string
	}{
		{name: "ShouldResolveRolaAsSongInEsMX", locale: "es-MX", typ: "MEDIA_TYPE", have: "rola", expected: "song"},
		{name: "ShouldResolveRolasAsSongInEsUS", locale: "es-US", typ: "MEDIA_TYPE", have: "rolas", expected: "song"},
		{name: "ShouldResolveOkeyAsSelectInEsMX", locale: "es-MX", typ: "NAV_ACTION", have: "okey", expected: "select"},
		{name: "ShouldResolveUnTantitoAsSmallInEsMX", locale: "es-MX", typ: "SEEK_SIZE", have: "un tantito", expected: "small"},
		{name: "ShouldResolveCancionAsSongInEsMX", locale: "es-MX", typ: "MEDIA_TYPE", have: "canción", expected: "song"},
		{name: "ShouldNotResolveRolaInEsES", locale: "es-ES", typ: "MEDIA_TYPE", have: "rola"},
		{name: "ShouldResolveTouneAsSongInFrCA", locale: "fr-CA", typ: "MEDIA_TYPE", have: "toune", expected: "song"},
		{name: "ShouldResolveTeleromanAsTVShowInFrCA", locale: "fr-CA", typ: "MEDIA_TYPE", have: "téléroman", expected: "tvshow"},
		{name: "ShouldResolveNouveauxVideoclipsInFrCA", locale: "fr-CA", typ: "LIBRARY_SECTION", have: "nouveaux vidéoclips", expected: "recentmusicvideos"},
		{name: "ShouldResolveVideoclipsRecentsInFrCA", locale: "fr-CA", typ: "LIBRARY_SECTION", have: "vidéoclips récents", expected: "recentmusicvideos"},
		{name: "ShouldResolveVideoclipsInFrCA", locale: "fr-CA", typ: "LIBRARY_SECTION", have: "vidéoclips", expected: "musicvideos"},
		{name: "ShouldNotResolveNouveauxVideoclipsInFrFR", locale: "fr-FR", typ: "LIBRARY_SECTION", have: "nouveaux vidéoclips"},
		{name: "ShouldResolveFermeAsOffInFrCA", locale: "fr-CA", typ: "SUBTITLE_ACTION", have: "ferme", expected: "off"},
		{name: "ShouldResolveOuvreAsOnInFrCA", locale: "fr-CA", typ: "SUBTITLE_ACTION", have: "ouvre", expected: "on"},
		{name: "ShouldNotResolveTouneInFrFR", locale: "fr-FR", typ: "MEDIA_TYPE", have: "toune"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.SlotID(tc.locale, tc.typ, tc.have))
		})
	}
}

func TestRegionalResponses(t *testing.T) {
	speech := regionalSpeech(t)

	testCases := []struct {
		name     string
		locale   string
		have     actions.Message
		expected string
	}{
		{name: "ShouldSayValeInEsES", locale: "es-ES", have: actions.Message{ID: "goodbye"}, expected: "Vale."},
		{name: "ShouldSayEstaBienInEsMX", locale: "es-MX", have: actions.Message{ID: "goodbye"}, expected: "Está bien."},
		{name: "ShouldSayNotFoundInEsUS", locale: "es-US", have: actions.Message{ID: "match.not_found", Params: map[string]string{"query": "Coco"}}, expected: "No encontré Coco en tu biblioteca."},
		{name: "ShouldSayTimeRemainingInEsMX", locale: "es-MX", have: actions.Message{ID: "player.remaining", Durations: map[string]time.Duration{"remaining": 72 * time.Minute}}, expected: "Termina en 1 hora 12 minutos."},
		{name: "ShouldListNewShowsInEsMX", locale: "es-MX", have: actions.Message{ID: "whatsnew.shows", Lists: map[string][]string{"titles": {"Rebelde", "Narcos"}}}, expected: "Hay episodios nuevos de Rebelde y Narcos."},
		{name: "ShouldSayPlaylistInFrFR", locale: "fr-FR", have: actions.Message{ID: "playlist.playing", Params: map[string]string{"playlist": "Party"}}, expected: "Je lance la playlist Party."},
		{name: "ShouldSayListeDeLectureInFrCA", locale: "fr-CA", have: actions.Message{ID: "playlist.playing", Params: map[string]string{"playlist": "Party"}}, expected: "Je lance la liste de lecture Party."},
		{name: "ShouldSayVideoclipInFrCA", locale: "fr-CA", have: actions.Message{ID: "music.playing_video", Params: map[string]string{"title": "Dégénérations", "artist": "Mes Aïeux"}}, expected: "Je lance le vidéoclip Dégénérations de Mes Aïeux."},
		{name: "ShouldNameAudioLanguageInFrCA", locale: "fr-CA", have: actions.Message{ID: "streams.audio_language", Languages: map[string]string{"language": "fre"}}, expected: "Audio : français."},
		{name: "ShouldSayAfternoonEndTimeInEsMX", locale: "es-MX", have: ends(15, 4), expected: "Hora de término: 3:04 p. m.; termina en 5 minutos."},
		{name: "ShouldSayMorningEndTimeInEsUS", locale: "es-US", have: ends(9, 30), expected: "Hora de término: 9:30 a. m.; termina en 5 minutos."},
		{name: "ShouldSayMidnightEndTimeInEsMX", locale: "es-MX", have: ends(0, 5), expected: "Hora de término: 12:05 a. m.; termina en 5 minutos."},
		{name: "ShouldSay24HourEndTimeInEsES", locale: "es-ES", have: ends(15, 4), expected: "Termina en 5 minutos, a las 15:04."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.Render(tc.locale, tc.have))
		})
	}
}

func TestRegionalSpokenTime(t *testing.T) {
	afternoon := time.Date(1, 1, 1, 15, 4, 0, 0, time.UTC)

	testCases := []struct {
		name     string
		locale   string
		lang     string
		expected string
	}{
		{name: "ShouldSpeakTwelveHourTimeInEsMX", locale: "es-MX", lang: "es", expected: "3:04 p. m."},
		{name: "ShouldSpeakTwelveHourTimeInEsUS", locale: "es-US", lang: "es", expected: "3:04 p. m."},
		{name: "ShouldSpeak24HourTimeInEsES", locale: "es-ES", lang: "es", expected: "15:04"},
		{name: "ShouldSpeak24HourTimeInFrCA", locale: "fr-CA", lang: "fr", expected: "15:04"},
		{name: "ShouldSpeak24HourTimeInFrFR", locale: "fr-FR", lang: "fr", expected: "15:04"},
		{name: "ShouldSpeakTwelveHourTimeInEnAU", locale: "en-AU", lang: "en", expected: "3:04 pm"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, spokenTime(tc.locale, tc.lang, afternoon))
		})
	}
}
