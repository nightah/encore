package de

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
)

// TestCologneVectors pins vectors captured from github.com/f1monkey/phonetic v1.4.2. The first three are the published examples for Kölner Phonetik (Müller-Lüdenscheidt, Wikipedia, Breschnew); the rest pin the reference's edge cases.
func TestCologneVectors(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldEncodeMuellerLuedenscheidt", have: "müller-lüdenscheidt", expected: "65752682"},
		{name: "ShouldEncodeWikipedia", have: "wikipedia", expected: "3412"},
		{name: "ShouldEncodeBreschnew", have: "breschnew", expected: "17863"},
		{name: "ShouldEncodeHeinz", have: "heinz", expected: "068"},
		{name: "ShouldEncodeKuehn", have: "kühn", expected: "46"},
		{name: "ShouldEncodeDreissig", have: "dreissig", expected: "2784"},
		{name: "ShouldEncodeEszett", have: "ßtraße", expected: "8278"},
		{name: "ShouldEncodeLoneC", have: "c", expected: "4"},
		{name: "ShouldEncodeCaesarWithUmlaut", have: "cäsar", expected: "487"},
		{name: "ShouldEncodeXaver", have: "xaver", expected: "4837"},
		{name: "ShouldEncodeAcht", have: "acht", expected: "042"},
		{name: "ShouldEncodeChor", have: "chor", expected: "47"},
		{name: "ShouldEncodeChrist", have: "christ", expected: "4782"},
		{name: "ShouldEncodeSachs", have: "sachs", expected: "848"},
		{name: "ShouldEncodePhilipp", have: "philipp", expected: "3513"},
		{name: "ShouldEncodeHannover", have: "hannover", expected: "0637"},
		{name: "ShouldEncodeLoneP", have: "p", expected: "3"},
		{name: "ShouldEncodeApp", have: "app", expected: "013"},
		{name: "ShouldEncodeTatort", have: "tatort", expected: "2272"},
		{name: "ShouldEncodeRammstein", have: "rammstein", expected: "76826"},
		{name: "ShouldEncodeGroenemeyer", have: "grönemeyer", expected: "47667"},
		{name: "ShouldEncodeZweiohrkueken", have: "zweiohrküken", expected: "837446"},
		{name: "ShouldEncodeNothingForAccentedVowel", have: "é"},
		{name: "ShouldEncodeSkippingDigits", have: "1a2b", expected: "01"},
		{name: "ShouldEncodeEmpty"},
		{name: "ShouldEncodeAachen", have: "aachen", expected: "046"},
		{name: "ShouldEncodeMatsch", have: "matsch", expected: "68"},
		{name: "ShouldEncodeSchwarz", have: "schwarz", expected: "8378"},
		{name: "ShouldEncodeRechtsanwalt", have: "rechtsanwalt", expected: "7486352"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, string(cologne(nil, tc.have)))
		})
	}
}

func TestKeys(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldKeyTatort", have: "tatort", expected: []string{"2272"}},
		{name: "ShouldPassDigitsThrough", have: "2049", expected: []string{"2049"}},
		{name: "ShouldFallBackToWordForAccentedLetter", have: "é", expected: []string{"é"}},
		{name: "ShouldFallBackToEmptyWord", expected: []string{""}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Encoder{}.Keys(nil, tc.have))
		})
	}

	assert.Equal(t, []string{"x", "2272"}, Encoder{}.Keys([]string{"x"}, "tatort"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("de-AT"))
	assert.Equal(t, Encoder{}, phonetic.For("DE_de"))
}

func genWord() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.String(),
		rapid.StringMatching(`[a-zäöüßéè]{0,24}`),
		rapid.StringMatching(`(sch|ch|chs|cks|ph|dt|tz|ck|cz|x|c|qu|ß|[a-z]){1,8}`),
		rapid.StringMatching(`[A-Za-zÄÖÜ0-9 '-]{0,30}`),
	)
}

func TestPropKeysContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		word := genWord().Draw(t, "word")
		keys := Encoder{}.Keys(nil, word)

		require.Len(t, keys, 1)
		require.Equal(t, keys, Encoder{}.Keys(nil, word), "deterministic")

		if k := keys[0]; k != word {
			require.NotEmpty(t, k)

			for j := range len(k) {
				require.True(t, k[j] >= '0' && k[j] <= '8', "key %q", k)
			}
		}

		if phonetic.IsDigits(word) {
			require.Equal(t, []string{word}, keys)
		}
	})
}

func TestKeysAllocations(t *testing.T) {
	dst := make([]string, 0, 3)

	testCases := []struct {
		name     string
		have     string
		expected float64
	}{
		{name: "ShouldAllocateOnceForTatort", have: "tatort", expected: 1},
		{name: "ShouldNotAllocateForDigits", have: "2049"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], tc.have) })
			assert.InDelta(t, tc.expected, got, 0, "only the returned key allocates")
		})
	}
}

// benchWords are title words of the kind the matcher indexes.
var benchWords = []string{
	"das", "boot", "schuh", "manitu", "lenin", "lola", "rennt", "leben", "anderen", "brucke", "untergang", "unendliche",
	"geschichte", "wunder", "bern", "fack", "gohte", "keinohrhasen", "zweiohrkuken", "kommissar", "anfanger", "fruher",
	"stirbt", "westen", "nichts", "neues", "tatort", "sendung", "maus", "biene", "maja", "fragezeichen", "arzte",
	"fantastischen", "rammstein", "herbert", "gronemeyer", "feuerzangenbowle", "systemsprenger", "kokowaah",
}

func BenchmarkKeys(b *testing.B) {
	dst := make([]string, 0, 3)

	b.ReportAllocs()

	for i := range b.N {
		dst = Encoder{}.Keys(dst[:0], benchWords[i%len(benchWords)])
	}
}
