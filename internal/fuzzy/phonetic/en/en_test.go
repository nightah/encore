package en

import (
	"slices"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
)

// TestDoubleMetaphoneVectors pins vectors captured from github.com/PhonoGrams/double_metaphone v0.1.0 EncodeUncapped. Most match Philips's published examples (Smith, Schmidt, Caesar, Chianti, Michael, Thumb, Sugar, Edge); "Jose" codes JS/HS rather than Philips's HS because the library's San/Jose rule tests for a space that letters-only input never has.
func TestDoubleMetaphoneVectors(t *testing.T) {
	testCases := []struct {
		name      string
		have      string
		primary   string
		alternate string
	}{
		{name: "ShouldEncodeThompson", have: "thompson", primary: "TMPSN"},
		{name: "ShouldEncodeSmith", have: "smith", primary: "SM0", alternate: "XMT"},
		{name: "ShouldEncodeSchmidt", have: "schmidt", primary: "XMT", alternate: "SMT"},
		{name: "ShouldEncodeXavier", have: "xavier", primary: "SF", alternate: "SFR"},
		{name: "ShouldEncodeCaesar", have: "caesar", primary: "SSR"},
		{name: "ShouldEncodeChianti", have: "chianti", primary: "KNT"},
		{name: "ShouldEncodeMichael", have: "michael", primary: "MKL", alternate: "MXL"},
		{name: "ShouldEncodeThumb", have: "thumb", primary: "0M", alternate: "TM"},
		{name: "ShouldEncodeJose", have: "jose", primary: "JS", alternate: "HS"},
		{name: "ShouldEncodeSugar", have: "sugar", primary: "XKR", alternate: "SKR"},
		{name: "ShouldEncodeEdge", have: "edge", primary: "AJ"},
		{name: "ShouldEncodeGnocchi", have: "gnocchi", primary: "NX"},
		{name: "ShouldEncodeTagliaro", have: "tagliaro", primary: "TKLR", alternate: "TLR"},
		{name: "ShouldEncodeBellocchio", have: "bellocchio", primary: "PLX"},
		{name: "ShouldEncodePepper", have: "pepper", primary: "PPR"},
		{name: "ShouldEncodePeppa", have: "peppa", primary: "PP"},
		{name: "ShouldEncodeCabrillo", have: "cabrillo", primary: "KPRL"},
		{name: "ShouldEncodeGallegos", have: "gallegos", primary: "KLKS"},
		{name: "ShouldEncodeDumb", have: "dumb", primary: "TM"},
		{name: "ShouldEncodeKnight", have: "knight", primary: "NT"},
		{name: "ShouldEncodeWright", have: "wright", primary: "RT"},
		{name: "ShouldEncodePsychology", have: "psychology", primary: "SXLJ", alternate: "SKLK"},
		{name: "ShouldEncodeWitz", have: "witz", primary: "ATS", alternate: "FFX"},
		{name: "ShouldEncodeFilipowicz", have: "filipowicz", primary: "FLPTS", alternate: "FLPFX"},
		{name: "ShouldEncodeAggie", have: "aggie", primary: "AJ", alternate: "AK"},
		{name: "ShouldEncodeDanger", have: "danger", primary: "TNJR", alternate: "TNKR"},
		{name: "ShouldEncodeGhislane", have: "ghislane", primary: "JLN"},
		{name: "ShouldEncodeLaugh", have: "laugh", primary: "LF"},
		{name: "ShouldEncodeTough", have: "tough", primary: "TF"},
		{name: "ShouldEncodeHugh", have: "hugh", primary: "H"},
		{name: "ShouldEncodeBacher", have: "bacher", primary: "PKR"},
		{name: "ShouldEncodeOrchestra", have: "orchestra", primary: "ARKSTR"},
		{name: "ShouldEncodeMcclellan", have: "mcclellan", primary: "MKLLN"},
		{name: "ShouldEncodeSucceed", have: "succeed", primary: "SKST"},
		{name: "ShouldEncodeBacci", have: "bacci", primary: "PX"},
		{name: "ShouldEncodeAccident", have: "accident", primary: "AKSTNT"},
		{name: "ShouldEncodeCzerny", have: "czerny", primary: "SRN", alternate: "XRN"},
		{name: "ShouldEncodeZhao", have: "zhao", primary: "J"},
		{name: "ShouldEncodeZola", have: "zola", primary: "SL"},
		{name: "ShouldEncodeIsland", have: "island", primary: "ALNT"},
		{name: "ShouldEncodeSchool", have: "school", primary: "SKL"},
		{name: "ShouldEncodeSchenker", have: "schenker", primary: "XNKR", alternate: "SKNKR"},
		{name: "ShouldEncodeSchooner", have: "schooner", primary: "SKNR"},
		{name: "ShouldEncodeSian", have: "sian", primary: "SN", alternate: "XN"},
		{name: "ShouldEncodeSnider", have: "snider", primary: "SNTR", alternate: "XNTR"},
		{name: "ShouldEncodeTichner", have: "tichner", primary: "TXNR", alternate: "TKNR"},
		{name: "ShouldEncodeThomas", have: "thomas", primary: "TMS"},
		{name: "ShouldEncodeKaiser", have: "kaiser", primary: "KSR"},
		{name: "ShouldEncodeHarry", have: "harry", primary: "HR"},
		{name: "ShouldEncodeOcean", have: "ocean", primary: "ASN"},
		{name: "ShouldEncodeRogier", have: "rogier", primary: "RJ", alternate: "RJR"},
		{name: "ShouldEncodeRosheim", have: "rosheim", primary: "RSM"},
		{name: "ShouldEncodeMosholm", have: "mosholm", primary: "MSLM"},
		{name: "ShouldEncodeLoneX", have: "x", primary: "S"},
		{name: "ShouldEncodeEmpty"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p, a := doubleMetaphone(tc.have)
			assert.Equal(t, tc.primary, p)
			assert.Equal(t, tc.alternate, a)
		})
	}
}

