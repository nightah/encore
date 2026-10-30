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

// TestReinterpretSeason reads a whole season asked for as a search query, by number or by ordinal, in every language.
func TestReinterpretSeason(t *testing.T) {
	speech := shippedSpeech(t)

	for _, tc := range []struct {
		locale, query string
		slots         map[string]string
	}{
		{"en-AU", "season 2 of bluey", map[string]string{"Show": "bluey", "Season": "2"}},
		{"en-GB", "series 2 of bluey", map[string]string{"Show": "bluey", "Season": "2"}},
		{"en-US", "bluey season 2", map[string]string{"Show": "bluey", "Season": "2"}},
		{"en-AU", "the second season of bluey", map[string]string{"Show": "bluey", "Ordinal": "second"}},
		{"en-AU", "the 2nd season of bluey", map[string]string{"Show": "bluey", "Ordinal": "2nd"}},
		{"en-AU", "all of season 3 of bluey", map[string]string{"Show": "bluey", "Season": "3"}},
		{"en-AU", "season 2", map[string]string{"Season": "2"}},
		{"de-DE", "staffel 2 von dark", map[string]string{"Show": "dark", "Season": "2"}},
		{"de-DE", "die zweite staffel von dark", map[string]string{"Show": "dark", "Ordinal": "zweite"}},
		{"de-DE", "die 2. staffel von dark", map[string]string{"Show": "dark", "Ordinal": "2."}},
		{"fr-FR", "la saison 2 de kaamelott", map[string]string{"Show": "kaamelott", "Season": "2"}},
		{"fr-FR", "la deuxième saison de kaamelott", map[string]string{"Show": "kaamelott", "Ordinal": "deuxième"}},
		{"fr-CA", "la 2e saison de district 31", map[string]string{"Show": "district 31", "Ordinal": "2e"}},
		{"es-ES", "la temporada 2 de la casa de papel", map[string]string{"Show": "la casa de papel", "Season": "2"}},
		{"es-ES", "la segunda temporada de la casa de papel", map[string]string{"Show": "la casa de papel", "Ordinal": "segunda"}},
		{"es-MX", "la 2da temporada de club de cuervos", map[string]string{"Show": "club de cuervos", "Ordinal": "2da"}},
		{"es-US", "la 2ª temporada de club de cuervos", map[string]string{"Show": "club de cuervos", "Ordinal": "2ª"}},
		{"it-IT", "la seconda stagione di gomorra", map[string]string{"Show": "gomorra", "Ordinal": "seconda"}},
		{"it-IT", "la stagione 2 di gomorra", map[string]string{"Show": "gomorra", "Season": "2"}},
		{"pt-BR", "a segunda temporada de sintonia", map[string]string{"Show": "sintonia", "Ordinal": "segunda"}},
		{"pt-BR", "a 2ª temporada de sintonia", map[string]string{"Show": "sintonia", "Ordinal": "2ª"}},
		{"nl-NL", "seizoen 2 van flikken maastricht", map[string]string{"Show": "flikken maastricht", "Season": "2"}},
		{"nl-NL", "het 2e seizoen van flikken maastricht", map[string]string{"Show": "flikken maastricht", "Ordinal": "2e"}},
		{"ar-SA", "الموسم الثاني من بلوي", map[string]string{"Show": "بلوي", "Ordinal": "الثاني"}},
		{"ar-SA", "الموسم 2 من بلوي", map[string]string{"Show": "بلوي", "Season": "2"}},
		{"hi-IN", "ब्लूई का दूसरा सीज़न", map[string]string{"Show": "ब्लूई", "Ordinal": "दूसरा"}},
		{"hi-IN", "ब्लूई का सीज़न 2", map[string]string{"Show": "ब्लूई", "Season": "2"}},
		{"ja-JP", "ブルーイのシーズン2", map[string]string{"Show": "ブルーイ", "Season": "2"}},
		{"ja-JP", "ブルーイのセカンドシーズン", map[string]string{"Show": "ブルーイ", "Ordinal": "セカンド"}},
		{"ja-JP", "ブルーイの第2期", map[string]string{"Show": "ブルーイ", "Season": "2"}},
	} {
		intent, slots := reading(speech, tc.locale, tc.query)
		assert.Equal(t, "WatchSeason", intent, "%s %q", tc.locale, tc.query)
		assert.Equal(t, tc.slots, slots, "%s %q", tc.locale, tc.query)
	}
}

// TestReinterpretSeasonNeedsAnOrdinal leaves a query whose ordinal slot would hold words that aren't one to be searched as a title.
func TestReinterpretSeasonNeedsAnOrdinal(t *testing.T) {
	speech := shippedSpeech(t)

	intent, slots := reading(speech, "en-AU", "the endless season of bluey")
	assert.Equal(t, "PlayMedia", intent)
	assert.Equal(t, map[string]string{"Query": "the endless season of bluey"}, slots)
}

