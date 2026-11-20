package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// similarityVectors are go-edlib 1.7.0's values (JaroWinklerSimilarity widened to float64, OSADamerauLevenshteinDistance); matching them exactly keeps scores, and the thresholds tuned on them, identical to go-edlib's.
var similarityVectors = []struct {
	name string
	a    string
	b    string
	jw   float64
	osa  int
}{
	{name: "ShouldScoreTwoEmptyStrings"},
	{name: "ShouldScoreEmptyAgainstText", b: "abc", osa: 3},
	{name: "ShouldScoreTextAgainstEmpty", a: "abc", osa: 3},
	{name: "ShouldScoreEqualRune", a: "a", b: "a", jw: 1},
	{name: "ShouldScoreDifferentRune", a: "a", b: "b", osa: 1},
	{name: "ShouldScoreSwappedPair", a: "ab", b: "ba", osa: 1},
	{name: "ShouldScoreAppendedRune", a: "ab", b: "abc", jw: 0.9111111164093018, osa: 1},
	{name: "ShouldScoreMarthaTransposition", a: "martha", b: "marhta", jw: 0.9611110687255859, osa: 1},
	{name: "ShouldScoreDixonAgainstDicksonx", a: "dixon", b: "dicksonx", jw: 0.8133333325386047, osa: 4},
	{name: "ShouldScoreJellyfishAgainstSmellyfish", a: "jellyfish", b: "smellyfish", jw: 0.8962963223457336, osa: 2},
	{name: "ShouldScoreTranspositionThatOSACannotEditTwice", a: "ca", b: "abc", osa: 3},
	{name: "ShouldScoreThreeSwappedPairs", a: "abcdef", b: "badcfe", jw: 0.8333333134651184, osa: 3},
	{name: "ShouldScoreSeinfeldTypo", a: "seinfeld", b: "seinfield", jw: 0.9777777791023254, osa: 1},
	{name: "ShouldScoreStarWarsTypo", a: "star wars", b: "star warz", jw: 0.9555555582046509, osa: 1},
	{name: "ShouldScoreSequelNumber", a: "rocky 2", b: "rocky 3", jw: 0.9428572058677673, osa: 1},
	{name: "ShouldScoreSwappedWords", a: "the office", b: "office the", jw: 0.7208333015441895, osa: 8},
	{name: "ShouldScoreLordOfTheRingsTypo", a: "lord of the rings", b: "lord of the rinks", jw: 0.9764705300331116, osa: 1},
	{name: "ShouldScoreSplitWord", a: "bluey", b: "blue y", jw: 0.9666666388511658, osa: 1},
	{name: "ShouldScoreTitleInsideLongerTitle", a: "matrix", b: "the matrix reloaded", jw: 0.6885964870452881, osa: 13},
	{name: "ShouldScoreUmlautAgainstPlainVowel", a: "brücke", b: "brucke", jw: 0.911111056804657, osa: 1},
	{name: "ShouldScoreUmlautAgainstSpelledOutUmlaut", a: "brücke", b: "bruecke", jw: 0.7993651032447815, osa: 2},
	{name: "ShouldScoreAccentAgainstPlainVowel", a: "amélie", b: "amelie", jw: 0.911111056804657, osa: 1},
	{name: "ShouldScoreCompoundAgainstSpacedWords", a: "zweiohrküken", b: "zwei ohr kueken", jw: 0.9118182063102722, osa: 4},
	{name: "ShouldScoreAccentsAgainstPlainVowels", a: "crème brûlée", b: "creme brulee", jw: 0.8370370268821716, osa: 3},
	{name: "ShouldScoreKanjiPrefix", a: "東京物語", b: "東京", jw: 0.8666666746139526, osa: 2},
	{name: "ShouldScoreSwappedKanjiWords", a: "東京物語", b: "物語東京", osa: 4},
	{name: "ShouldScoreSwappedEmoji", a: "😀a", b: "a😀", osa: 1},
	{name: "ShouldScoreTildeAgainstPlainLetters", a: "ñandú", b: "nandu", jw: 0.7333333492279053, osa: 2},
	// Invalid UTF-8 decodes to U+FFFD.
	{name: "ShouldScoreInvalidUTF8", a: "\xff\xfe", b: "\xfd", jw: 0.8499999642372131, osa: 1},
	{name: "ShouldScoreInvalidUTF8InsideText", a: "a\xffb", b: "ab", jw: 0.6499999761581421, osa: 1},
	// Longer than stackRunes takes the heap path.
	{name: "ShouldScoreLongSwappedPairsOnHeap", a: strings.Repeat("ab", 40), b: strings.Repeat("ba", 40), jw: 0.8333333134651184, osa: 2},
	{name: "ShouldScoreLongUmlautsOnHeap", a: strings.Repeat("ü", 70), b: strings.Repeat("ü", 69) + "u", jw: 0.9942857027053833, osa: 1},
}

func TestSimilarityVectors(t *testing.T) {
	for _, tc := range similarityVectors {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.jw, jaroWinkler(tc.a, tc.b)) //nolint:testifylint // exact: scores must not drift from the captured values
			assert.Equal(t, tc.osa, osaDistance(tc.a, tc.b))
		})
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
