package fr

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	"github.com/nightah/encore/internal/fuzzy/phonetic/en"
)

func TestKeys(t *testing.T) {
	for _, tc := range []struct {
		word string
		want []string
	}{
		// Silent endings: plural marks, final t/d/p, g/c after n, -er.
		{"bronzes", []string{"PRNS", "PRNSS"}},
		{"bronzer", []string{"PRNS", "PRNSR"}},
		{"cent", []string{"SN", "SNT"}},
		{"sang", []string{"SN", "SNK"}},
		{"sans", []string{"SN", "SNS"}},
		{"taxis", []string{"TKS", "TKSS"}},
		{"florette", []string{"FLRT"}},
		{"floret", []string{"FLR", "FLRT"}},
		// Short words keep their last consonant.
		{"dit", []string{"TT"}},
		{"mer", []string{"MR"}},
		// Silent h, and a leading vowel sound becomes A.
		{"hallyday", []string{"ALT"}},
		{"haine", []string{"AN"}},
		// Digraphs and soft c/g.
		{"chtis", []string{"XT", "XTS"}},
		{"christ", []string{"KRS", "KRST"}},
		{"philippe", []string{"FLP"}},
		{"agneau", []string{"AN"}},
		{"guerre", []string{"KR"}},
		{"genou", []string{"JN"}},
		{"arnaqueur", []string{"ARNKR"}},
		{"accent", []string{"AKSN", "AKSNT"}},
		{"nation", []string{"NSN"}},
		{"question", []string{"KSTN"}},
		{"champ", []string{"XN", "XNP"}},
		{"xavier", []string{"SF", "SFR"}},
		// Pass-through and fallback.
		{"2049", []string{"2049"}},
		{"h", []string{"h"}},
		{"", []string{""}},
	} {
		assert.Equal(t, tc.want, Encoder{}.Keys(nil, tc.word), tc.word)
	}

	assert.Equal(t, []string{"x", "KML", "KMLT"}, Encoder{}.Keys([]string{"x"}, "camelot"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("fr-CA"))
}

// TestHomophones pins the French spellings of one sound that English Double Metaphone keeps apart.
func TestHomophones(t *testing.T) {
	for _, pair := range [][2]string{
		{"bronzes", "bronzer"},
		{"cent", "sang"},
		{"sang", "sans"},
		{"visiteurs", "visiteur"},
		{"hallyday", "alliday"},
		{"haine", "aine"},
		{"camelot", "kaamelott"},
		{"floret", "florette"},
		{"engrenages", "engrenage"},
		{"chtis", "shtis"},
	} {
		a, b := Encoder{}.Keys(nil, pair[0]), Encoder{}.Keys(nil, pair[1])
		assert.True(t, shareKey(a, b), "%s %q and %s %q should share a key", pair[0], a, pair[1], b)
	}
}

func shareKey(a, b []string) bool {
	return slices.ContainsFunc(a, func(k string) bool { return slices.Contains(b, k) })
}

func TestPropKeysContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		word := rapid.OneOf(rapid.String(), rapid.StringMatching(`[a-z]{0,20}`), rapid.StringMatching(`[a-z0-9éèàç]{0,20}`)).Draw(t, "word")
		keys := Encoder{}.Keys(nil, word)

		require.NotEmpty(t, keys)
		require.LessOrEqual(t, len(keys), 2)
		require.Equal(t, keys, Encoder{}.Keys(nil, word), "deterministic")

		for i, k := range keys {
			require.False(t, slices.Contains(keys[:i], k), "duplicate key %q", k)

			if k == word {
				continue
			}

			require.NotEmpty(t, k)

			for j := range len(k) {
				require.True(t, k[j] >= 'A' && k[j] <= 'Z', "key %q", k)
			}
		}

		if phonetic.IsDigits(word) {
			require.Equal(t, []string{word}, keys)
		}
	})
}

func TestKeysAllocations(t *testing.T) {
	dst := make([]string, 0, 3)

	for word, want := range map[string]float64{"bronzes": 2, "kaamelott": 1, "2049": 0} {
		got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], word) })
		assert.InDelta(t, want, got, 0, "%s: only the returned keys allocate", word)
	}
}

// benchWords are French title words.
var benchWords = []string{
	"fabuleux", "destin", "amelie", "poulain", "intouchables", "bronzes", "font", "ski", "diner", "cons", "visiteurs",
	"bienvenue", "chez", "chtis", "choristes", "haine", "grande", "vadrouille", "asterix", "obelix", "mission",
	"cleopatre", "cite", "peur", "tontons", "flingueurs", "grand", "bleu", "caire", "espions", "taxi", "cyrano",
	"bergerac", "florette", "sources", "femmes", "arnacoeur", "petit", "nicolas", "kaamelott",
}

// BenchmarkKeys compares the French encoder with the English Double Metaphone it would replace for French titles.
func BenchmarkKeys(b *testing.B) {
	for _, impl := range []struct {
		name string
		enc  phonetic.Encoder
	}{{"fr", Encoder{}}, {"en", en.Encoder{}}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			dst := make([]string, 0, 3)

			b.ReportAllocs()

			for i := range b.N {
				dst = impl.enc.Keys(dst[:0], benchWords[i%len(benchWords)])
			}
		})
	}
}
