package alexa

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// reading is the first way Reinterpret reads a search query: the intent and its slots' values.
func reading(speech *Speech, locale, query string) (string, map[string]string) {
	got := speech.Reinterpret(locale, Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: query}}})[0]

	values := map[string]string{}
	for name, slot := range got.Slots {
		values[name] = slot.Value
	}

	return got.Name, values
}

// TestReinterpretFirstWrittenAsDigits reads a first episode asked for with the ordinal written the way Alexa writes it in each language's search queries.
func TestReinterpretFirstWrittenAsDigits(t *testing.T) {
	speech := shippedSpeech(t)

	for _, tc := range []struct {
		locale, query, show string
	}{
		{"en-AU", "the 1st episode of bluey", "bluey"},
		{"nl-NL", "de 1e aflevering van flikken maastricht", "flikken maastricht"},
		{"nl-NL", "de 1ste aflevering van flikken maastricht", "flikken maastricht"},
		{"pt-BR", "o 1º episódio de irmandade", "irmandade"},
		{"pt-BR", "o 1° episódio de irmandade", "irmandade"},
		{"fr-FR", "le 1er épisode de kaamelott", "kaamelott"},
		{"fr-CA", "le 1er épisode de district 31", "district 31"},
		{"es-ES", "el 1er capítulo de la casa de papel", "la casa de papel"},
		{"es-MX", "el 1º episodio de club de cuervos", "club de cuervos"},
		{"es-US", "el 1.er episodio de club de cuervos", "club de cuervos"},
		{"it-IT", "il 1º episodio di gomorra", "gomorra"},
		{"it-IT", "il 1° episodio di gomorra", "gomorra"},
		{"de-DE", "die 1. folge von dark", "dark"},
		{"hi-IN", "पंचायत का 1ला एपिसोड", "पंचायत"},
		{"ja-JP", "ブルーイの第1話", "ブルーイ"},
		{"ja-JP", "ブルーイの1話目", "ブルーイ"},
	} {
		intent, slots := reading(speech, tc.locale, tc.query)
		assert.Equal(t, "WatchFirstEpisode", intent, "%s %q", tc.locale, tc.query)
		assert.Equal(t, map[string]string{"Show": tc.show}, slots, "%s %q", tc.locale, tc.query)
	}
}

// TestReinterpretKeepsNumberSlots keeps a digit that a sample's number slot reads from being taken for an ordinal word: the first episode of a season is episode 1, not the show's first episode.
func TestReinterpretKeepsNumberSlots(t *testing.T) {
	speech := shippedSpeech(t)

	intent, slots := reading(speech, "ja-JP", "ブルーイのシーズン2の第1話")
	assert.Equal(t, "WatchEpisode", intent)
	assert.Equal(t, map[string]string{"Show": "ブルーイ", "Season": "2", "Episode": "1"}, slots)
}
