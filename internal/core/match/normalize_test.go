package match

import (
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		lang, in, want string
	}{
		// Case, punctuation, diacritics and articles.
		{"en", "The Matrix", "matrix"},
		{"en", "  THE   matrix!! ", "matrix"},
		{"en", "Amélie", "amelie"},
		{"en", "Sigur Rós", "sigur ros"},
		{"en", "Mötley Crüe", "motley crue"},
		{"en", "Ocean's Eleven", "oceans 11"},
		{"en", "Fast & Furious", "fast and furious"},
		{"en", "Spider-Man: Into the Spider-Verse", "spider man into the spider verse"},
		{"en", "A Quiet Place", "quiet place"},
		{"en", "An American Werewolf in London", "american werewolf in london"},
		{"en", "The The", "the"},
		{"en", "The", "the"},
		{"en", "Æon Flux", "aeon flux"},
		{"en", "", ""},
		{"en", "!!!", ""},
		// Letter runs are joined.
		{"en", "E.T. the Extra-Terrestrial", "et the extra terrestrial"},
		{"en", "M*A*S*H", "mash"},
		{"en", "AC/DC", "ac dc"},
		{"en", "a c d c", "acdc"},
		// Roman numerals only after another word, and single letters survive.
		{"en", "Rocky II", "rocky 2"},
		{"en", "Star Wars: Episode IV - A New Hope", "star wars episode 4 a new hope"},
		{"en", "Final Fantasy XX", "final fantasy 20"},
		{"en", "Rocky XXI", "rocky xxi"},
		{"en", "I, Robot", "i robot"},
		{"en", "Malcolm X", "malcolm x"},
		{"en", "Rocky V", "rocky v"},
		{"en", "II Fast", "ii fast"},
		{"en", "Mix", "mix"},
		// Number words, ordinals and parts.
		{"en", "Rocky Two", "rocky 2"},
		{"en", "rocky second", "rocky 2"},
		{"en", "toy story three", "toy story 3"},
		{"en", "Twenty One Pilots", "21 pilots"},
		{"en", "twenty-first century", "21 century"},
		{"en", "one hundred and one dalmatians", "101 dalmatians"},
		{"en", "two thousand and one a space odyssey", "2001 a space odyssey"},
		{"en", "nineteen eighty four", "1984"},
		{"en", "blade runner twenty forty nine", "blade runner 2049"},
		{"en", "one two three", "1 2 3"},
		{"en", "zero dark thirty", "0 dark 30"},
		{"en", "fifty first dates", "51 dates"},
		{"en", "Dune: Part Two", "dune 2"},
		{"en", "Kill Bill: Vol. 1", "kill bill 1"},
		{"en", "part part two", "2"},
		{"en", "Harry Potter and the Deathly Hallows Part 1", "harry potter and the deathly hallows 1"},
		{"en", "Chapter", "chapter"},
		{"en", "hundred and", "100 and"},
		{"en", "twenty eleven", "2011"},
		{"en", "one twenty", "1 20"},
		{"en", "two hundred hundred", "200 100"},
		{"en", "one thousand thousand", "1000 1000"},
		{"en", "two thousand five thousand", "2005 1000"},
		{"en", "two thousand one hundred thousand", "2100 1000"},
		{"en", "nineteen twentieth", "19 20"},
		{"en", "twenty and twelve", "20 and 12"},
		{"en", "twenty and one", "21"},
		// German: umlauts, ß, articles, compounds and ordinals.
		{"de", "Die Brücke", "brucke"},
		{"de", "Der Schuh des Manitu", "schuh des manitu"},
		{"de", "Straße", "strasse"},
		{"de", "Bibi & Tina", "bibi und tina"},
		{"de", "Ein Fisch namens Wanda", "fisch namens wanda"},
		{"de", "Die Fantastischen Vier", "fantastischen 4"},
		{"de", "einundzwanzig", "21"},
		{"de", "Der zweite Teil", "2 teil"},
		{"de", "Fack ju Göhte Teil drei", "fack ju gohte 3"},
		{"de", "hundert und eins", "101"},
		// German numbers written as one word, and units said before "und" and the tens.
		{"de", "neunzehnhundertvierundachtzig", "1984"},
		{"de", "Emil und die Detektive neunzehnhunderteinunddreißig", "emil und die detektive 1931"},
		{"de", "zweitausendeins", "2001"},
		{"de", "Apache zweihundertsieben", "apache 207"},
		{"de", "einhundertzwanzig", "120"},
		{"de", "tausendundeine Nacht", "1001 nacht"},
		{"de", "tausend und eine Nacht", "1001 nacht"},
		{"de", "neun und neunzig Luftballons", "99 luftballons"},
		{"de", "hundert vier und zwanzig", "124"},
		{"de", "eins und eins", "1 und 1"},
		{"de", "zehn und zwanzig", "10 und 20"},
		{"de", "Du hast mich tausendmal belogen", "du hast mich tausendmal belogen"},
		{"de", "Elfenbein", "elfenbein"},
		{"de", "Achtung", "achtung"},
		{"de", "zweiundzwanzig drei", "22 3"},
		// French: elision and articles.
		{"fr", "L'Auberge espagnole", "auberge espagnole"},
		{"fr", "Le Fabuleux Destin d'Amélie Poulain", "fabuleux destin d amelie poulain"},
		{"fr", "Les Quatre Cents Coups", "400 coups"},
		{"fr", "Un homme et une femme", "homme et une femme"},
		{"fr", "vingt et un", "21"},
		{"fr", "dix sept", "17"},
		{"fr", "Astérix & Obélix", "asterix et obelix"},
		// Spanish and Italian.
		{"es", "El Laberinto del Fauno", "laberinto del fauno"},
		{"es", "Veintiuno", "21"},
		{"es", "treinta y uno", "31"},
		{"es", "uno veintiuno", "1 21"},
		{"es", "veinte veintiuno", "2021"},
		{"es", "La Casa de Papel parte dos", "casa de papel 2"},
		// Spanish hundreds from doscientos to novecientos, in either gender.
		{"es", "argentina mil novecientos ochenta y cinco", "argentina 1985"},
		{"es", "diecinueve días y quinientas noches", "19 dias y 500 noches"},
		{"es", "doscientos", "200"},
		{"es", "setecientas", "700"},
		{"es", "novecientos noventa y nueve", "999"},
		{"es", "dos mil veintitrés", "2023"},
		{"es", "ciento veinte", "120"},
		{"it", "La Vita è Bella", "vita e bella"},
		{"it", "L'ultimo bacio", "ultimo bacio"},
		{"it", "Gli Intoccabili", "intoccabili"},
		{"it", "ventuno", "21"},
		{"it", "il secondo tragico", "2 tragico"},
		// Italian numbers written as one word, with the hundreds, thousands and millions they are built from.
		{"it", "millenovecentonovantadue", "1992"},
		{"it", "mille novecento novantadue", "1992"},
		{"it", "vacanze di natale millenovecentonovantacinque", "vacanze di natale 1995"},
		{"it", "Novecento", "900"},
		{"it", "nove cento", "900"},
		{"it", "centoventisei", "126"},
		{"it", "franco centoventisei", "franco 126"},
		{"it", "ottocento ottantatré", "883"},
		{"it", "centottanta", "180"},
		{"it", "millenovecentottantotto", "1988"},
		{"it", "centouno", "101"},
		{"it", "centuno", "101"},
		{"it", "duemila", "2000"},
		{"it", "duemilaventi", "2020"},
		{"it", "tremila", "3000"},
		{"it", "due milioni cinquecentomila", "2500000"},
		{"it", "un milione di dollari", "1000000 di dollari"},
		{"it", "mille e una notte", "1001 notte"},
		{"it", "zerozerozero", "0 0 0"},
		{"it", "ottoottotre", "8 8 3"},
		{"it", "centottanta gradi", "180 gradi"},
		{"it", "tre duecento", "3 200"},
		{"it", "mille milioni", "1000 1000000"},
		// Words that only start with number words stay words.
		{"it", "treno", "treno"},
		{"it", "settembre", "settembre"},
		{"it", "centro", "centro"},
		{"it", "duetto", "duetto"},
		{"it", "unotre", "unotre"},
		{"it", "treedue", "treedue"},
		// Ordinals written with digits read as the number, as spelled-out ordinals do.
		{"en", "The 6th Sense", "6 sense"},
		{"en", "the sixth sense", "6 sense"},
		{"en", "21st Century Breakdown", "21 century breakdown"},
		{"en", "3rd Rock from the Sun", "3 rock from the sun"},
		{"en", "2nd", "2"},
		{"en", "5e", "5e"},
		{"en", "4x4", "4x4"},
		{"fr", "Le 5ème Élément", "5 element"},
		{"fr", "le 5e élément", "5 element"},
		{"fr", "Le Cinquième Élément", "5 element"},
		{"fr", "1er", "1"},
		{"fr", "1re", "1"},
		{"es", "la 2ª temporada", "2 temporada"},
		{"es", "3er", "3"},
		{"it", "il 2º tempo", "2 tempo"},
		{"de", "5te", "5te"},
		// ½ reads as each language says "and a half", so the title meets the spoken form.
		{"it", "8½", "8 e mezzo"},
		{"it", "otto e mezzo", "8 e mezzo"},
		{"en", "8½ Women", "8 and a half women"},
		{"en", "eight and a half women", "8 and a half women"},
		{"de", "Die 8½ Frauen", "8 einhalb frauen"},
		{"fr", "Huit et demi", "8 et demi"},
		{"fr", "8½", "8 et demi"},
		{"es", "8½", "8 y medio"},
		{"es", "ocho y medio", "8 y medio"},
		// Devanagari keeps its vowel signs, nasalisation, virama and nukta, which are combining marks, and splits nothing at the spacing ones.
		{"en", "दिल चाहता है", "दिल चाहता है"},
		{"en", "हँसी तो फँसी", "हँसी तो फँसी"},
		{"en", "क्या कहना", "क्या कहना"},
		{"en", "ज़िंदगी", "ज़िंदगी"},
		{"en", "ज़िंदगी", "ज़िंदगी"},
		// Kana keep their voicing: が stays が (U+304C) rather than か, and halfwidth katakana compose to the fullwidth letters.
		{"en", "が か ぱ ば は", "が か ぱ ば は"},
		{"en", "ガンダム", "ガンダム"},
		{"en", "ｶﾞﾝﾀﾞﾑ", "ガンダム"},
		{"en", "千と千尋の神隠し", "千と千尋の神隠し"},
		{"en", "ドラゴンボールZ", "ドラゴンボールz"},
		// Arabic harakat, hamza and tatweel and Hebrew points are dropped.
		{"en", "مُحَمَّد", "محمد"},
		{"en", "أحمد إسلام آمن", "احمد اسلام امن"},
		{"en", "محـــمد", "محمد"},
		{"en", "שָׁלוֹם", "שלום"},
		// Every script's decimal digits are ASCII digits, and read as numbers.
		{"en", "दंगल २०१६", "दंगल 2016"},
		{"en", "فيلم ٢٠٢١", "فيلم 2021"},
		{"en", "۱۹۸۴", "1984"},
		{"en", "１９８４", "1984"},
		{"en", "rocky ٢", "rocky 2"},
		{"en", "Rocky ２nd", "rocky 2"},
		// Locale codes and unknown languages.
		{"de-DE", "Die Ärzte", "arzte"},
		{"en_GB", "The Crown", "crown"},
		{"xx", "The Crown", "crown"},
	}

	for _, tt := range tests {
		t.Run(tt.lang+"/"+tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, Normalize(tt.lang, tt.in))
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

	tests := map[rune]rune{'7': '7', '०': '0', '९': '9', '٣': '3', '۷': '7', '৫': '5', '๙': '9', '𝟠': '8'}

	for in, want := range tests {
		assert.Equal(t, want, asciiDigit(in), "%U", in)
	}
}

