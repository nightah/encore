package match

import (
	"strings"
	"testing"

	edlib "github.com/hbollon/go-edlib"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// similarityVectors were captured from go-edlib 1.7.0 (JaroWinklerSimilarity widened to float64, OSADamerauLevenshteinDistance).
var similarityVectors = []struct {
	a, b string
	jw   float64
	osa  int
}{
	{"", "", 0, 0},
	{"", "abc", 0, 3},
	{"abc", "", 0, 3},
	{"a", "a", 1, 0},
	{"a", "b", 0, 1},
	{"ab", "ba", 0, 1},
	{"ab", "abc", 0.9111111164093018, 1},
	{"martha", "marhta", 0.9611110687255859, 1},
	{"dixon", "dicksonx", 0.8133333325386047, 4},
	{"jellyfish", "smellyfish", 0.8962963223457336, 2},
	{"ca", "abc", 0, 3},
	{"abcdef", "badcfe", 0.8333333134651184, 3},
	{"seinfeld", "seinfield", 0.9777777791023254, 1},
	{"star wars", "star warz", 0.9555555582046509, 1},
	{"rocky 2", "rocky 3", 0.9428572058677673, 1},
	{"the office", "office the", 0.7208333015441895, 8},
	{"lord of the rings", "lord of the rinks", 0.9764705300331116, 1},
	{"bluey", "blue y", 0.9666666388511658, 1},
	{"matrix", "the matrix reloaded", 0.6885964870452881, 13},
	{"brücke", "brucke", 0.911111056804657, 1},
	{"brücke", "bruecke", 0.7993651032447815, 2},
	{"amélie", "amelie", 0.911111056804657, 1},
	{"zweiohrküken", "zwei ohr kueken", 0.9118182063102722, 4},
	{"crème brûlée", "creme brulee", 0.8370370268821716, 3},
	{"東京物語", "東京", 0.8666666746139526, 2},
	{"東京物語", "物語東京", 0, 4},
	{"😀a", "a😀", 0, 1},
	{"ñandú", "nandu", 0.7333333492279053, 2},
	// Invalid UTF-8 decodes to U+FFFD.
	{"\xff\xfe", "\xfd", 0.8499999642372131, 1},
	{"a\xffb", "ab", 0.6499999761581421, 1},
	// Longer than stackRunes takes the heap path.
	{strings.Repeat("ab", 40), strings.Repeat("ba", 40), 0.8333333134651184, 2},
	{strings.Repeat("ü", 70), strings.Repeat("ü", 69) + "u", 0.9942857027053833, 1},
}

func TestSimilarityVectors(t *testing.T) {
	for _, v := range similarityVectors {
		assert.Equal(t, v.jw, jaroWinkler(v.a, v.b), "jaroWinkler(%q, %q)", v.a, v.b) //nolint:testifylint // exact: scores must not drift from the captured values
		assert.Equal(t, v.osa, osaDistance(v.a, v.b), "osaDistance(%q, %q)", v.a, v.b)
	}
}

// genSimilarityText favours a small alphabet, where matches, swaps and shared prefixes are common, but also draws arbitrary Unicode, invalid UTF-8 and strings longer than stackRunes.
func genSimilarityText() *rapid.Generator[string] {
	small := rapid.StringOfN(rapid.RuneFrom([]rune("abcü 東😀")), 0, 12, -1)

	return rapid.OneOf(
		small,
		small,
		rapid.String(),
		rapid.StringOfN(rapid.RuneFrom([]rune("abü")), 60, 90, -1),
		rapid.Map(rapid.SliceOfN(rapid.Byte(), 0, 12), func(b []byte) string { return string(b) }),
	)
}

func TestPropSimilarityMatchesEdlib(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSimilarityText().Draw(t, "a")
		b := genSimilarityText().Draw(t, "b")

		if got, want := jaroWinkler(a, b), float64(edlib.JaroWinklerSimilarity(a, b)); got != want {
			t.Fatalf("jaroWinkler(%q, %q) = %v, go-edlib %v", a, b, got, want)
		}

		if got, want := osaDistance(a, b), edlib.OSADamerauLevenshteinDistance(a, b); got != want {
			t.Fatalf("osaDistance(%q, %q) = %d, go-edlib %d", a, b, got, want)
		}
	})
}

var (
	sinkFloat float64
	sinkInt   int
)

var similarityBenchPairs = [][2]string{
	{"seinfield", "seinfeld"},
	{"lord of the rinks", "lord of the rings return of the king"},
	{"zwei ohr kueken", "zweiohrküken"},
	{"the office", "office the"},
}

func BenchmarkJaroWinkler(b *testing.B) {
	b.ReportAllocs()

	for i := range b.N {
		p := similarityBenchPairs[i%len(similarityBenchPairs)]
		sinkFloat = jaroWinkler(p[0], p[1])
	}
}

func BenchmarkOSADistance(b *testing.B) {
	b.ReportAllocs()

	for i := range b.N {
		p := similarityBenchPairs[i%len(similarityBenchPairs)]
		sinkInt = osaDistance(p[0], p[1])
	}
}

func BenchmarkEdlibJaroWinkler(b *testing.B) {
	b.ReportAllocs()

	for i := range b.N {
		p := similarityBenchPairs[i%len(similarityBenchPairs)]
		sinkFloat = float64(edlib.JaroWinklerSimilarity(p[0], p[1]))
	}
}

func BenchmarkEdlibOSADistance(b *testing.B) {
	b.ReportAllocs()

	for i := range b.N {
		p := similarityBenchPairs[i%len(similarityBenchPairs)]
		sinkInt = edlib.OSADamerauLevenshteinDistance(p[0], p[1])
	}
}
