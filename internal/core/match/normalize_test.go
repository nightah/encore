package match

import (
	"testing"

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
		{"es", "La Casa de Papel parte dos", "casa de papel 2"},
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
