package match

import (
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
)

func TestNormalize(t *testing.T) {
	testCases := []struct {
		name     string
		lang     string
		have     string
		expected string
	}{
		// Case, punctuation, diacritics and articles.
		{name: "ShouldNormalizeTheMatrix", lang: "en", have: "The Matrix", expected: "matrix"},
		{name: "ShouldNormalizeShoutedTheMatrix", lang: "en", have: "  THE   matrix!! ", expected: "matrix"},
		{name: "ShouldNormalizeAmelie", lang: "en", have: "Amélie", expected: "amelie"},
		{name: "ShouldNormalizeSigurRos", lang: "en", have: "Sigur Rós", expected: "sigur ros"},
		{name: "ShouldNormalizeMotleyCrue", lang: "en", have: "Mötley Crüe", expected: "motley crue"},
		{name: "ShouldNormalizeOceansEleven", lang: "en", have: "Ocean's Eleven", expected: "oceans 11"},
		{name: "ShouldNormalizeFastAndFurious", lang: "en", have: "Fast & Furious", expected: "fast and furious"},
		{name: "ShouldNormalizeSpiderManIntoTheSpiderVerse", lang: "en", have: "Spider-Man: Into the Spider-Verse", expected: "spider man into the spider verse"},
		{name: "ShouldNormalizeAQuietPlace", lang: "en", have: "A Quiet Place", expected: "quiet place"},
		{name: "ShouldNormalizeAnAmericanWerewolfInLondon", lang: "en", have: "An American Werewolf in London", expected: "american werewolf in london"},
		{name: "ShouldNormalizeTheThe", lang: "en", have: "The The", expected: "the"},
		{name: "ShouldNormalizeThe", lang: "en", have: "The", expected: "the"},
		{name: "ShouldNormalizeAeonFlux", lang: "en", have: "Æon Flux", expected: "aeon flux"},
		{name: "ShouldNormalizeEmpty", lang: "en"},
		{name: "ShouldNormalizePunctuationToEmpty", lang: "en", have: "!!!"},
		// Letter runs are joined.
		{name: "ShouldNormalizeETTheExtraTerrestrial", lang: "en", have: "E.T. the Extra-Terrestrial", expected: "et the extra terrestrial"},
		{name: "ShouldNormalizeMASH", lang: "en", have: "M*A*S*H", expected: "mash"},
		{name: "ShouldNormalizeACDC", lang: "en", have: "AC/DC", expected: "ac dc"},
		{name: "ShouldJoinSpacedLetterRun", lang: "en", have: "a c d c", expected: "acdc"},
		// Roman numerals only after another word, and single letters survive.
		{name: "ShouldNormalizeRockyII", lang: "en", have: "Rocky II", expected: "rocky 2"},
		{name: "ShouldNormalizeStarWarsEpisodeIVANewHope", lang: "en", have: "Star Wars: Episode IV - A New Hope", expected: "star wars episode 4 a new hope"},
		{name: "ShouldNormalizeFinalFantasyXX", lang: "en", have: "Final Fantasy XX", expected: "final fantasy 20"},
		{name: "ShouldNormalizeRockyXXI", lang: "en", have: "Rocky XXI", expected: "rocky xxi"},
		{name: "ShouldNormalizeIRobot", lang: "en", have: "I, Robot", expected: "i robot"},
		{name: "ShouldNormalizeMalcolmX", lang: "en", have: "Malcolm X", expected: "malcolm x"},
		{name: "ShouldNormalizeRockyV", lang: "en", have: "Rocky V", expected: "rocky v"},
		{name: "ShouldNormalizeIIFast", lang: "en", have: "II Fast", expected: "ii fast"},
		{name: "ShouldNormalizeMix", lang: "en", have: "Mix", expected: "mix"},
		// Number words, ordinals and parts.
		{name: "ShouldNormalizeRockyTwo", lang: "en", have: "Rocky Two", expected: "rocky 2"},
		{name: "ShouldNormalizeRockySecond", lang: "en", have: "rocky second", expected: "rocky 2"},
		{name: "ShouldNormalizeToyStoryThree", lang: "en", have: "toy story three", expected: "toy story 3"},
		{name: "ShouldNormalizeTwentyOnePilots", lang: "en", have: "Twenty One Pilots", expected: "21 pilots"},
		{name: "ShouldNormalizeTwentyFirstCentury", lang: "en", have: "twenty-first century", expected: "21 century"},
		{name: "ShouldNormalizeOneHundredAndOneDalmatians", lang: "en", have: "one hundred and one dalmatians", expected: "101 dalmatians"},
		{name: "ShouldNormalizeTwoThousandAndOneASpaceOdyssey", lang: "en", have: "two thousand and one a space odyssey", expected: "2001 a space odyssey"},
		{name: "ShouldNormalizeNineteenEightyFour", lang: "en", have: "nineteen eighty four", expected: "1984"},
		{name: "ShouldNormalizeBladeRunnerTwentyFortyNine", lang: "en", have: "blade runner twenty forty nine", expected: "blade runner 2049"},
		{name: "ShouldNormalizeOneTwoThree", lang: "en", have: "one two three", expected: "1 2 3"},
		{name: "ShouldNormalizeZeroDarkThirty", lang: "en", have: "zero dark thirty", expected: "0 dark 30"},
		{name: "ShouldNormalizeFiftyFirstDates", lang: "en", have: "fifty first dates", expected: "51 dates"},
		{name: "ShouldNormalizeDunePartTwo", lang: "en", have: "Dune: Part Two", expected: "dune 2"},
		{name: "ShouldNormalizeKillBillVol1", lang: "en", have: "Kill Bill: Vol. 1", expected: "kill bill 1"},
		{name: "ShouldNormalizePartPartTwo", lang: "en", have: "part part two", expected: "2"},
		{name: "ShouldNormalizeHarryPotterAndTheDeathlyHallowsPart1", lang: "en", have: "Harry Potter and the Deathly Hallows Part 1", expected: "harry potter and the deathly hallows 1"},
		{name: "ShouldNormalizeChapter", lang: "en", have: "Chapter", expected: "chapter"},
		{name: "ShouldNormalizeHundredAnd", lang: "en", have: "hundred and", expected: "100 and"},
		{name: "ShouldNormalizeTwentyEleven", lang: "en", have: "twenty eleven", expected: "2011"},
		{name: "ShouldNormalizeOneTwenty", lang: "en", have: "one twenty", expected: "1 20"},
		{name: "ShouldNormalizeTwoHundredHundred", lang: "en", have: "two hundred hundred", expected: "200 100"},
		{name: "ShouldNormalizeOneThousandThousand", lang: "en", have: "one thousand thousand", expected: "1000 1000"},
		{name: "ShouldNormalizeTwoThousandFiveThousand", lang: "en", have: "two thousand five thousand", expected: "2005 1000"},
		{name: "ShouldNormalizeTwoThousandOneHundredThousand", lang: "en", have: "two thousand one hundred thousand", expected: "2100 1000"},
		{name: "ShouldNormalizeNineteenTwentieth", lang: "en", have: "nineteen twentieth", expected: "19 20"},
		{name: "ShouldNormalizeTwentyAndTwelve", lang: "en", have: "twenty and twelve", expected: "20 and 12"},
		{name: "ShouldNormalizeTwentyAndOne", lang: "en", have: "twenty and one", expected: "21"},
		// German: umlauts, ß, articles, compounds and ordinals.
		{name: "ShouldNormalizeDieBruckeInGerman", lang: "de", have: "Die Brücke", expected: "brucke"},
		{name: "ShouldNormalizeDerSchuhDesManituInGerman", lang: "de", have: "Der Schuh des Manitu", expected: "schuh des manitu"},
		{name: "ShouldNormalizeStrasseInGerman", lang: "de", have: "Straße", expected: "strasse"},
		{name: "ShouldNormalizeBibiAndTinaInGerman", lang: "de", have: "Bibi & Tina", expected: "bibi und tina"},
		{name: "ShouldNormalizeEinFischNamensWandaInGerman", lang: "de", have: "Ein Fisch namens Wanda", expected: "fisch namens wanda"},
		{name: "ShouldNormalizeDieFantastischenVierInGerman", lang: "de", have: "Die Fantastischen Vier", expected: "fantastischen 4"},
		{name: "ShouldNormalizeEinundzwanzigInGerman", lang: "de", have: "einundzwanzig", expected: "21"},
		{name: "ShouldNormalizeDerZweiteTeilInGerman", lang: "de", have: "Der zweite Teil", expected: "2 teil"},
		{name: "ShouldNormalizeFackJuGohteTeilDreiInGerman", lang: "de", have: "Fack ju Göhte Teil drei", expected: "fack ju gohte 3"},
		{name: "ShouldNormalizeHundertUndEinsInGerman", lang: "de", have: "hundert und eins", expected: "101"},
		// German numbers written as one word, and units said before "und" and the tens.
		{name: "ShouldNormalizeNeunzehnhundertvierundachtzigInGerman", lang: "de", have: "neunzehnhundertvierundachtzig", expected: "1984"},
		{name: "ShouldNormalizeEmilUndDieDetektiveNeunzehnhunderteinunddreissigInGerman", lang: "de", have: "Emil und die Detektive neunzehnhunderteinunddreißig", expected: "emil und die detektive 1931"},
		{name: "ShouldNormalizeZweitausendeinsInGerman", lang: "de", have: "zweitausendeins", expected: "2001"},
		{name: "ShouldNormalizeApacheZweihundertsiebenInGerman", lang: "de", have: "Apache zweihundertsieben", expected: "apache 207"},
		{name: "ShouldNormalizeEinhundertzwanzigInGerman", lang: "de", have: "einhundertzwanzig", expected: "120"},
		{name: "ShouldNormalizeTausendundeineNachtInGerman", lang: "de", have: "tausendundeine Nacht", expected: "1001 nacht"},
		{name: "ShouldNormalizeTausendUndEineNachtInGerman", lang: "de", have: "tausend und eine Nacht", expected: "1001 nacht"},
		{name: "ShouldNormalizeNeunUndNeunzigLuftballonsInGerman", lang: "de", have: "neun und neunzig Luftballons", expected: "99 luftballons"},
		{name: "ShouldNormalizeHundertVierUndZwanzigInGerman", lang: "de", have: "hundert vier und zwanzig", expected: "124"},
		{name: "ShouldNormalizeEinsUndEinsInGerman", lang: "de", have: "eins und eins", expected: "1 und 1"},
		{name: "ShouldNormalizeZehnUndZwanzigInGerman", lang: "de", have: "zehn und zwanzig", expected: "10 und 20"},
		{name: "ShouldNormalizeDuHastMichTausendmalBelogenInGerman", lang: "de", have: "Du hast mich tausendmal belogen", expected: "du hast mich tausendmal belogen"},
		{name: "ShouldNormalizeElfenbeinInGerman", lang: "de", have: "Elfenbein", expected: "elfenbein"},
		{name: "ShouldNormalizeAchtungInGerman", lang: "de", have: "Achtung", expected: "achtung"},
		{name: "ShouldNormalizeZweiundzwanzigDreiInGerman", lang: "de", have: "zweiundzwanzig drei", expected: "22 3"},
		// French: elision and articles.
		{name: "ShouldNormalizeLAubergeEspagnoleInFrench", lang: "fr", have: "L'Auberge espagnole", expected: "auberge espagnole"},
		{name: "ShouldNormalizeLeFabuleuxDestinDAmeliePoulainInFrench", lang: "fr", have: "Le Fabuleux Destin d'Amélie Poulain", expected: "fabuleux destin d amelie poulain"},
		{name: "ShouldNormalizeLesQuatreCentsCoupsInFrench", lang: "fr", have: "Les Quatre Cents Coups", expected: "400 coups"},
		{name: "ShouldNormalizeUnHommeEtUneFemmeInFrench", lang: "fr", have: "Un homme et une femme", expected: "homme et une femme"},
		{name: "ShouldNormalizeVingtEtUnInFrench", lang: "fr", have: "vingt et un", expected: "21"},
		{name: "ShouldNormalizeDixSeptInFrench", lang: "fr", have: "dix sept", expected: "17"},
		{name: "ShouldNormalizeAsterixAndObelixInFrench", lang: "fr", have: "Astérix & Obélix", expected: "asterix et obelix"},
		// Spanish and Italian.
		{name: "ShouldNormalizeElLaberintoDelFaunoInSpanish", lang: "es", have: "El Laberinto del Fauno", expected: "laberinto del fauno"},
		{name: "ShouldNormalizeVeintiunoInSpanish", lang: "es", have: "Veintiuno", expected: "21"},
		{name: "ShouldNormalizeTreintaYUnoInSpanish", lang: "es", have: "treinta y uno", expected: "31"},
		{name: "ShouldNormalizeUnoVeintiunoInSpanish", lang: "es", have: "uno veintiuno", expected: "1 21"},
		{name: "ShouldNormalizeVeinteVeintiunoInSpanish", lang: "es", have: "veinte veintiuno", expected: "2021"},
		{name: "ShouldNormalizeLaCasaDePapelParteDosInSpanish", lang: "es", have: "La Casa de Papel parte dos", expected: "casa de papel 2"},
		// Spanish hundreds from doscientos to novecientos, in either gender.
		{name: "ShouldNormalizeArgentinaMilNovecientosOchentaYCincoInSpanish", lang: "es", have: "argentina mil novecientos ochenta y cinco", expected: "argentina 1985"},
		{name: "ShouldNormalizeDiecinueveDiasYQuinientasNochesInSpanish", lang: "es", have: "diecinueve días y quinientas noches", expected: "19 dias y 500 noches"},
		{name: "ShouldNormalizeDoscientosInSpanish", lang: "es", have: "doscientos", expected: "200"},
		{name: "ShouldNormalizeSetecientasInSpanish", lang: "es", have: "setecientas", expected: "700"},
		{name: "ShouldNormalizeNovecientosNoventaYNueveInSpanish", lang: "es", have: "novecientos noventa y nueve", expected: "999"},
		{name: "ShouldNormalizeDosMilVeintitresInSpanish", lang: "es", have: "dos mil veintitrés", expected: "2023"},
		{name: "ShouldNormalizeCientoVeinteInSpanish", lang: "es", have: "ciento veinte", expected: "120"},
		{name: "ShouldNormalizeLaVitaEBellaInItalian", lang: "it", have: "La Vita è Bella", expected: "vita e bella"},
		{name: "ShouldNormalizeLultimoBacioInItalian", lang: "it", have: "L'ultimo bacio", expected: "ultimo bacio"},
		{name: "ShouldNormalizeGliIntoccabiliInItalian", lang: "it", have: "Gli Intoccabili", expected: "intoccabili"},
		{name: "ShouldNormalizeVentunoInItalian", lang: "it", have: "ventuno", expected: "21"},
		{name: "ShouldNormalizeIlSecondoTragicoInItalian", lang: "it", have: "il secondo tragico", expected: "2 tragico"},
		// Italian numbers written as one word, with the hundreds, thousands and millions they are built from.
		{name: "ShouldNormalizeMillenovecentonovantadueInItalian", lang: "it", have: "millenovecentonovantadue", expected: "1992"},
		{name: "ShouldNormalizeMilleNovecentoNovantadueAsWordsInItalian", lang: "it", have: "mille novecento novantadue", expected: "1992"},
		{name: "ShouldNormalizeVacanzeDiNataleMillenovecentonovantacinqueInItalian", lang: "it", have: "vacanze di natale millenovecentonovantacinque", expected: "vacanze di natale 1995"},
		{name: "ShouldNormalizeNovecentoInItalian", lang: "it", have: "Novecento", expected: "900"},
		{name: "ShouldNormalizeNoveCentoAsTwoWordsInItalian", lang: "it", have: "nove cento", expected: "900"},
		{name: "ShouldNormalizeCentoventiseiInItalian", lang: "it", have: "centoventisei", expected: "126"},
		{name: "ShouldNormalizeFrancoCentoventiseiInItalian", lang: "it", have: "franco centoventisei", expected: "franco 126"},
		{name: "ShouldNormalizeOttocentoOttantatreInItalian", lang: "it", have: "ottocento ottantatré", expected: "883"},
		{name: "ShouldNormalizeCentottantaInItalian", lang: "it", have: "centottanta", expected: "180"},
		{name: "ShouldNormalizeMillenovecentottantottoInItalian", lang: "it", have: "millenovecentottantotto", expected: "1988"},
		{name: "ShouldNormalizeMilioneMilioniInItalian", lang: "it", have: "milione milioni", expected: "1000000 1000000"},
		{name: "ShouldNormalizeCentounoInItalian", lang: "it", have: "centouno", expected: "101"},
		{name: "ShouldNormalizeCentunoInItalian", lang: "it", have: "centuno", expected: "101"},
		{name: "ShouldNormalizeDuemilaInItalian", lang: "it", have: "duemila", expected: "2000"},
		{name: "ShouldNormalizeDuemilaventiInItalian", lang: "it", have: "duemilaventi", expected: "2020"},
		{name: "ShouldNormalizeTremilaInItalian", lang: "it", have: "tremila", expected: "3000"},
		{name: "ShouldNormalizeDueMilioniCinquecentomilaInItalian", lang: "it", have: "due milioni cinquecentomila", expected: "2500000"},
		{name: "ShouldNormalizeUnMilioneDiDollariInItalian", lang: "it", have: "un milione di dollari", expected: "1000000 di dollari"},
		{name: "ShouldNormalizeMilleEUnaNotteInItalian", lang: "it", have: "mille e una notte", expected: "1001 notte"},
		{name: "ShouldNormalizeZerozerozeroInItalian", lang: "it", have: "zerozerozero", expected: "0 0 0"},
		{name: "ShouldNormalizeOttoottotreInItalian", lang: "it", have: "ottoottotre", expected: "8 8 3"},
		{name: "ShouldNormalizeCentottantaGradiInItalian", lang: "it", have: "centottanta gradi", expected: "180 gradi"},
		{name: "ShouldNormalizeTreDuecentoInItalian", lang: "it", have: "tre duecento", expected: "3 200"},
		{name: "ShouldNormalizeMilleMilioniInItalian", lang: "it", have: "mille milioni", expected: "1000 1000000"},
		// Words that only start with number words stay words.
		{name: "ShouldNormalizeTrenoInItalian", lang: "it", have: "treno", expected: "treno"},
		{name: "ShouldNormalizeSettembreInItalian", lang: "it", have: "settembre", expected: "settembre"},
		{name: "ShouldNormalizeCentroInItalian", lang: "it", have: "centro", expected: "centro"},
		{name: "ShouldNormalizeDuettoInItalian", lang: "it", have: "duetto", expected: "duetto"},
		{name: "ShouldNormalizeUnotreInItalian", lang: "it", have: "unotre", expected: "unotre"},
		{name: "ShouldNormalizeTreedueInItalian", lang: "it", have: "treedue", expected: "treedue"},
		// Ordinals written with digits read as the number, as spelled-out ordinals do.
		{name: "ShouldNormalizeThe6thSense", lang: "en", have: "The 6th Sense", expected: "6 sense"},
		{name: "ShouldNormalizeTheSixthSense", lang: "en", have: "the sixth sense", expected: "6 sense"},
		{name: "ShouldNormalize21stCenturyBreakdown", lang: "en", have: "21st Century Breakdown", expected: "21 century breakdown"},
		{name: "ShouldNormalize3rdRockFromTheSun", lang: "en", have: "3rd Rock from the Sun", expected: "3 rock from the sun"},
		{name: "ShouldNormalize2nd", lang: "en", have: "2nd", expected: "2"},
		{name: "ShouldNormalize5e", lang: "en", have: "5e", expected: "5e"},
		{name: "ShouldNormalize4x4", lang: "en", have: "4x4", expected: "4x4"},
		{name: "ShouldNormalizeLe5emeElementInFrench", lang: "fr", have: "Le 5ème Élément", expected: "5 element"},
		{name: "ShouldNormalizeLe5eElementLowerCaseInFrench", lang: "fr", have: "le 5e élément", expected: "5 element"},
		{name: "ShouldNormalizeLeCinquiemeElementInFrench", lang: "fr", have: "Le Cinquième Élément", expected: "5 element"},
		{name: "ShouldNormalize1erInFrench", lang: "fr", have: "1er", expected: "1"},
		{name: "ShouldNormalize1reInFrench", lang: "fr", have: "1re", expected: "1"},
		{name: "ShouldNormalizeLa2aTemporadaInSpanish", lang: "es", have: "la 2ª temporada", expected: "2 temporada"},
		{name: "ShouldNormalize3erInSpanish", lang: "es", have: "3er", expected: "3"},
		{name: "ShouldNormalizeIl2oTempoInItalian", lang: "it", have: "il 2º tempo", expected: "2 tempo"},
		{name: "ShouldNormalize5teInGerman", lang: "de", have: "5te", expected: "5te"},
		// ½ reads as each language says "and a half", so the title meets the spoken form.
		{name: "ShouldNormalize8HalfInItalian", lang: "it", have: "8½", expected: "8 e mezzo"},
		{name: "ShouldNormalizeOttoEMezzoInItalian", lang: "it", have: "otto e mezzo", expected: "8 e mezzo"},
		{name: "ShouldNormalize8HalfWomen", lang: "en", have: "8½ Women", expected: "8 and a half women"},
		{name: "ShouldNormalizeEightAndAHalfWomen", lang: "en", have: "eight and a half women", expected: "8 and a half women"},
		{name: "ShouldNormalizeDie8HalfFrauenInGerman", lang: "de", have: "Die 8½ Frauen", expected: "8 einhalb frauen"},
		{name: "ShouldNormalizeHuitEtDemiInFrench", lang: "fr", have: "Huit et demi", expected: "8 et demi"},
		{name: "ShouldNormalize8HalfInFrench", lang: "fr", have: "8½", expected: "8 et demi"},
		{name: "ShouldNormalize8HalfInSpanish", lang: "es", have: "8½", expected: "8 y medio"},
		{name: "ShouldNormalizeOchoYMedioInSpanish", lang: "es", have: "ocho y medio", expected: "8 y medio"},
		// Every script's decimal digits are ASCII digits, and read as numbers.
		{name: "ShouldNormalizeExtendedArabicIndicDigits", lang: "en", have: "۱۹۸۴", expected: "1984"},
		{name: "ShouldNormalizeFullWidthDigits", lang: "en", have: "１９８４", expected: "1984"},
		{name: "ShouldNormalizeRockyWithArabicIndicDigit", lang: "en", have: "rocky ٢", expected: "rocky 2"},
		{name: "ShouldNormalizeRockyWithFullWidthOrdinal", lang: "en", have: "Rocky ２nd", expected: "rocky 2"},
		// Locale codes and unknown languages.
		{name: "ShouldNormalizeDieArzteInGermanLocale", lang: "de-DE", have: "Die Ärzte", expected: "arzte"},
		{name: "ShouldNormalizeTheCrownInBritishLocale", lang: "en_GB", have: "The Crown", expected: "crown"},
		{name: "ShouldNormalizeTheCrownInUnknownLanguage", lang: "xx", have: "The Crown", expected: "crown"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize(tc.lang, tc.have))
		})
	}
}

