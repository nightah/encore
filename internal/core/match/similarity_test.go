package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// similarityVectors were captured from go-edlib 1.7.0 (JaroWinklerSimilarity widened to float64, OSADamerauLevenshteinDistance), which scoring used before these functions; matching them exactly keeps scores and thresholds unchanged.
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

func TestPropJaroWinkler(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSimilarityText().Draw(t, "a")
		b := genSimilarityText().Draw(t, "b")
		ra, rb := []rune(a), []rune(b)
		jw := jaroWinkler(a, b)

		if jw < 0 || jw > 1 {
			t.Fatalf("jaroWinkler(%q, %q) = %v, outside [0, 1]", a, b, jw)
		}

		if back := jaroWinkler(b, a); back != jw {
			t.Fatalf("jaroWinkler(%q, %q) = %v but reversed %v", a, b, jw, back)
		}

		if j := float64(jaro(ra, rb)); jw < j {
			t.Fatalf("jaroWinkler(%q, %q) = %v, below Jaro %v", a, b, jw, j)
		}

		if same := jaroWinkler(a, a); len(ra) > 0 && same != 1 {
			t.Fatalf("jaroWinkler(%q, itself) = %v, want 1", a, same)
		}

		if jw == 1 && string(ra) != string(rb) {
			t.Fatalf("jaroWinkler(%q, %q) = 1 for different strings", a, b)
		}
	})
}

func TestPropOSADistance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSimilarityText().Draw(t, "a")
		b := genSimilarityText().Draw(t, "b")
		ra, rb := []rune(a), []rune(b)
		d := osaDistance(a, b)

		if back := osaDistance(b, a); back != d {
			t.Fatalf("osaDistance(%q, %q) = %d but reversed %d", a, b, d, back)
		}

		// At least the length difference, at most one substitution per rune of the longer string.
		if d < abs(len(ra)-len(rb)) || d > max(len(ra), len(rb)) {
			t.Fatalf("osaDistance(%q, %q) = %d, outside [%d, %d]", a, b, d, abs(len(ra)-len(rb)), max(len(ra), len(rb)))
		}

		if (d == 0) != (string(ra) == string(rb)) {
			t.Fatalf("osaDistance(%q, %q) = %d", a, b, d)
		}

		// Never more than insertions and deletions alone, which the independent LCS code measures.
		sa, sb := string(ra), string(rb)
		if indel := len(ra) + len(rb) - 2*lcsLen(sa, sb); d > indel {
			t.Fatalf("osaDistance(%q, %q) = %d, above the indel distance %d", a, b, d, indel)
		}

		// Swapping two adjacent runes costs exactly one edit when they differ.
		if len(ra) >= 2 {
			i := rapid.IntRange(0, len(ra)-2).Draw(t, "i")
			if ra[i] != ra[i+1] {
				sw := []rune(sa)
				sw[i], sw[i+1] = sw[i+1], sw[i]

				if got := osaDistance(sa, string(sw)); got != 1 {
					t.Fatalf("osaDistance(%q, %q) = %d after one swap, want 1", sa, string(sw), got)
				}
			}
		}
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
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