func TestLanguageHooks(t *testing.T) {
	tests := []struct {
		name string
		edit func(l *language)
		in   string
		want string
	}{
		{"none", func(*language) {}, "Xy Zw", "xy zw"},
		{"folds", func(l *language) { l.folds = map[rune]string{'x': "ks", 'カ': "か"} }, "Xylo", "ksylo"},
		{"folds before letterFolds", func(l *language) { l.folds = map[rune]string{'ß': "s"} }, "Straße", "strase"},
		{"folds keep voicing", func(l *language) { l.folds = map[rune]string{'カ': "か"} }, "ガカ", "がか"},
		{"split", func(l *language) { l.split = splitEvery(2) }, "abcdef gh", "ab cd ef gh"},
		{"split before letter runs", func(l *language) { l.split = splitEvery(1) }, "abc", "abc"},
		{"prefixes", func(l *language) { l.articlePrefixes = []string{"al", "wal"} }, "alfoo walbar baz", "foo bar baz"},
		{"longest prefix", func(l *language) { l.articlePrefixes = []string{"w", "wal"} }, "walbar", "bar"},
		{"prefix minimum", func(l *language) { l.articlePrefixes = []string{"al"}; l.articlePrefixMin = 3 }, "alto alfoo", "alto foo"},
		{"prefix never empties", func(l *language) { l.articlePrefixes = []string{"al"} }, "al", "al"},
		{"prefix before articles", func(l *language) { l.articlePrefixes = []string{"x"} }, "xthe matrix", "matrix"},
		{"prefix runes", func(l *language) { l.articlePrefixes = []string{"ال"}; l.articlePrefixMin = 2 }, "الملك الم", "ملك الم"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := english()
			tt.edit(l)

			assert.Equal(t, tt.want, strings.Join(l.normalize(tt.in, false), " "))
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

	assert.Equal(t, []string{"rocky", "5"}, l.normalize("Rocky V", true))
	assert.Equal(t, []string{"malcolm", "10"}, l.normalize("Malcolm X", true))
	assert.Equal(t, []string{"i", "robot"}, l.normalize("I, Robot", true))
}

func TestShortenings(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"Dune: Part Two", []string{"Dune", "Part Two"}},
		{"Star Wars: Episode IV - A New Hope", []string{"Star Wars", "Episode IV - A New Hope", "Star Wars: Episode IV", "A New Hope"}},
		{"The Office (US)", []string{"The Office"}},
		{"The Office (US", []string{"The Office"}},
		{"Seinfeld", nil},
	}

	for _, tt := range tests {
		assert.ElementsMatch(t, tt.want, shortenings(tt.in), tt.in)
	}
}

