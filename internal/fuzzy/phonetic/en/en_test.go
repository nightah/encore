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

// metaphoneVectors were captured from github.com/PhonoGrams/double_metaphone v0.1.0 EncodeUncapped. Most match Philips's published examples (Smith, Schmidt, Caesar, Chianti, Michael, Thumb, Sugar, Edge); "Jose" codes JS/HS rather than Philips's HS because the library's San/Jose rule tests for a space that letters-only input never has.
var metaphoneVectors = []struct{ word, primary, alternate string }{
	{"thompson", "TMPSN", ""},
	{"smith", "SM0", "XMT"},
	{"schmidt", "XMT", "SMT"},
	{"xavier", "SF", "SFR"},
	{"caesar", "SSR", ""},
	{"chianti", "KNT", ""},
	{"michael", "MKL", "MXL"},
	{"thumb", "0M", "TM"},
	{"jose", "JS", "HS"},
	{"sugar", "XKR", "SKR"},
	{"edge", "AJ", ""},
	{"gnocchi", "NX", ""},
	{"tagliaro", "TKLR", "TLR"},
	{"bellocchio", "PLX", ""},
	{"pepper", "PPR", ""},
	{"peppa", "PP", ""},
	{"cabrillo", "KPRL", ""},
	{"gallegos", "KLKS", ""},
	{"dumb", "TM", ""},
	{"knight", "NT", ""},
	{"wright", "RT", ""},
	{"psychology", "SXLJ", "SKLK"},
	{"witz", "ATS", "FFX"},
	{"filipowicz", "FLPTS", "FLPFX"},
	{"aggie", "AJ", "AK"},
	{"danger", "TNJR", "TNKR"},
	{"ghislane", "JLN", ""},
	{"laugh", "LF", ""},
	{"tough", "TF", ""},
	{"hugh", "H", ""},
	{"bacher", "PKR", ""},
	{"orchestra", "ARKSTR", ""},
	{"mcclellan", "MKLLN", ""},
	{"succeed", "SKST", ""},
	{"bacci", "PX", ""},
	{"accident", "AKSTNT", ""},
	{"czerny", "SRN", "XRN"},
	{"zhao", "J", ""},
	{"zola", "SL", ""},
	{"island", "ALNT", ""},
	{"school", "SKL", ""},
	{"schenker", "XNKR", "SKNKR"},
	{"schooner", "SKNR", ""},
	{"sian", "SN", "XN"},
	{"snider", "SNTR", "XNTR"},
	{"tichner", "TXNR", "TKNR"},
	{"thomas", "TMS", ""},
	{"kaiser", "KSR", ""},
	{"harry", "HR", ""},
	{"ocean", "ASN", ""},
	{"x", "S", ""},
	{"", "", ""},
}

func TestDoubleMetaphoneVectors(t *testing.T) {
	for _, v := range metaphoneVectors {
		p, a := doubleMetaphone(v.word)
		assert.Equal(t, v.primary, p, v.word)
		assert.Equal(t, v.alternate, a, v.word)
	}
}

func TestKeys(t *testing.T) {
	for _, tc := range []struct {
		word string
		want []string
	}{
		{"smith", []string{"SM0", "XMT"}},
		{"pepper", []string{"PPR", "PP"}},
		{"peppa", []string{"PP"}},
		{"2049", []string{"2049"}},
		{"123abc", []string{"APK"}},
		{"é", []string{"é"}},
		{"", []string{""}},
		{"snider", []string{"SNTR", "XNTR", "SNT"}},
	} {
		assert.Equal(t, tc.want, Encoder{}.Keys(nil, tc.word), tc.word)
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

	for word, want := range map[string]float64{"pepper": 1, "smith": 2, "2049": 0} {
		got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], word) })
		assert.InDelta(t, want, got, 0, "%s: only the returned keys allocate", word)
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
