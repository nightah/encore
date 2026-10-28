package de

import (
	"slices"
	"strings"
	"testing"

	lib "github.com/f1monkey/phonetic/cologne"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
)

// cologneVectors were captured from github.com/f1monkey/phonetic v1.4.2. The first three are the published examples for Kölner Phonetik (Müller-Lüdenscheidt, Wikipedia, Breschnew); the rest pin the reference's edge cases.
var cologneVectors = []struct{ word, code string }{
	{"müller-lüdenscheidt", "65752682"},
	{"wikipedia", "3412"},
	{"breschnew", "17863"},
	{"heinz", "068"},
	{"kühn", "46"},
	{"dreissig", "2784"},
	{"ßtraße", "8278"},
	{"c", "4"},
	{"cäsar", "487"},
	{"xaver", "4837"},
	{"acht", "042"},
	{"chor", "47"},
	{"christ", "4782"},
	{"sachs", "848"},
	{"philipp", "3513"},
	{"hannover", "0637"},
	{"p", "3"},
	{"app", "013"},
	{"tatort", "2272"},
	{"rammstein", "76826"},
	{"grönemeyer", "47667"},
	{"zweiohrküken", "837446"},
	{"é", ""},
	{"1a2b", "01"},
	{"", ""},
	{"aachen", "046"},
	{"matsch", "68"},
	{"schwarz", "8378"},
	{"rechtsanwalt", "7486352"},
}

func TestCologneVectors(t *testing.T) {
	for _, v := range cologneVectors {
		assert.Equal(t, v.code, string(cologne(nil, v.word)), v.word)
	}
}

func TestKeys(t *testing.T) {
	for _, tc := range []struct {
		word string
		want []string
	}{
		{"tatort", []string{"2272"}},
		{"2049", []string{"2049"}},
		{"é", []string{"é"}},
		{"", []string{""}},
	} {
		assert.Equal(t, tc.want, Encoder{}.Keys(nil, tc.word), tc.word)
	}

	assert.Equal(t, []string{"x", "2272"}, Encoder{}.Keys([]string{"x"}, "tatort"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("de-AT"))
	assert.Equal(t, Encoder{}, phonetic.For("DE_de"))
}

var reference = lib.NewEncoder()

// refKeys is internal/core/match's cologneKeys over the library, the behaviour Keys must reproduce.
func refKeys(tok string) []string {
	if phonetic.IsDigits(tok) {
		return []string{tok}
	}

	if k := reference.Encode(tok); k != "" {
		return []string{k}
	}

	return []string{tok}
}

// fataler is what checkSame needs from testing.T and rapid.T.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

func checkSame(t fataler, word string) {
	t.Helper()

	if got, want := string(cologne(nil, word)), reference.Encode(word); got != want {
		t.Fatalf("cologne(%q) = %q; library %q", word, got, want)
	}

	if got, want := (Encoder{}).Keys(nil, word), refKeys(word); !slices.Equal(got, want) {
		t.Fatalf("Keys(%q) = %q; reference %q", word, got, want)
	}
}

// TestDifferentialExhaustive compares every word of up to four letters from a-z and the German extras with the library.
func TestDifferentialExhaustive(t *testing.T) {
	alphabet := []rune("abcdefghijklmnopqrstuvwxyzäöüß")
	word := make([]rune, 0, 4)

	var walk func()
	walk = func() {
		checkSame(t, string(word))

		if len(word) == cap(word) {
			return
		}

		for _, r := range alphabet {
			word = append(word, r)
			walk()
			word = word[:len(word)-1]
		}
	}

	walk()
}

func genWord() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.String(),
		rapid.StringMatching(`[a-zäöüßéè]{0,24}`),
		rapid.StringMatching(`(sch|ch|chs|cks|ph|dt|tz|ck|cz|x|c|qu|ß|[a-z]){1,8}`),
		rapid.StringMatching(`[A-Za-zÄÖÜ0-9 '-]{0,30}`),
	)
}

func TestDifferentialRapid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		checkSame(t, genWord().Draw(t, "word"))
	})
}

func TestDifferentialInvalidUTF8(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		checkSame(t, string(rapid.SliceOfN(rapid.Byte(), 0, 32).Draw(t, "bytes")))
	})
}

func TestDifferentialLong(t *testing.T) {
	for _, w := range []string{strings.Repeat("rechtsanwalt", 10), strings.Repeat("ß", 40), strings.Repeat("ab", 100)} {
		checkSame(t, w)
	}
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

	for word, want := range map[string]float64{"tatort": 1, "2049": 0} {
		got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], word) })
		assert.InDelta(t, want, got, 0, "%s: only the returned key allocates", word)
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
	b.Run("impl=intree", func(b *testing.B) {
		dst := make([]string, 0, 3)

		b.ReportAllocs()

		for i := range b.N {
			dst = Encoder{}.Keys(dst[:0], benchWords[i%len(benchWords)])
		}
	})

	b.Run("impl=library", func(b *testing.B) {
		b.ReportAllocs()

		for i := range b.N {
			_ = refKeys(benchWords[i%len(benchWords)])
		}
	})
}
