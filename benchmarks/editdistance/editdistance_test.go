package editdistance

import (
	"strings"
	"testing"

	edlib "github.com/hbollon/go-edlib"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/core/match"
)

// The reference is go-edlib's JaroWinklerSimilarity (a float32, widened to float64) and OSADamerauLevenshteinDistance. The in-tree functions must return exactly the same values, bit for bit, so scores and the thresholds tuned on them match the reference's.

// genText favours a small alphabet, where matches, swaps and shared prefixes are common, but also draws arbitrary Unicode, invalid UTF-8 and strings longer than the 64 runes the in-tree functions keep on the stack.
func genText() *rapid.Generator[string] {
	small := rapid.StringOfN(rapid.RuneFrom([]rune("abcü 東😀")), 0, 12, -1)

	return rapid.OneOf(
		small,
		small,
		rapid.String(),
		rapid.StringOfN(rapid.RuneFrom([]rune("abü")), 60, 90, -1),
		rapid.Map(rapid.SliceOfN(rapid.Byte(), 0, 12), func(b []byte) string { return string(b) }),
	)
}

// fataler is what same needs from *testing.T and *rapid.T.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

func same(t fataler, a, b string) {
	t.Helper()

	if got, want := match.JaroWinkler(a, b), float64(edlib.JaroWinklerSimilarity(a, b)); got != want {
		t.Fatalf("JaroWinkler(%q, %q) = %v, go-edlib %v", a, b, got, want)
	}

	if got, want := match.OSADistance(a, b), edlib.OSADamerauLevenshteinDistance(a, b); got != want {
		t.Fatalf("OSADistance(%q, %q) = %d, go-edlib %d", a, b, got, want)
	}
}

func TestParityGenerated(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		same(t, genText().Draw(t, "a"), genText().Draw(t, "b"))
	})
}

// TestParityExhaustive compares every pair of strings of up to three runes over an alphabet with a repeated letter, an accented letter and a character outside the Basic Multilingual Plane.
func TestParityExhaustive(t *testing.T) {
	alphabet := []rune("abü😀")

	var words []string

	var walk func(w []rune)
	walk = func(w []rune) {
		words = append(words, string(w))

		if len(w) == 3 {
			return
		}

		for _, r := range alphabet {
			walk(append(w, r))
		}
	}

	walk(nil)

	for _, a := range words {
		for _, b := range words {
			same(t, a, b)
		}
	}
}

func TestParityTitles(t *testing.T) {
	for _, p := range benchPairs {
		same(t, p[0], p[1])
		same(t, p[1], p[0])
	}

	same(t, strings.Repeat("ab", 40), strings.Repeat("ba", 40))
	same(t, strings.Repeat("ü", 70), strings.Repeat("ü", 69)+"u")
}

// benchPairs are what the scorer compares: a mishearing and the title, word order swapped, spelt-out umlauts.
var benchPairs = [][2]string{
	{"seinfield", "seinfeld"},
	{"lord of the rinks", "lord of the rings return of the king"},
	{"zwei ohr kueken", "zweiohrküken"},
	{"the office", "office the"},
}

var (
	sinkFloat float64
	sinkInt   int
)

func BenchmarkJaroWinkler(b *testing.B) {
	b.Run("impl=edlib", func(b *testing.B) {
		b.ReportAllocs()

		for i := range b.N {
			p := benchPairs[i%len(benchPairs)]
			sinkFloat = float64(edlib.JaroWinklerSimilarity(p[0], p[1]))
		}
	})

	b.Run("impl=intree", func(b *testing.B) {
		b.ReportAllocs()

		for i := range b.N {
			p := benchPairs[i%len(benchPairs)]
			sinkFloat = match.JaroWinkler(p[0], p[1])
		}
	})
}

func BenchmarkOSADistance(b *testing.B) {
	b.Run("impl=edlib", func(b *testing.B) {
		b.ReportAllocs()

		for i := range b.N {
			p := benchPairs[i%len(benchPairs)]
			sinkInt = edlib.OSADamerauLevenshteinDistance(p[0], p[1])
		}
	})

	b.Run("impl=intree", func(b *testing.B) {
		b.ReportAllocs()

		for i := range b.N {
			p := benchPairs[i%len(benchPairs)]
			sinkInt = match.OSADistance(p[0], p[1])
		}
	})
}