func TestASCIIDigit(t *testing.T) {
	// asciiDigit relies on every unbroken run of decimal digits being whole runs of ten from zero.
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.IsDigit(r) || unicode.IsDigit(r-1) {
			continue
		}

		hi := r
		for unicode.IsDigit(hi + 1) {
			hi++
		}

		assert.Zero(t, (hi-r+1)%10, "%U-%U", r, hi)
	}

	testCases := []struct {
		name     string
		have     rune
		expected rune
	}{
		{name: "ShouldKeepASCIIDigit", have: '7', expected: '7'},
		{name: "ShouldMapDevanagariZero", have: '०', expected: '0'},
		{name: "ShouldMapDevanagariNine", have: '९', expected: '9'},
		{name: "ShouldMapArabicIndicThree", have: '٣', expected: '3'},
		{name: "ShouldMapExtendedArabicIndicSeven", have: '۷', expected: '7'},
		{name: "ShouldMapBengaliFive", have: '৫', expected: '5'},
		{name: "ShouldMapThaiNine", have: '๙', expected: '9'},
		{name: "ShouldMapMathematicalDoubleStruckEight", have: '𝟠', expected: '8'},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, asciiDigit(tc.have))
		})
	}
}

func TestLanguageHooks(t *testing.T) {
	testCases := []struct {
		name     string
		edit     func(l *language)
		have     string
		expected string
	}{
		{name: "ShouldNormalizeWithoutHooks", edit: func(*language) {}, have: "Xy Zw", expected: "xy zw"},
		{name: "ShouldApplyFolds", edit: func(l *language) { l.folds = map[rune]string{'x': "ks", 'カ': "か"} }, have: "Xylo", expected: "ksylo"},
		{name: "ShouldApplyFoldsBeforeLetterFolds", edit: func(l *language) { l.folds = map[rune]string{'ß': "s"} }, have: "Straße", expected: "strase"},
		{name: "ShouldKeepVoicingWhenFolding", edit: func(l *language) { l.folds = map[rune]string{'カ': "か"} }, have: "ガカ", expected: "がか"},
		{name: "ShouldApplySplit", edit: func(l *language) { l.split = splitEvery(2) }, have: "abcdef gh", expected: "ab cd ef gh"},
		{name: "ShouldSplitBeforeJoiningLetterRuns", edit: func(l *language) { l.split = splitEvery(1) }, have: "abc", expected: "abc"},
		{name: "ShouldStripArticlePrefixes", edit: func(l *language) { l.articlePrefixes = []string{"al", "wal"} }, have: "alfoo walbar baz", expected: "foo bar baz"},
		{name: "ShouldStripLongestArticlePrefix", edit: func(l *language) { l.articlePrefixes = []string{"w", "wal"} }, have: "walbar", expected: "bar"},
		{name: "ShouldKeepArticlePrefixBelowMinimum", edit: func(l *language) { l.articlePrefixes = []string{"al"}; l.articlePrefixMin = 3 }, have: "alto alfoo", expected: "alto foo"},
		{name: "ShouldNeverEmptyWordByStrippingArticlePrefix", edit: func(l *language) { l.articlePrefixes = []string{"al"} }, have: "al", expected: "al"},
		{name: "ShouldStripArticlePrefixBeforeArticles", edit: func(l *language) { l.articlePrefixes = []string{"x"} }, have: "xthe matrix", expected: "matrix"},
		{name: "ShouldApplyRomanize", edit: func(l *language) { l.romanize = strings.ToUpper }, have: "abc de", expected: "ABC DE"},
		{name: "ShouldDropWordsRomanizedToEmpty", edit: func(l *language) { l.romanize = func(t string) string { return strings.TrimPrefix(t, "x") } }, have: "x abc", expected: "abc"},
		{name: "ShouldSplitRomanizedWords", edit: func(l *language) { l.romanize = func(t string) string { return t + " " + t } }, have: "ab", expected: "ab ab"},
		{name: "ShouldRomanizeAfterStrippingArticlePrefixes", edit: func(l *language) {
			l.articlePrefixes = []string{"ال"}
			l.romanize = func(t string) string { return map[string]string{"ملك": "malik", "الملك": "almalik"}[t] }
		}, have: "الملك", expected: "malik"},
		{name: "ShouldCountArticlePrefixMinimumInRunes", edit: func(l *language) { l.articlePrefixes = []string{"ال"}; l.articlePrefixMin = 2 }, have: "الملك الم", expected: "ملك الم"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			l := english()
			tc.edit(l)

			got := l.normalize(tc.have, false)
			if l.foreignOwner(tc.have) != nil {
				// Another language reads this script in a normalised string, so the language's own hooks are checked alone.
				got = l.ownWords(l.tokenize(tc.have))
			}

			assert.Equal(t, tc.expected, strings.Join(got, " "))
		})
	}
}

