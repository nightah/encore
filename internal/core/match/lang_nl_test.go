package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDutch(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		// Articles, including the elided "'t" and "'s" and the English "the" Dutch recognisers write.
		{name: "ShouldNormalizeDeLuizenmoeder", have: "De Luizenmoeder", expected: "luizenmoeder"},
		{name: "ShouldNormalizeHetSchaepMetDe5Pooten", have: "Het Schaep met de 5 Pooten", expected: "schaep met de 5 pooten"},
		{name: "ShouldNormalizeElidedHetSchaepMetDe5Pooten", have: "'t Schaep met de 5 pooten", expected: "schaep met de 5 pooten"},
		{name: "ShouldNormalizeEenVluchtRegenwulpen", have: "Een vlucht regenwulpen", expected: "vlucht regenwulpen"},
		{name: "ShouldNormalizeSHertogenbosch", have: "'s-Hertogenbosch", expected: "hertogenbosch"},
		{name: "ShouldNormalizeTheGodfather", have: "The Godfather", expected: "godfather"},
		{name: "ShouldKeepLoneArticle", have: "de", expected: "de"},
		// Apostrophes join a possessive or plural to its word; "'ns" stays a word.
		{name: "ShouldNormalizeOceansEleven", have: "Ocean's Eleven", expected: "oceans 11"},
		{name: "ShouldNormalizeOpas", have: "opa's", expected: "opas"},
		{name: "ShouldNormalizeZegNsAaa", have: "Zeg 'ns Aaa", expected: "zeg ns aaa"},
		// Diacritics, the ij ligature and the diaeresis.
		{name: "ShouldNormalizeBlof", have: "Bløf", expected: "blof"},
		{name: "ShouldNormalizeAndreHazes", have: "André Hazes", expected: "andre hazes"},
		{name: "ShouldNormalizeIjsselmeer", have: "ĳsselmeer", expected: "ijsselmeer"},
		{name: "ShouldNormalizeSuzanAndFreek", have: "Suzan & Freek", expected: "suzan en freek"},
		// Cardinals, units before tens, fused into one word.
		{name: "ShouldNormalizeFlodderDrie", have: "flodder drie", expected: "flodder 3"},
		{name: "ShouldNormalizeTwaalf", have: "twaalf", expected: "12"},
		{name: "ShouldNormalizeNegentien", have: "negentien", expected: "19"},
		{name: "ShouldNormalizeEenentwintigJumpStreet", have: "eenentwintig jump street", expected: "21 jump street"},
		{name: "ShouldNormalizeTweeentwintig", have: "tweeëntwintig", expected: "22"},
		{name: "ShouldNormalizeDrieendertig", have: "drieëndertig", expected: "33"},
		{name: "ShouldNormalizeVierentachtig", have: "vierentachtig", expected: "84"},
		{name: "ShouldNormalizeNegenennegentig", have: "negenennegentig", expected: "99"},
		{name: "ShouldNormalizeDrieEnTwintig", have: "drie en twintig", expected: "23"},
		{name: "ShouldNormalizeHonderd", have: "honderd", expected: "100"},
		{name: "ShouldNormalizeHonderdeen", have: "honderdeen", expected: "101"},
		{name: "ShouldNormalizeHonderdEnEen", have: "honderd en een", expected: "101"},
		{name: "ShouldNormalizeZevenhonderd", have: "zevenhonderd", expected: "700"},
		{name: "ShouldNormalizeEenhonderd", have: "eenhonderd", expected: "100"},
		{name: "ShouldNormalizeDuizend", have: "duizend", expected: "1000"},
		{name: "ShouldNormalizeDuizendeneen", have: "duizendeneen", expected: "1001"},
		{name: "ShouldNormalizeTweeduizend", have: "tweeduizend", expected: "2000"},
		{name: "ShouldNormalizeTweeduizendWithSoftHyphen", have: "twee\u00adduizend", expected: "2000"},
		{name: "ShouldNormalizeTweeduizendeen", have: "tweeduizendeen", expected: "2001"},
		{name: "ShouldNormalizeDrieduizendVijfhonderd", have: "drieduizend vijfhonderd", expected: "3500"},
		// "miljoen" stays a word, as "million" does in English: a recogniser writes the count before it as digits as often as words ("15 miljoen mensen").
		{name: "ShouldNormalizeVijftienMiljoenMensen", have: "vijftien miljoen mensen", expected: "15 miljoen mensen"},
		{name: "ShouldNormalize15MiljoenMensenWithDigits", have: "15 Miljoen Mensen", expected: "15 miljoen mensen"},
		{name: "ShouldNormalizeNegentienhonderdvierentachtig", have: "negentienhonderdvierentachtig", expected: "1984"},
		{name: "ShouldNormalizeNegentienVierentachtig", have: "negentien vierentachtig", expected: "1984"},
		{name: "ShouldNormalizeNulNulZeven", have: "nul nul zeven", expected: "0 0 7"},
		// "een" is a number only inside one.
		{name: "ShouldNormalizeEenVrouw", have: "een vrouw", expected: "vrouw"},
		{name: "ShouldNormalizeAllesIsEen", have: "alles is een", expected: "alles is een"},
		{name: "ShouldNormalizeSuzanEnFreekLowerCase", have: "suzan en freek", expected: "suzan en freek"},
		// Words that start like numbers stay words.
		{name: "ShouldNormalizeDeTweeling", have: "De tweeling", expected: "tweeling"},
		{name: "ShouldNormalizeTienen", have: "tienen", expected: "tienen"},
		{name: "ShouldNormalizeAchten", have: "achten", expected: "achten"},
		{name: "ShouldNormalizeVierkant", have: "vierkant", expected: "vierkant"},
		{name: "ShouldNormalizeElfen", have: "elfen", expected: "elfen"},
		// English cardinals, for English titles in a Dutch library.
		{name: "ShouldNormalizeOceansTwelve", have: "Ocean's Twelve", expected: "oceans 12"},
		{name: "ShouldNormalizeOceansTwaalf", have: "oceans twaalf", expected: "oceans 12"},
		{name: "ShouldNormalizeApolloThirteen", have: "Apollo Thirteen", expected: "apollo 13"},
		{name: "ShouldNormalizeTwentyOnePilots", have: "twenty one pilots", expected: "21 pilots"},
		// Ordinals, spelled out and with digits.
		{name: "ShouldNormalizeDeEerste", have: "de eerste", expected: "1"},
		{name: "ShouldNormalizeDeVierdeMan", have: "de vierde man", expected: "4 man"},
		{name: "ShouldNormalizeHetZesdeZintuig", have: "het zesde zintuig", expected: "6 zintuig"},
		{name: "ShouldNormalizeAchtste", have: "achtste", expected: "8"},
		{name: "ShouldNormalizeTwaalfde", have: "twaalfde", expected: "12"},
		{name: "ShouldNormalizeTwintigste", have: "twintigste", expected: "20"},
		{name: "ShouldNormalizeEenentwintigsteEeuw", have: "eenentwintigste eeuw", expected: "21 eeuw"},
		{name: "ShouldNormalizeHonderdste", have: "honderdste", expected: "100"},
		{name: "ShouldNormalizeDe3deMan", have: "de 3de man", expected: "3 man"},
		{name: "ShouldNormalizeHet2eSeizoen", have: "het 2e seizoen", expected: "2 seizoen"},
		{name: "ShouldNormalize21steEeuw", have: "21ste eeuw", expected: "21 eeuw"},
		{name: "ShouldNormalize8ste", have: "8ste", expected: "8"},
		{name: "ShouldNormalizeThe6thDay", have: "The 6th Day", expected: "6 day"},
		{name: "ShouldNormalizeTheSixthDay", have: "the sixth day", expected: "6 day"},
		{name: "ShouldNormalize21stCentury", have: "21st century", expected: "21 century"},
		{name: "ShouldNormalizeSecondHand", have: "second hand", expected: "2 hand"},
		// Parts before a number.
		{name: "ShouldNormalizeDeGodfatherDeelTwee", have: "de godfather deel twee", expected: "godfather 2"},
		{name: "ShouldNormalizeTheGodfatherPartII", have: "The Godfather Part II", expected: "godfather 2"},
		{name: "ShouldNormalizeKillBillVolume1", have: "Kill Bill Volume 1", expected: "kill bill 1"},
		{name: "ShouldNormalizeHoofdstukDrie", have: "hoofdstuk drie", expected: "3"},
		{name: "ShouldNormalizeFlikkenSeizoen2", have: "flikken seizoen 2", expected: "flikken 2"},
		{name: "ShouldNormalizeDeel", have: "deel", expected: "deel"},
		// ½ reads "en een half".
		{name: "ShouldNormalize8Half", have: "8½", expected: "8 en een half"},
		{name: "ShouldNormalizeAchtEnEenHalf", have: "acht en een half", expected: "8 en een half"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize("nl", tc.have))
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
	testCases := []struct {
		name     string
		have     string
		expected int
	}{
		{name: "ShouldPairFirstUnitWithFirstTens", have: "eenentwintig", expected: 21},
		{name: "ShouldPairLastUnitWithFirstTens", have: "negenentwintig", expected: 29},
		{name: "ShouldPairFirstUnitWithLastTens", have: "eenennegentig", expected: 91},
		{name: "ShouldPairLastUnitWithLastTens", have: "negenennegentig", expected: 99},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w, ok := l.nums[tc.have]
			require.True(t, ok)
			assert.Equal(t, tc.expected, w.val)
			assert.Equal(t, numCompound, w.kind)
		})
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

	testCases := []struct {
		name     string
		heard    string
		expected string
	}{
		{name: "ShouldFindElevenSpokenInDutch", heard: "oceans elf", expected: "oceans11"},
		{name: "ShouldFindTwelveSpokenInDutch", heard: "oceans twaalf", expected: "oceans12"},
		{name: "ShouldFindFusedNumberWord", heard: "eenentwintig jump street", expected: "jump"},
		{name: "ShouldFindTitleWithElidedArticle", heard: "t schaep met de vijf pooten", expected: "schaep"},
		{name: "ShouldFindTitleWithArticle", heard: "het schaep met de vijf pooten", expected: "schaep"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ix.Search(t.Context(), Query{Text: tc.heard})
			require.NoError(t, err)

			best, ok := res.Best()
			require.True(t, ok)
			assert.Equal(t, tc.expected, best.Doc.ID)
			assert.Equal(t, DecisionPlay, res.Decision)
		})
	}
}
