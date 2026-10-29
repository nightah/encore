package pt

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
		// Sibilants, ch and x.
		{"cassia", []string{"KS"}},
		{"cacia", []string{"KS"}},
		{"brazileiro", []string{"BRSLR"}},
		{"xuxa", []string{"XX"}},
		{"chuchu", []string{"XX"}},
		{"show", []string{"X"}},
		{"texto", []string{"TST"}},
		{"exame", []string{"ASM"}},
		{"torax", []string{"TRKS"}},
		{"enxame", []string{"ANXM"}},
		// lh, nh, l before a consonant or at the end.
		{"filhos", []string{"FLS"}},
		{"minha", []string{"MN"}},
		{"brasil", []string{"BRS", "BRSL"}},
		{"gil", []string{"J", "JL"}},
		{"alto", []string{"AT", "ALT"}},
		// Nasals.
		{"jobim", []string{"JBN"}},
		{"homens", []string{"AMNS", "RMNS"}},
		{"chefao", []string{"XFN"}},
		{"falam", []string{"FLN"}},
		// Affricates and the epenthetic i.
		{"tim", []string{"XN", "TN"}},
		{"chim", []string{"XN"}},
		{"cidade", []string{"SDJ", "SDD"}},
		{"mutantes", []string{"MTNXS", "MTNTS"}},
		{"djavan", []string{"JVN", "DJVN"}},
		{"dijavan", []string{"JVN", "DJVN"}},
		{"diavan", []string{"JVN", "DVN"}},
		{"ritmo", []string{"RXM", "RTM"}},
		{"trem", []string{"TRN"}},
		{"atlas", []string{"ATLS"}},
		{"internet", []string{"ANTRNX", "ANTRNT"}},
		// g, j, gu, qu, th, h.
		{"gente", []string{"JNX", "JNT"}},
		{"guerra", []string{"GR"}},
		{"quatro", []string{"KTR"}},
		{"phil", []string{"F", "FL"}},
		{"the", []string{"J", "T"}},
		{"thrones", []string{"DRNS", "TRNS"}},
		{"hoje", []string{"AJ", "RJ"}},
		{"house", []string{"AS", "RS"}},
		{"hhh", []string{"hhh"}},
		// The epenthetic vowel before s and a consonant, and the dropped final r.
		{"iskank", []string{"SKNK"}},
		{"esse", []string{"AS"}},
		{"voltar", []string{"VT", "VLTR"}},
		{"dor", []string{"DR"}},
		// Pass-through and fallback.
		{"1984", []string{"1984"}},
		{"", []string{""}},
	} {
		assert.Equal(t, tc.want, Encoder{}.Keys(nil, tc.word), tc.word)
	}

	assert.Equal(t, []string{"x", "XX"}, Encoder{}.Keys([]string{"x"}, "xuxa"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("pt-BR"))
}

// TestHomophones pins the Brazilian Portuguese spellings of one sound, and the ways speech recognition writes them, that English Double Metaphone keeps apart.
func TestHomophones(t *testing.T) {
	for _, pair := range [][2]string{
		{"chucha", "xuxa"},
		{"xico", "chico"},
		{"seichas", "seixas"},
		{"gau", "gal"},
		{"brasiu", "brasil"},
		{"ton", "tom"},
		{"jobin", "jobim"},
		{"tin", "tim"},
		{"chim", "tim"},
		{"chitas", "titas"},
		{"diavan", "djavan"},
		{"javan", "djavan"},
		{"dijavan", "djavan"},
		{"mariguela", "marighella"},
		{"iskank", "skank"},
		{"djoker", "joker"},
		{"coudplei", "coldplay"},
		{"eliti", "elite"},
		{"carosel", "carrossel"},
		{"rouse", "house"},
		{"de", "the"},
		{"trones", "thrones"},
		{"volta", "voltar"},
		{"regiao", "regiam"},
	} {
		a, b := Encoder{}.Keys(nil, pair[0]), Encoder{}.Keys(nil, pair[1])
		assert.True(t, shareKey(a, b), "%s %q and %s %q should share a key", pair[0], a, pair[1], b)
	}
}

// TestDistinct pins sounds Brazilian Portuguese keeps apart, which a looser encoder would merge.
func TestDistinct(t *testing.T) {
	for _, pair := range [][2]string{
		{"vila", "bila"},
		{"faca", "vaca"},
		{"pato", "bato"},
		{"casa", "gasa"},
		{"filhos", "fios"},
		{"mala", "mana"},
	} {
		a, b := Encoder{}.Keys(nil, pair[0]), Encoder{}.Keys(nil, pair[1])
		assert.False(t, shareKey(a, b), "%s %q and %s %q should not share a key", pair[0], a, pair[1], b)
	}
}

func shareKey(a, b []string) bool {
	return slices.ContainsFunc(a, func(k string) bool { return slices.Contains(b, k) })
}

func TestPropKeysContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		word := rapid.OneOf(rapid.String(), rapid.StringMatching(`[a-z]{0,20}`), rapid.StringMatching(`[a-z0-9çãõáéíóú]{0,20}`)).Draw(t, "word")
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

	for word, want := range map[string]float64{"cidade": 2, "xuxa": 1, "1984": 0} {
		got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], word) })
		assert.InDelta(t, want, got, 0, "%s: only the returned keys allocate", word)
	}
}

// benchWords are Brazilian Portuguese title words.
var benchWords = []string{
	"cidade", "deus", "central", "brasil", "tropa", "elite", "auto", "compadecida", "horas", "volta", "bacurau", "ainda",
	"estou", "aqui", "carandiru", "filhos", "francisco", "minha", "mae", "pessa", "brasileiro", "lisbela", "prisioneiro",
	"bicho", "sete", "cabessas", "hoje", "quero", "voltar", "sozinho", "palhasso", "marighella", "poderoso", "chefao",
	"sintonia", "irmandade", "homens", "invisivel", "sessao", "terapia",
}

// BenchmarkKeys compares the Portuguese encoder with the English Double Metaphone that Portuguese titles would otherwise fall back to.
func BenchmarkKeys(b *testing.B) {
	for _, impl := range []struct {
		name string
		enc  phonetic.Encoder
	}{{"pt", Encoder{}}, {"en", en.Encoder{}}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			dst := make([]string, 0, 3)

			b.ReportAllocs()

			for i := range b.N {
				dst = impl.enc.Keys(dst[:0], benchWords[i%len(benchWords)])
			}
		})
	}
}
