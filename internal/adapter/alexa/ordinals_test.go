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

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected string
	}{
		{name: "ShouldRead1stInEnAU", locale: "en-AU", have: "the 1st episode of bluey", expected: "bluey"},
		{name: "ShouldRead1eInNlNL", locale: "nl-NL", have: "de 1e aflevering van flikken maastricht", expected: "flikken maastricht"},
		{name: "ShouldRead1steInNlNL", locale: "nl-NL", have: "de 1ste aflevering van flikken maastricht", expected: "flikken maastricht"},
		{name: "ShouldRead1WithOrdinalIndicatorInPtBR", locale: "pt-BR", have: "o 1º episódio de irmandade", expected: "irmandade"},
		{name: "ShouldRead1WithDegreeSignInPtBR", locale: "pt-BR", have: "o 1° episódio de irmandade", expected: "irmandade"},
		{name: "ShouldRead1erInFrFR", locale: "fr-FR", have: "le 1er épisode de kaamelott", expected: "kaamelott"},
		{name: "ShouldRead1erInFrCA", locale: "fr-CA", have: "le 1er épisode de district 31", expected: "district 31"},
		{name: "ShouldRead1erInEsES", locale: "es-ES", have: "el 1er capítulo de la casa de papel", expected: "la casa de papel"},
		{name: "ShouldRead1WithOrdinalIndicatorInEsMX", locale: "es-MX", have: "el 1º episodio de club de cuervos", expected: "club de cuervos"},
		{name: "ShouldRead1DotErInEsUS", locale: "es-US", have: "el 1.er episodio de club de cuervos", expected: "club de cuervos"},
		{name: "ShouldRead1WithOrdinalIndicatorInItIT", locale: "it-IT", have: "il 1º episodio di gomorra", expected: "gomorra"},
		{name: "ShouldRead1WithDegreeSignInItIT", locale: "it-IT", have: "il 1° episodio di gomorra", expected: "gomorra"},
		{name: "ShouldRead1DotInDeDE", locale: "de-DE", have: "die 1. folge von dark", expected: "dark"},
		{name: "ShouldRead1laInHiIN", locale: "hi-IN", have: "पंचायत का 1ला एपिसोड", expected: "पंचायत"},
		{name: "ShouldReadDai1waInJaJP", locale: "ja-JP", have: "ブルーイの第1話", expected: "ブルーイ"},
		{name: "ShouldRead1wameInJaJP", locale: "ja-JP", have: "ブルーイの1話目", expected: "ブルーイ"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			intent, slots := reading(speech, tc.locale, tc.have)
			assert.Equal(t, "WatchFirstEpisode", intent)
			assert.Equal(t, map[string]string{"Show": tc.expected}, slots)
		})
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

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected map[string]string
	}{
		{name: "ShouldReadSeasonNumberInEnAU", locale: "en-AU", have: "season 2 of bluey", expected: map[string]string{"Show": "bluey", "Season": "2"}},
		{name: "ShouldReadSeriesNumberInEnGB", locale: "en-GB", have: "series 2 of bluey", expected: map[string]string{"Show": "bluey", "Season": "2"}},
		{name: "ShouldReadSeasonNumberAfterShowInEnUS", locale: "en-US", have: "bluey season 2", expected: map[string]string{"Show": "bluey", "Season": "2"}},
		{name: "ShouldReadOrdinalWordInEnAU", locale: "en-AU", have: "the second season of bluey", expected: map[string]string{"Show": "bluey", "Ordinal": "second"}},
		{name: "ShouldReadWrittenOrdinalInEnAU", locale: "en-AU", have: "the 2nd season of bluey", expected: map[string]string{"Show": "bluey", "Ordinal": "2nd"}},
		{name: "ShouldReadAllOfSeasonInEnAU", locale: "en-AU", have: "all of season 3 of bluey", expected: map[string]string{"Show": "bluey", "Season": "3"}},
		{name: "ShouldReadSeasonWithoutShowInEnAU", locale: "en-AU", have: "season 2", expected: map[string]string{"Season": "2"}},
		{name: "ShouldReadSeasonNumberInDeDE", locale: "de-DE", have: "staffel 2 von dark", expected: map[string]string{"Show": "dark", "Season": "2"}},
		{name: "ShouldReadOrdinalWordInDeDE", locale: "de-DE", have: "die zweite staffel von dark", expected: map[string]string{"Show": "dark", "Ordinal": "zweite"}},
		{name: "ShouldReadWrittenOrdinalInDeDE", locale: "de-DE", have: "die 2. staffel von dark", expected: map[string]string{"Show": "dark", "Ordinal": "2."}},
		{name: "ShouldReadSeasonNumberInFrFR", locale: "fr-FR", have: "la saison 2 de kaamelott", expected: map[string]string{"Show": "kaamelott", "Season": "2"}},
		{name: "ShouldReadOrdinalWordInFrFR", locale: "fr-FR", have: "la deuxième saison de kaamelott", expected: map[string]string{"Show": "kaamelott", "Ordinal": "deuxième"}},
		{name: "ShouldReadWrittenOrdinalInFrCA", locale: "fr-CA", have: "la 2e saison de district 31", expected: map[string]string{"Show": "district 31", "Ordinal": "2e"}},
		{name: "ShouldReadSeasonNumberInEsES", locale: "es-ES", have: "la temporada 2 de la casa de papel", expected: map[string]string{"Show": "la casa de papel", "Season": "2"}},
		{name: "ShouldReadOrdinalWordInEsES", locale: "es-ES", have: "la segunda temporada de la casa de papel", expected: map[string]string{"Show": "la casa de papel", "Ordinal": "segunda"}},
		{name: "ShouldReadWrittenOrdinalInEsMX", locale: "es-MX", have: "la 2da temporada de club de cuervos", expected: map[string]string{"Show": "club de cuervos", "Ordinal": "2da"}},
		{name: "ShouldReadWrittenOrdinalInEsUS", locale: "es-US", have: "la 2ª temporada de club de cuervos", expected: map[string]string{"Show": "club de cuervos", "Ordinal": "2ª"}},
		{name: "ShouldReadOrdinalWordInItIT", locale: "it-IT", have: "la seconda stagione di gomorra", expected: map[string]string{"Show": "gomorra", "Ordinal": "seconda"}},
		{name: "ShouldReadSeasonNumberInItIT", locale: "it-IT", have: "la stagione 2 di gomorra", expected: map[string]string{"Show": "gomorra", "Season": "2"}},
		{name: "ShouldReadOrdinalWordInPtBR", locale: "pt-BR", have: "a segunda temporada de sintonia", expected: map[string]string{"Show": "sintonia", "Ordinal": "segunda"}},
		{name: "ShouldReadWrittenOrdinalInPtBR", locale: "pt-BR", have: "a 2ª temporada de sintonia", expected: map[string]string{"Show": "sintonia", "Ordinal": "2ª"}},
		{name: "ShouldReadSeasonNumberInNlNL", locale: "nl-NL", have: "seizoen 2 van flikken maastricht", expected: map[string]string{"Show": "flikken maastricht", "Season": "2"}},
		{name: "ShouldReadWrittenOrdinalInNlNL", locale: "nl-NL", have: "het 2e seizoen van flikken maastricht", expected: map[string]string{"Show": "flikken maastricht", "Ordinal": "2e"}},
		{name: "ShouldReadOrdinalWordInArSA", locale: "ar-SA", have: "الموسم الثاني من بلوي", expected: map[string]string{"Show": "بلوي", "Ordinal": "الثاني"}},
		{name: "ShouldReadSeasonNumberInArSA", locale: "ar-SA", have: "الموسم 2 من بلوي", expected: map[string]string{"Show": "بلوي", "Season": "2"}},
		{name: "ShouldReadOrdinalWordInHiIN", locale: "hi-IN", have: "ब्लूई का दूसरा सीज़न", expected: map[string]string{"Show": "ब्लूई", "Ordinal": "दूसरा"}},
		{name: "ShouldReadSeasonNumberInHiIN", locale: "hi-IN", have: "ब्लूई का सीज़न 2", expected: map[string]string{"Show": "ब्लूई", "Season": "2"}},
		{name: "ShouldReadSeasonNumberInJaJP", locale: "ja-JP", have: "ブルーイのシーズン2", expected: map[string]string{"Show": "ブルーイ", "Season": "2"}},
		{name: "ShouldReadOrdinalWordInJaJP", locale: "ja-JP", have: "ブルーイのセカンドシーズン", expected: map[string]string{"Show": "ブルーイ", "Ordinal": "セカンド"}},
		{name: "ShouldReadDaiNKiInJaJP", locale: "ja-JP", have: "ブルーイの第2期", expected: map[string]string{"Show": "ブルーイ", "Season": "2"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			intent, slots := reading(speech, tc.locale, tc.have)
			assert.Equal(t, "WatchSeason", intent)
			assert.Equal(t, tc.expected, slots)
		})
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

	testCases := []struct {
		name   string
		locale string
		have   string
		intent string
		slots  map[string]string
	}{
		{name: "ShouldReadLowercaseCode", locale: "en-AU", have: "bluey s2e3", intent: "WatchEpisode", slots: map[string]string{"Show": "bluey", "Season": "2", "Episode": "3"}},
		{name: "ShouldReadUppercasePaddedCode", locale: "en-AU", have: "Bluey S02E03", intent: "WatchEpisode", slots: map[string]string{"Show": "Bluey", "Season": "2", "Episode": "3"}},
		{name: "ShouldReadSpokenLettersCode", locale: "en-US", have: "bluey s. 2 e. 3", intent: "WatchEpisode", slots: map[string]string{"Show": "bluey", "Season": "2", "Episode": "3"}},
		{name: "ShouldReadEpCode", locale: "en-GB", have: "bluey s2 ep3", intent: "WatchEpisode", slots: map[string]string{"Show": "bluey", "Season": "2", "Episode": "3"}},
		{name: "ShouldReadCodeInDeDE", locale: "de-DE", have: "dark s1e4", intent: "WatchEpisode", slots: map[string]string{"Show": "dark", "Season": "1", "Episode": "4"}},
		{name: "ShouldReadSeasonOnlyCodeAsSeason", locale: "en-AU", have: "bluey s2", intent: "WatchSeason", slots: map[string]string{"Show": "bluey", "Season": "2"}},
		{name: "ShouldKeepTitleEndingInNumberAsPlayMedia", locale: "en-AU", have: "ocean's 12", intent: "PlayMedia", slots: map[string]string{"Query": "ocean's 12"}},
		{name: "ShouldKeepCodeWithoutShowAsPlayMedia", locale: "en-AU", have: "s2e3", intent: "PlayMedia", slots: map[string]string{"Query": "s2e3"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			intent, slots := reading(speech, tc.locale, tc.have)
			assert.Equal(t, tc.intent, intent)
			assert.Equal(t, tc.slots, slots)
		})
	}
}