func TestKeys(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldKeySmith", have: "smith", expected: []string{"SM0", "XMT"}},
		{name: "ShouldKeyPepper", have: "pepper", expected: []string{"PPR", "PP"}},
		{name: "ShouldKeyPeppa", have: "peppa", expected: []string{"PP"}},
		{name: "ShouldPassDigitsThrough", have: "2049", expected: []string{"2049"}},
		{name: "ShouldSkipLeadingDigits", have: "123abc", expected: []string{"APK"}},
		{name: "ShouldFallBackToWordForAccentedLetter", have: "é", expected: []string{"é"}},
		{name: "ShouldFallBackToEmptyWord", expected: []string{""}},
		{name: "ShouldKeySnider", have: "snider", expected: []string{"SNTR", "XNTR", "SNT"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Encoder{}.Keys(nil, tc.have))
		})
	}

	assert.Equal(t, []string{"x", "SM0", "XMT"}, Encoder{}.Keys([]string{"x"}, "smith"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("en-AU"))
	assert.Equal(t, Encoder{}, phonetic.For("xx"), "unknown languages fall back to English")
}

// rulyAlphabet weights the letters Double Metaphone has context rules for, so long random words exercise them.
const rulyAlphabet = "aceghijlmnorstwxyzbckpsdgu"

func genWord() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.String(),
		rapid.StringMatching(`[a-zçñäöüß]{0,24}`),
		rapid.StringMatching(`[`+rulyAlphabet+`]{1,12}`),
		rapid.StringMatching(`(sch|ch|gh|gn|cc|ll|th|wicz|witz|cia|tion|ough|mb|ps|wr|kn|jose|van|von|san|sugar|caesar|[a-z]){1,6}`),
		rapid.StringMatching(`[A-Za-z0-9 '-]{0,30}`),
	)
}

func TestPropKeysContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		word := genWord().Draw(t, "word")
		keys := Encoder{}.Keys(nil, word)

		require.NotEmpty(t, keys)
		require.LessOrEqual(t, len(keys), 3)
		require.Equal(t, keys, Encoder{}.Keys(nil, word), "deterministic")

		for i, k := range keys {
			require.False(t, slices.Contains(keys[:i], k), "duplicate key %q", k)

			if k == word {
				continue
			}

			require.NotEmpty(t, k)

			for j := range len(k) {
				require.True(t, k[j] < utf8.RuneSelf && (k[j] == '0' || (k[j] >= 'A' && k[j] <= 'Z')), "key %q", k)
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
		{name: "ShouldAllocateOnceForPepper", have: "pepper", expected: 1},
		{name: "ShouldAllocateTwiceForSmith", have: "smith", expected: 2},
		{name: "ShouldNotAllocateForDigits", have: "2049"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], tc.have) })
			assert.InDelta(t, tc.expected, got, 0, "only the returned keys allocate")
		})
	}
}

// benchWords are title words of the kind the matcher indexes.
var benchWords = []string{
	"star", "wars", "empire", "strikes", "back", "lord", "rings", "fellowship", "matrix", "reloaded", "harry", "potter",
	"philosophers", "stone", "shawshank", "redemption", "silence", "lambs", "inception", "interstellar", "seinfeld",
	"mandalorian", "schwarzenegger", "bluey", "peppa", "pig", "ocean", "eleven", "godfather", "dalmatians", "beyonce",
	"knowles", "kendrick", "thriller", "michael", "jackson", "chianti", "psychology", "witz", "gnocchi",
}

func BenchmarkKeys(b *testing.B) {
	dst := make([]string, 0, 3)

	b.ReportAllocs()

	for i := range b.N {
		dst = Encoder{}.Keys(dst[:0], benchWords[i%len(benchWords)])
	}
}