// splitEvery splits each token into pieces of n bytes, standing in for a word segmenter.
func splitEvery(n int) func([]string) []string {
	return func(toks []string) []string {
		var out []string

		for _, tok := range toks {
			for len(tok) > n {
				out = append(out, tok[:n])
				tok = tok[n:]
			}

			out = append(out, tok)
		}

		return out
	}
}

func TestNormalizeAggressiveRomans(t *testing.T) {
	l := languageFor("en")

	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldReadSingleLetterRomanAfterWord", have: "Rocky V", expected: []string{"rocky", "5"}},
		{name: "ShouldReadXAfterWord", have: "Malcolm X", expected: []string{"malcolm", "10"}},
		{name: "ShouldNotReadLeadingRoman", have: "I, Robot", expected: []string{"i", "robot"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, l.normalize(tc.have, true))
		})
	}
}

func TestShortenings(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldSplitAtColon", have: "Dune: Part Two", expected: []string{"Dune", "Part Two"}},
		{name: "ShouldSplitAtColonAndDash", have: "Star Wars: Episode IV - A New Hope", expected: []string{"Star Wars", "Episode IV - A New Hope", "Star Wars: Episode IV", "A New Hope"}},
		{name: "ShouldDropParenthesis", have: "The Office (US)", expected: []string{"The Office"}},
		{name: "ShouldDropUnclosedParenthesis", have: "The Office (US", expected: []string{"The Office"}},
		{name: "ShouldShortenNothingWithoutSeparator", have: "Seinfeld"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.expected, shortenings(tc.have))
		})
	}
}

