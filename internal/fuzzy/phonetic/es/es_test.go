package es

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
		// Betacism, silent h, yeísmo, seseo.
		{"volver", []string{"BLBR"}},
		{"hola", []string{"AL"}},
		{"ella", []string{"AY"}},
		{"llamas", []string{"YM", "YMS"}},
		{"celda", []string{"SLT"}},
		{"zapato", []string{"SPT"}},
		// Digraphs, soft g, assimilated n.
		{"chico", []string{"XK"}},
		{"quevedo", []string{"KBT"}},
		{"guerra", []string{"GR"}},
		{"gente", []string{"JNT"}},
		{"accion", []string{"AKSN"}},
		{"bunbury", []string{"BMBR"}},
		{"xavier", []string{"SBR"}},
		// Final s and d dropped in the primary key, except in short words; y as a vowel.
		{"iglesias", []string{"AGLS", "AGLSS"}},
		{"madrid", []string{"MTR", "MTRT"}},
		{"bis", []string{"BS"}},
		{"hay", []string{"A"}},
		{"yo", []string{"Y"}},
		// Pass-through and fallback.
		{"211", []string{"211"}},
		{"h", []string{"h"}},
		{"", []string{""}},
	} {
		assert.Equal(t, tc.want, Encoder{}.Keys(nil, tc.word), tc.word)
	}

	assert.Equal(t, []string{"x", "BNN"}, Encoder{}.Keys([]string{"x"}, "veneno"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("es-MX"))
}

// TestHomophones pins the Spanish spellings of one sound that English Double Metaphone keeps apart.
func TestHomophones(t *testing.T) {
	for _, pair := range [][2]string{
		{"vis", "bis"},
		{"veneno", "beneno"},
		{"velvet", "belbet"},
		{"isabel", "isavel"},
		{"ella", "eya"},
		{"hable", "able"},
		{"gogh", "gog"},
		{"iglesias", "iglesia"},
		{"bunbury", "bumbury"},
		{"llamas", "yamas"},
		{"celda", "selda"},
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
		word := rapid.OneOf(rapid.String(), rapid.StringMatching(`[a-z]{0,20}`), rapid.StringMatching(`[a-z0-9ñáéíóú]{0,20}`)).Draw(t, "word")
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

	for word, want := range map[string]float64{"iglesias": 2, "volver": 1, "211": 0} {
		got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], word) })
		assert.InDelta(t, want, got, 0, "%s: only the returned keys allocate", word)
	}
}

// benchWords are Spanish title words.
var benchWords = []string{
	"volver", "hable", "ella", "todo", "sobre", "madre", "mujeres", "borde", "ataque", "nervios", "laberinto", "fauno",
	"otros", "orfanato", "adentro", "ocho", "apellidos", "vascos", "campeones", "celda", "abre", "ojos", "torrente",
	"brazo", "tonto", "secreto", "vivir", "facil", "cerrados", "relatos", "salvajes", "casa", "papel", "elite", "serrano",
	"aqui", "quien", "viva", "cuentame", "ministerio",
}

// BenchmarkKeys compares the Spanish encoder with the English Double Metaphone that Spanish titles would otherwise fall back to.
func BenchmarkKeys(b *testing.B) {
	for _, impl := range []struct {
		name string
		enc  phonetic.Encoder
	}{{"es", Encoder{}}, {"en", en.Encoder{}}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			dst := make([]string, 0, 3)

			b.ReportAllocs()

			for i := range b.N {
				dst = impl.enc.Keys(dst[:0], benchWords[i%len(benchWords)])
			}
		})
	}
}