func TestReinterpretEpisodeCode(t *testing.T) {
	speech := shippedSpeech(t)

	for _, tc := range []struct {
		locale, query, intent string
		slots                 map[string]string
	}{
		{"en-AU", "bluey s2e3", "WatchEpisode", map[string]string{"Show": "bluey", "Season": "2", "Episode": "3"}},
		{"en-AU", "Bluey S02E03", "WatchEpisode", map[string]string{"Show": "Bluey", "Season": "2", "Episode": "3"}},
		{"en-US", "bluey s. 2 e. 3", "WatchEpisode", map[string]string{"Show": "bluey", "Season": "2", "Episode": "3"}},
		{"en-GB", "bluey s2 ep3", "WatchEpisode", map[string]string{"Show": "bluey", "Season": "2", "Episode": "3"}},
		{"de-DE", "dark s1e4", "WatchEpisode", map[string]string{"Show": "dark", "Season": "1", "Episode": "4"}},
		{"en-AU", "bluey s2", "WatchSeason", map[string]string{"Show": "bluey", "Season": "2"}},
		{"en-AU", "ocean's 12", "PlayMedia", map[string]string{"Query": "ocean's 12"}},
		{"en-AU", "s2e3", "PlayMedia", map[string]string{"Query": "s2e3"}},
	} {
		intent, slots := reading(speech, tc.locale, tc.query)
		assert.Equal(t, tc.intent, intent, "%s %q", tc.locale, tc.query)
		assert.Equal(t, tc.slots, slots, "%s %q", tc.locale, tc.query)
	}
}

// TestSpeechOrdinal reads ordinals as words of the ORDINAL type and as Alexa writes them with digits in each language.
func TestSpeechOrdinal(t *testing.T) {
	speech := shippedSpeech(t)

	for _, tc := range []struct {
		locale, heard string
		want          int
	}{
		{"en-AU", "second", 2},
		{"en-AU", "2nd", 2},
		{"en-AU", "3rd", 3},
		{"en-AU", "21st", 21},
		{"en-AU", "4", 4},
		{"de-DE", "zweite", 2},
		{"de-DE", "zweiten", 2},
		{"de-DE", "3.", 3},
		{"fr-FR", "première", 1},
		{"fr-FR", "1re", 1},
		{"fr-FR", "1er", 1},
		{"fr-FR", "2e", 2},
		{"fr-CA", "2ème", 2},
		{"fr-FR", "seconde", 2},
		{"es-ES", "tercera", 3},
		{"es-ES", "3ª", 3},
		{"es-ES", "1.º", 1},
		{"es-MX", "3ra", 3},
		{"es-US", "2do", 2},
		{"it-IT", "terza", 3},
		{"it-IT", "3º", 3},
		{"it-IT", "3°", 3},
		{"pt-BR", "terceira", 3},
		{"pt-BR", "3ª", 3},
		{"nl-NL", "derde", 3},
		{"nl-NL", "3e", 3},
		{"nl-NL", "8ste", 8},
		{"ar-SA", "الثالث", 3},
		{"ar-SA", "الاول", 1},
		{"hi-IN", "तीसरा", 3},
		{"hi-IN", "पांचवां", 5},
		{"hi-IN", "3रा", 3},
		{"ja-JP", "サード", 3},
		{"ja-JP", "第三", 3},
		{"ja-JP", "第3", 3},
		{"xx-XX", "third", 3},
	} {
		got, ok := speech.Ordinal(tc.locale, tc.heard)
		assert.True(t, ok, "%s %q", tc.locale, tc.heard)
		assert.Equal(t, tc.want, got, "%s %q", tc.locale, tc.heard)
	}

	for _, tc := range []struct{ locale, heard string }{
		{"en-AU", ""},
		{"en-AU", "endless"},
		{"en-AU", "2e"},
		{"de-DE", "2nd"},
	} {
		_, ok := speech.Ordinal(tc.locale, tc.heard)
		assert.False(t, ok, "%s %q", tc.locale, tc.heard)
	}
}

// TestEveryLanguageReadsWrittenOrdinals keeps writtenOrdinals and firstOrdinals to the languages the skill speaks.
func TestEveryLanguageReadsWrittenOrdinals(t *testing.T) {
	speech := shippedSpeech(t)

	spoken := map[string]bool{}
	for _, lang := range speech.languages {
		spoken[lang] = true
	}

	for lang := range writtenOrdinals {
		assert.True(t, spoken[lang], "writtenOrdinals has %s, which the skill doesn't speak", lang)
	}

	for lang := range firstOrdinals {
		assert.True(t, spoken[lang], "firstOrdinals has %s, which the skill doesn't speak", lang)

		for written, word := range firstOrdinals[lang] {
			n, ok := writtenOrdinal(lang, written)
			assert.True(t, ok && n == 1, "%s %q is 1 to writtenOrdinals too", lang, written)
			assert.NotEmpty(t, word)
		}
	}
}