func TestUmlautVariant(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldSpellLowerCaseUmlaut", have: "Die Brücke", expected: "Die Bruecke"},
		{name: "ShouldSpellUpperCaseUmlauts", have: "Öl und Äpfel", expected: "Oel und Aepfel"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, umlautVariant(tc.have))
		})
	}
}

func TestNormalizeFrenchSeventyToNinetyNine(t *testing.T) {
	spelled := map[int]string{
		70: "soixante-dix", 71: "soixante et onze", 72: "soixante-douze", 73: "soixante-treize", 74: "soixante-quatorze",
		75: "soixante-quinze", 76: "soixante-seize", 77: "soixante-dix-sept", 78: "soixante-dix-huit", 79: "soixante-dix-neuf",
		80: "quatre-vingts", 81: "quatre-vingt-un", 82: "quatre-vingt-deux", 83: "quatre-vingt-trois", 84: "quatre-vingt-quatre",
		85: "quatre-vingt-cinq", 86: "quatre-vingt-six", 87: "quatre-vingt-sept", 88: "quatre-vingt-huit", 89: "quatre-vingt-neuf",
		90: "quatre-vingt-dix", 91: "quatre-vingt-onze", 92: "quatre-vingt-douze", 93: "quatre-vingt-treize", 94: "quatre-vingt-quatorze",
		95: "quatre-vingt-quinze", 96: "quatre-vingt-seize", 97: "quatre-vingt-dix-sept", 98: "quatre-vingt-dix-huit", 99: "quatre-vingt-dix-neuf",
	}

	for v := 70; v <= 99; v++ {
		s, ok := spelled[v]
		if !assert.True(t, ok, "missing %d", v) {
			continue
		}

		want := strconv.Itoa(v)

		// Traditional spelling, spoken words split by spaces, and the 1990 reform's hyphens everywhere ("soixante-et-onze").
		for _, in := range []string{s, strings.ReplaceAll(s, "-", " "), strings.ReplaceAll(s, " ", "-")} {
			assert.Equal(t, want, Normalize("fr", in), in)
		}
	}
}