func TestUmlautVariant(t *testing.T) {
	assert.Equal(t, "Die Bruecke", umlautVariant("Die Brücke"))
	assert.Equal(t, "Oel und Aepfel", umlautVariant("Öl und Äpfel"))
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
	tests := []struct {
		in, want string
	}{
		{"quatre-vingt", "80"},
		{"quatre vingts dix", "90"},
		{"Le Tour du monde en quatre-vingts jours", "tour du monde en 80 jours"},
		{"cent quatre-vingts", "180"},
		{"deux cent soixante-dix-sept", "277"},
		{"mille neuf cent quatre-vingt-dix-neuf", "1999"},
		{"dix-neuf cent quatre-vingt-quatre", "1984"},
		{"soixante et un", "61"},
		{"soixante-dix onze", "70 11"},
		{"soixante-dix-sept dix", "77 10"},
		{"quatre-vingt-dix-neuf vingt", "99 20"},
		{"vingt quatre vingt", "24 20"},
		{"vingt et onze", "20 et 11"},
		{"quatre-vingt et onze", "80 et 11"},
		{"quarante dix", "40 10"}, //nolint:misspell // French number words.
		{"trente douze", "30 12"},
		{"cinq vingts", "5 20"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, Normalize("fr", tt.in), tt.in)
	}
}
