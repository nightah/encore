package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDutch(t *testing.T) {
	tests := []struct{ in, want string }{
		// Articles, including the elided "'t" and "'s" and the English "the" Dutch recognisers write.
		{"De Luizenmoeder", "luizenmoeder"},
		{"Het Schaep met de 5 Pooten", "schaep met de 5 pooten"},
		{"'t Schaep met de 5 pooten", "schaep met de 5 pooten"},
		{"Een vlucht regenwulpen", "vlucht regenwulpen"},
		{"'s-Hertogenbosch", "hertogenbosch"},
		{"The Godfather", "godfather"},
		{"de", "de"},
		// Apostrophes join a possessive or plural to its word; "'ns" stays a word.
		{"Ocean's Eleven", "oceans 11"},
		{"opa's", "opas"},
		{"Zeg 'ns Aaa", "zeg ns aaa"},
		// Diacritics, the ij ligature and the diaeresis.
		{"Bløf", "blof"},
		{"André Hazes", "andre hazes"},
		{"ĳsselmeer", "ijsselmeer"},
		{"Suzan & Freek", "suzan en freek"},
		// Cardinals, units before tens, fused into one word.
		{"flodder drie", "flodder 3"},
		{"twaalf", "12"},
		{"negentien", "19"},
		{"eenentwintig jump street", "21 jump street"},
		{"tweeëntwintig", "22"},
		{"drieëndertig", "33"},
		{"vierentachtig", "84"},
		{"negenennegentig", "99"},
		{"drie en twintig", "23"},
		{"honderd", "100"},
		{"honderdeen", "101"},
		{"honderd en een", "101"},
		{"zevenhonderd", "700"},
		{"eenhonderd", "100"},
		{"duizend", "1000"},
		{"duizendeneen", "1001"},
		{"tweeduizend", "2000"},
		{"twee\u00adduizend", "2000"},
		{"tweeduizendeen", "2001"},
		{"drieduizend vijfhonderd", "3500"},
		// "miljoen" stays a word, as "million" does in English: a recogniser writes the count before it as digits as often as words ("15 miljoen mensen").
		{"vijftien miljoen mensen", "15 miljoen mensen"},
		{"15 Miljoen Mensen", "15 miljoen mensen"},
		{"negentienhonderdvierentachtig", "1984"},
		{"negentien vierentachtig", "1984"},
		{"nul nul zeven", "0 0 7"},
		// "een" is a number only inside one.
		{"een vrouw", "vrouw"},
		{"alles is een", "alles is een"},
		{"suzan en freek", "suzan en freek"},
		// Words that start like numbers stay words.
		{"De tweeling", "tweeling"},
		{"tienen", "tienen"},
		{"achten", "achten"},
		{"vierkant", "vierkant"},
		{"elfen", "elfen"},
		// English cardinals, for English titles in a Dutch library.
		{"Ocean's Twelve", "oceans 12"},
		{"oceans twaalf", "oceans 12"},
		{"Apollo Thirteen", "apollo 13"},
		{"twenty one pilots", "21 pilots"},
		// Ordinals, spelled out and with digits.
		{"de eerste", "1"},
		{"de vierde man", "4 man"},
		{"het zesde zintuig", "6 zintuig"},
		{"achtste", "8"},
		{"twaalfde", "12"},
		{"twintigste", "20"},
		{"eenentwintigste eeuw", "21 eeuw"},
		{"honderdste", "100"},
		{"de 3de man", "3 man"},
		{"het 2e seizoen", "2 seizoen"},
		{"21ste eeuw", "21 eeuw"},
		{"8ste", "8"},
		{"The 6th Day", "6 day"},
		{"the sixth day", "6 day"},
		{"21st century", "21 century"},
		{"second hand", "2 hand"},
		// Parts before a number.
		{"de godfather deel twee", "godfather 2"},
		{"The Godfather Part II", "godfather 2"},
		{"Kill Bill Volume 1", "kill bill 1"},
		{"hoofdstuk drie", "3"},
		{"flikken seizoen 2", "flikken 2"},
		{"deel", "deel"},
		// ½ reads "en een half".
		{"8½", "8 en een half"},
		{"acht en een half", "8 en een half"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, Normalize("nl", tt.in))
		})
	}

	assert.Equal(t, "schaep", Normalize("nl-NL", "Het Schaep"), "locale codes reach Dutch")
}

func TestDutchLanguage(t *testing.T) {
	l := languages["nl"]
	require.NotNil(t, l)

	assert.Equal(t, "nl", l.code)
	assert.Same(t, l, languageFor("nl-BE"))
	assert.True(t, l.fused)
	assert.True(t, l.unitsFirst)

	// Every unit pairs with every tens as one word.
	for _, tc := range []struct {
		word string
		want int
	}{{"eenentwintig", 21}, {"negenentwintig", 29}, {"eenennegentig", 91}, {"negenennegentig", 99}} {
		w, ok := l.nums[tc.word]
		require.True(t, ok, tc.word)
		assert.Equal(t, tc.want, w.val, tc.word)
		assert.Equal(t, numCompound, w.kind, tc.word)
	}

	assert.True(t, l.nums["een"].cont, "een doubles as the article")
	assert.True(t, l.nums["eenentwintigste"].ordinal)
	assert.Equal(t, 21, l.nums["eenentwintigste"].val)
}

func TestDutchMatches(t *testing.T) {
	ix, err := New("nl-NL", Config{})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, ix.Close()) })

	require.NoError(t, ix.Upsert(
		Doc{ID: "oceans11", Title: "Ocean's Eleven"},
		Doc{ID: "oceans12", Title: "Ocean's Twelve"},
		Doc{ID: "jump", Title: "21 Jump Street"},
		Doc{ID: "schaep", Title: "Het schaep met de 5 pooten"},
	))

	for heard, want := range map[string]string{
		"oceans elf":                    "oceans11",
		"oceans twaalf":                 "oceans12",
		"eenentwintig jump street":      "jump",
		"t schaep met de vijf pooten":   "schaep",
		"het schaep met de vijf pooten": "schaep",
	} {
		res, err := ix.Search(t.Context(), Query{Text: heard})
		require.NoError(t, err)

		best, ok := res.Best()
		require.True(t, ok, heard)
		assert.Equal(t, want, best.Doc.ID, heard)
		assert.Equal(t, DecisionPlay, res.Decision, heard)
	}
}