func TestNormalizeFrenchVigesimal(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldNormalizeQuatreVingt", have: "quatre-vingt", expected: "80"},
		{name: "ShouldNormalizeQuatreVingtsDix", have: "quatre vingts dix", expected: "90"},
		{name: "ShouldNormalizeLeTourDuMondeEnQuatreVingtsJours", have: "Le Tour du monde en quatre-vingts jours", expected: "tour du monde en 80 jours"},
		{name: "ShouldNormalizeCentQuatreVingts", have: "cent quatre-vingts", expected: "180"},
		{name: "ShouldNormalizeDeuxCentSoixanteDixSept", have: "deux cent soixante-dix-sept", expected: "277"},
		{name: "ShouldNormalizeMilleNeufCentQuatreVingtDixNeuf", have: "mille neuf cent quatre-vingt-dix-neuf", expected: "1999"},
		{name: "ShouldNormalizeDixNeufCentQuatreVingtQuatre", have: "dix-neuf cent quatre-vingt-quatre", expected: "1984"},
		{name: "ShouldNormalizeSoixanteEtUn", have: "soixante et un", expected: "61"},
		{name: "ShouldNormalizeSoixanteDixOnze", have: "soixante-dix onze", expected: "70 11"},
		{name: "ShouldNormalizeSoixanteDixSeptDix", have: "soixante-dix-sept dix", expected: "77 10"},
		{name: "ShouldNormalizeQuatreVingtDixNeufVingt", have: "quatre-vingt-dix-neuf vingt", expected: "99 20"},
		{name: "ShouldNormalizeVingtQuatreVingt", have: "vingt quatre vingt", expected: "24 20"},
		{name: "ShouldNormalizeVingtEtOnze", have: "vingt et onze", expected: "20 et 11"},
		{name: "ShouldNormalizeQuatreVingtEtOnze", have: "quatre-vingt et onze", expected: "80 et 11"},
		{name: "ShouldNormalizeQuaranteDix", have: "quarante dix", expected: "40 10"}, //nolint:misspell // French number words.
		{name: "ShouldNormalizeTrenteDouze", have: "trente douze", expected: "30 12"},
		{name: "ShouldNormalizeCinqVingts", have: "cinq vingts", expected: "5 20"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize("fr", tc.have))
		})
	}
}