// TestSpeechOrdinal reads ordinals as words of the ORDINAL type and as Alexa writes them with digits in each language.
func TestSpeechOrdinal(t *testing.T) {
	speech := shippedSpeech(t)

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected int
		ok       bool
	}{
		{name: "ShouldReadSecondInEnAU", locale: "en-AU", have: "second", expected: 2, ok: true},
		{name: "ShouldRead2ndInEnAU", locale: "en-AU", have: "2nd", expected: 2, ok: true},
		{name: "ShouldRead3rdInEnAU", locale: "en-AU", have: "3rd", expected: 3, ok: true},
		{name: "ShouldRead21stInEnAU", locale: "en-AU", have: "21st", expected: 21, ok: true},
		{name: "ShouldReadPlainNumberInEnAU", locale: "en-AU", have: "4", expected: 4, ok: true},
		{name: "ShouldReadZweiteInDeDE", locale: "de-DE", have: "zweite", expected: 2, ok: true},
		{name: "ShouldReadInflectedZweitenInDeDE", locale: "de-DE", have: "zweiten", expected: 2, ok: true},
		{name: "ShouldReadNumberWithDotInDeDE", locale: "de-DE", have: "3.", expected: 3, ok: true},
		{name: "ShouldReadPremiereInFrFR", locale: "fr-FR", have: "première", expected: 1, ok: true},
		{name: "ShouldRead1reInFrFR", locale: "fr-FR", have: "1re", expected: 1, ok: true},
		{name: "ShouldRead1erInFrFR", locale: "fr-FR", have: "1er", expected: 1, ok: true},
		{name: "ShouldRead2eInFrFR", locale: "fr-FR", have: "2e", expected: 2, ok: true},
		{name: "ShouldRead2emeInFrCA", locale: "fr-CA", have: "2ème", expected: 2, ok: true},
		{name: "ShouldReadSecondeInFrFR", locale: "fr-FR", have: "seconde", expected: 2, ok: true},
		{name: "ShouldReadTerceraInEsES", locale: "es-ES", have: "tercera", expected: 3, ok: true},
		{name: "ShouldRead3WithFeminineIndicatorInEsES", locale: "es-ES", have: "3ª", expected: 3, ok: true},
		{name: "ShouldRead1DotWithMasculineIndicatorInEsES", locale: "es-ES", have: "1.º", expected: 1, ok: true},
		{name: "ShouldRead3raInEsMX", locale: "es-MX", have: "3ra", expected: 3, ok: true},
		{name: "ShouldRead2doInEsUS", locale: "es-US", have: "2do", expected: 2, ok: true},
		{name: "ShouldReadTerzaInItIT", locale: "it-IT", have: "terza", expected: 3, ok: true},
		{name: "ShouldRead3WithOrdinalIndicatorInItIT", locale: "it-IT", have: "3º", expected: 3, ok: true},
		{name: "ShouldRead3WithDegreeSignInItIT", locale: "it-IT", have: "3°", expected: 3, ok: true},
		{name: "ShouldReadTerceiraInPtBR", locale: "pt-BR", have: "terceira", expected: 3, ok: true},
		{name: "ShouldRead3WithFeminineIndicatorInPtBR", locale: "pt-BR", have: "3ª", expected: 3, ok: true},
		{name: "ShouldReadDerdeInNlNL", locale: "nl-NL", have: "derde", expected: 3, ok: true},
		{name: "ShouldRead3eInNlNL", locale: "nl-NL", have: "3e", expected: 3, ok: true},
		{name: "ShouldRead8steInNlNL", locale: "nl-NL", have: "8ste", expected: 8, ok: true},
		{name: "ShouldReadThirdInArSA", locale: "ar-SA", have: "الثالث", expected: 3, ok: true},
		{name: "ShouldReadFirstWithoutHamzaInArSA", locale: "ar-SA", have: "الاول", expected: 1, ok: true},
		{name: "ShouldReadTisraInHiIN", locale: "hi-IN", have: "तीसरा", expected: 3, ok: true},
		{name: "ShouldReadPanchvanInHiIN", locale: "hi-IN", have: "पांचवां", expected: 5, ok: true},
		{name: "ShouldRead3raInHiIN", locale: "hi-IN", have: "3रा", expected: 3, ok: true},
		{name: "ShouldReadSaadoInJaJP", locale: "ja-JP", have: "サード", expected: 3, ok: true},
		{name: "ShouldReadDaiSanInJaJP", locale: "ja-JP", have: "第三", expected: 3, ok: true},
		{name: "ShouldReadDai3InJaJP", locale: "ja-JP", have: "第3", expected: 3, ok: true},
		{name: "ShouldReadThirdInUnknownLocale", locale: "xx-XX", have: "third", expected: 3, ok: true},
		{name: "ShouldRead21stInUnknownLocale", locale: "xx-XX", have: "21st", expected: 21, ok: true},
		{name: "ShouldRejectEmpty", locale: "en-AU"},
		{name: "ShouldRejectWordThatIsNotAnOrdinal", locale: "en-AU", have: "endless"},
		{name: "ShouldRejectFrenchSuffixInEnAU", locale: "en-AU", have: "2e"},
		{name: "ShouldRejectEnglishSuffixInDeDE", locale: "de-DE", have: "2nd"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := speech.Ordinal(tc.locale, tc.have)

			if !tc.ok {
				assert.False(t, ok)

				return
			}

			assert.True(t, ok)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

// TestWrittenOrdinalPlainNumber reads a plain number as itself in any language, and never a negative one.
func TestWrittenOrdinalPlainNumber(t *testing.T) {
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldReadPlainNumberInEnglish", have: "en"},
		{name: "ShouldReadPlainNumberInArabic", have: "ar"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := writtenOrdinal(tc.have, "4")
			assert.True(t, ok)
			assert.Equal(t, 4, n)

			_, ok = writtenOrdinal(tc.have, "-1")
			assert.False(t, ok)
		})
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