func TestStopWord(t *testing.T) {
	testCases := []struct {
		name     string
		lang     string
		have     string
		expected bool
	}{
		{name: "ShouldTreatTheAsStopWord", lang: "en", have: "the", expected: true},
		{name: "ShouldTreatCapitalisedTheAsStopWord", lang: "en", have: "The", expected: true},
		{name: "ShouldTreatOfAsStopWord", lang: "en", have: "of", expected: true},
		{name: "ShouldNotTreatTitleWordAsStopWord", lang: "en", have: "frozen"},
		{name: "ShouldNotTreatPhraseAsStopWord", lang: "en", have: "the movie"},
		{name: "ShouldTreatLeAsStopWordInFrench", lang: "fr", have: "le", expected: true},
		{name: "ShouldTreatDenAsStopWordInGerman", lang: "de", have: "den", expected: true},
		{name: "ShouldTreatNoAsStopWordInJapanese", lang: "ja", have: "の", expected: true},
		{name: "ShouldNotTreatKanjiAsStopWordInJapanese", lang: "ja", have: "映"},
		{name: "ShouldTreatOAsStopWordInBrazilianPortuguese", lang: "pt-BR", have: "o", expected: true},
		{name: "ShouldNotTreatEmptyAsStopWord", lang: "en"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, StopWord(tc.lang, tc.have))
		})
	}
}

func TestArticle(t *testing.T) {
	testCases := []struct {
		name     string
		lang     string
		have     string
		expected bool
	}{
		{name: "ShouldTreatTheAsArticle", lang: "en", have: "The", expected: true},
		{name: "ShouldTreatLaAsArticleInFrench", lang: "fr-FR", have: "la", expected: true},
		{name: "ShouldNotTreatStopWordThatIsNoArticleAsArticle", lang: "en", have: "of"},
		{name: "ShouldNotTreatNoAsArticleInJapanese", lang: "ja", have: "の"},
		{name: "ShouldNotTreatPhraseAsArticle", lang: "en", have: "the end"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Article(tc.lang, tc.have))
		})
	}
}

// TestNormalizeOtherScripts checks that text in a script another language reads goes through that language's rules whatever the library's language, and that the library's own script keeps its own.
func TestNormalizeOtherScripts(t *testing.T) {
	testCases := []struct {
		name     string
		lang     string
		have     string
		expected string
	}{
		// Kana, Devanagari and Arabic in an English or German library.
		{name: "ShouldReadKuinInEnglishLibrary", lang: "en", have: "クイーン", expected: "kuin"},
		{name: "ShouldReadTaitanikkuInEnglishLibrary", lang: "en", have: "タイタニック", expected: "taitanikku"},
		{name: "ShouldReadDangalInGermanLibrary", lang: "de", have: "दंगल", expected: "dangal"},
		{name: "ShouldReadTaytankInEnglishLibrary", lang: "en", have: "تايتانك", expected: "taytank"},
		// Each script's own segmentation, fused articles and folds.
		{name: "ShouldReadToisutori3InEnglishLibrary", lang: "en", have: "トイストーリー3", expected: "toisutori 3"},
		{name: "ShouldStripFusedArabicArticleInFrenchLibrary", lang: "fr", have: "الملك", expected: "mlk"},
		{name: "ShouldReadArabicAsArabicLibraryDoesInEnglishLibrary", lang: "en", have: "وجدة", expected: Normalize("ar", "وجدة")},
		// Number words and parts of the script's language, and a run of digits staying with the run before it.
		{name: "ShouldReadDhoom2InEnglishLibrary", lang: "en", have: "धूम दो", expected: "dhoom 2"},
		{name: "ShouldReadPartWordInHindiInEnglishLibrary", lang: "en", have: "भाग 2", expected: "2"},
		{name: "ShouldReadTuySturi3InEnglishLibrary", lang: "en", have: "توي ستوري ٣", expected: "tuy sturi 3"},
		// A leading article of the script's language.
		{name: "ShouldStripLeadingArabicArticleInEnglishLibrary", lang: "en", have: "ذا ماتريكس", expected: "matriks"},
		// Latin text next to it keeps the library's rules.
		{name: "ShouldReadLatinAndKanaInEnglishLibrary", lang: "en", have: "The Matrix クイーン", expected: "matrix kuin"},
		{name: "ShouldReadLatinAndDevanagariInGermanLibrary", lang: "de", have: "Der Pate दंगल", expected: "pate dangal"},
		{name: "ShouldSplitLatinFromDevanagariInOneWordInEnglishLibrary", lang: "en", have: "abcदंगल", expected: "abc dangal"},
		{name: "ShouldReadHalfWidthKatakanaInEnglishLibrary", lang: "en", have: "ｶﾞﾝﾀﾞﾑ", expected: "gandamu"},
		{name: "ShouldReadDangal2016InEnglishLibrary", lang: "en", have: "दंगल २०१६", expected: "dangal 2016"},
		{name: "ShouldReadFilm2021InEnglishLibrary", lang: "en", have: "فيلم ٢٠٢١", expected: "film 2021"},
		// ヴ is the v of the Latin title another library holds, and the b line Japanese says it as in a Japanese library.
		{name: "ShouldReadVuAsVInEnglishLibrary", lang: "en", have: "マーヴェリック", expected: "maverikku"},
		{name: "ShouldReadFinalVuAsVInEnglishLibrary", lang: "en", have: "ドライヴ", expected: "doraivu"},
		{name: "ShouldReadVuAsBInJapaneseLibrary", lang: "ja", have: "マーヴェリック", expected: "maberikku"},
		{name: "ShouldReadFinalVuAsBInJapaneseLibrary", lang: "ja", have: "ドライヴ", expected: "doraibu"},
		// A library in Japanese, Hindi or Arabic reads its own script as it always has.
		{name: "ShouldReadToisutori3InJapaneseLibrary", lang: "ja", have: "トイストーリー3", expected: "toisutori 3"},
		{name: "ShouldReadDhoom2InHindiLibrary", lang: "hi", have: "धूम दो", expected: "dhoom 2"},
		{name: "ShouldStripLeadingArabicArticleInArabicLibrary", lang: "ar", have: "ذا ماتريكس", expected: "matriks"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize(tc.lang, tc.have))
		})
	}
}

// TestTokenizeScripts checks what tokenize keeps of each script's letters and marks, ahead of the rules scriptWords applies to them.
func TestTokenizeScripts(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		// Devanagari keeps its vowel signs, nasalisation, virama and nukta, which are combining marks, and splits nothing at the spacing ones.
		{name: "ShouldKeepDevanagariVowelSigns", have: "दिल चाहता है", expected: "दिल चाहता है"},
		{name: "ShouldKeepDevanagariNasalisation", have: "हँसी तो फँसी", expected: "हँसी तो फँसी"},
		{name: "ShouldKeepDevanagariVirama", have: "क्या कहना", expected: "क्या कहना"},
		{name: "ShouldKeepDevanagariNukta", have: "ज़िंदगी", expected: "ज़िंदगी"},
		{name: "ShouldKeepDevanagariNuktaWhenDecomposed", have: "ज़िंदगी", expected: "ज़िंदगी"},
		// Kana keep their voicing: が stays が (U+304C) rather than か, and halfwidth katakana compose to the fullwidth letters.
		{name: "ShouldKeepKanaVoicing", have: "が か ぱ ば は", expected: "が か ぱ ば は"},
		{name: "ShouldKeepKatakana", have: "ガンダム", expected: "ガンダム"},
		{name: "ShouldComposeHalfWidthKatakana", have: "ｶﾞﾝﾀﾞﾑ", expected: "ガンダム"},
		{name: "ShouldKeepKanjiAndKana", have: "千と千尋の神隠し", expected: "千と千尋の神隠し"},
		{name: "ShouldLowerCaseLatinAfterKatakana", have: "ドラゴンボールZ", expected: "ドラゴンボールz"},
		// Arabic harakat, hamza and tatweel and Hebrew points are dropped.
		{name: "ShouldDropArabicHarakat", have: "مُحَمَّد", expected: "محمد"},
		{name: "ShouldFoldArabicHamzaSeats", have: "أحمد إسلام آمن", expected: "احمد اسلام امن"},
		{name: "ShouldDropArabicTatweel", have: "محـــمد", expected: "محمد"},
		{name: "ShouldDropHebrewPoints", have: "שָׁלוֹם", expected: "שלום"},
		// Every script's decimal digits are ASCII digits.
		{name: "ShouldMapDevanagariDigits", have: "दंगल २०१६", expected: "दंगल 2016"},
		{name: "ShouldMapArabicIndicDigits", have: "فيلم ٢٠٢١", expected: "فيلم 2021"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, strings.Join(english().tokenize(tc.have), " "))
		})
	}
}

func TestSameTitle(t *testing.T) {
	testCases := []struct {
		name    string
		heard   string
		title   string
		year    int
		same    bool
		literal bool
	}{
		{name: "ShouldMatchTitleWithArticleLiterally", heard: "the odyssey", title: "The Odyssey", year: 2026, same: true, literal: true},
		{name: "ShouldMatchTitleWithoutArticle", heard: "the odyssey", title: "Odyssey", year: 2005, same: true},
		{name: "ShouldMatchTitleWhenArticleIsNotHeard", heard: "odyssey", title: "The Odyssey", year: 2026, same: true},
		{name: "ShouldMatchTitleWithYearLiterally", heard: "the odyssey 2026", title: "The Odyssey", year: 2026, same: true, literal: true},
		{name: "ShouldMatchTitleWithFromYearLiterally", heard: "the odyssey from 2026", title: "The Odyssey", year: 2026, same: true, literal: true},
		{name: "ShouldNotMatchTitleOfAnotherYear", heard: "the odyssey from 2026", title: "Odyssey", year: 2005},
		{name: "ShouldMatchTitleWithFromYearWithoutArticle", heard: "odyssey from 2005", title: "Odyssey", year: 2005, same: true},
		{name: "ShouldNotMatchYearWhenTitleHasNone", heard: "the odyssey from 2026", title: "The Odyssey"},
		{name: "ShouldNotMatchNothingHeard", title: "The Odyssey", year: 2026},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			same, literal := SameTitle("en", tc.heard, tc.title, tc.year)
			assert.Equal(t, tc.same, same)
			assert.Equal(t, tc.literal, literal)
		})
	}
}

// TestSameTitleYearJoins checks each language's word between a title and its year, as its question says it, when Amazon leaves the year in the title ("ik bedoel dune uit 2021" heard as the answer "dune uit 2021").
func TestSameTitleYearJoins(t *testing.T) {
	testCases := []struct {
		name  string
		lang  string
		heard string
	}{
		{name: "ShouldJoinYearWithVonInGerman", lang: "de", heard: "dune von 2021"},
		{name: "ShouldJoinYearWithAusInGerman", lang: "de", heard: "dune aus 2021"},
		{name: "ShouldJoinYearWithDeInFrench", lang: "fr", heard: "dune de 2021"},
		{name: "ShouldJoinYearWithDeInSpanish", lang: "es", heard: "dune de 2021"},
		{name: "ShouldJoinYearWithDelInItalian", lang: "it", heard: "dune del 2021"},
		{name: "ShouldJoinYearWithUitInDutch", lang: "nl", heard: "dune uit 2021"},
		{name: "ShouldJoinYearWithDeInPortuguese", lang: "pt", heard: "dune de 2021"},
		{name: "ShouldJoinYearWithLiAamInArabic", lang: "ar", heard: "دون لعام 2021"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			title := "Dune"
			if tc.lang == "ar" {
				title = "دون"
			}

			same, _ := SameTitle(tc.lang, tc.heard, title, 2021)
			assert.True(t, same)
		})
	}

	same, _ := SameTitle("nl", "dune uit 2021", "Dune", 1984)
	assert.False(t, same, "another year is another title")
}
