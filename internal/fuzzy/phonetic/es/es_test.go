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
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		// Betacism, silent h, yeísmo, seseo.
		{name: "ShouldKeyVolver", have: "volver", expected: []string{"BLBR"}},
		{name: "ShouldKeyHola", have: "hola", expected: []string{"AL", "JL"}},
		{name: "ShouldKeyElla", have: "ella", expected: []string{"AY"}},
		{name: "ShouldKeyLlamas", have: "llamas", expected: []string{"YM", "YMS"}},
		{name: "ShouldKeyCelda", have: "celda", expected: []string{"SLT"}},
		{name: "ShouldKeyZapato", have: "zapato", expected: []string{"SPT"}},
		// Digraphs, soft g, assimilated n.
		{name: "ShouldKeyChico", have: "chico", expected: []string{"XK"}},
		{name: "ShouldKeyQuevedo", have: "quevedo", expected: []string{"KBT"}},
		{name: "ShouldKeyGuerra", have: "guerra", expected: []string{"GR"}},
		{name: "ShouldKeyGente", have: "gente", expected: []string{"JNT"}},
		{name: "ShouldKeyAccion", have: "accion", expected: []string{"AKSN"}},
		{name: "ShouldKeyBunbury", have: "bunbury", expected: []string{"BMBR"}},
		{name: "ShouldKeyXavier", have: "xavier", expected: []string{"SBR"}},
		// Final s and d dropped in the primary key, except in short words; y as a vowel.
		{name: "ShouldKeyIglesias", have: "iglesias", expected: []string{"AGLS", "AGLSS"}},
		{name: "ShouldKeyMadrid", have: "madrid", expected: []string{"MTR", "MTRT"}},
		{name: "ShouldKeyBis", have: "bis", expected: []string{"BS"}},
		{name: "ShouldKeyHay", have: "hay", expected: []string{"A", "J"}},
		{name: "ShouldKeyYo", have: "yo", expected: []string{"Y"}},
		// An h or w onset also keyed as the jota or gu a recogniser may have written that way.
		{name: "ShouldKeyHose", have: "hose", expected: []string{"AS", "JS"}},
		{name: "ShouldKeyHuan", have: "huan", expected: []string{"AN", "JN"}},
		{name: "ShouldKeyWeros", have: "weros", expected: []string{"BR", "BRS", "GR"}},
		{name: "ShouldKeyJose", have: "jose", expected: []string{"JS"}},
		{name: "ShouldKeyHjalmar", have: "hjalmar", expected: []string{"JLMR"}},
		{name: "ShouldFallBackToWordForDoubleH", have: "hh", expected: []string{"hh"}},
		// Pass-through and fallback.
		{name: "ShouldPassDigitsThrough", have: "211", expected: []string{"211"}},
		{name: "ShouldFallBackToWordForLoneH", have: "h", expected: []string{"h"}},
		{name: "ShouldFallBackToEmptyWord", expected: []string{""}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Encoder{}.Keys(nil, tc.have))
		})
	}

	assert.Equal(t, []string{"x", "BNN"}, Encoder{}.Keys([]string{"x"}, "veneno"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("es-MX"))
}

// TestHomophones pins the Spanish spellings of one sound that English Double Metaphone keeps apart.
func TestKeysAppendsToDst(t *testing.T) {
	testCases := []struct {
		name     string
		dst      []string
		have     string
		expected []string
	}{
		{name: "ShouldNotHideKeyAlreadyInDstFromAnotherWord", dst: []string{"JL"}, have: "hola", expected: []string{"JL", "AL", "JL"}},
		{name: "ShouldAppendEveryKeyAfterDst", dst: []string{"BR"}, have: "weros", expected: []string{"BR", "BR", "BRS", "GR"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Encoder{}.Keys(tc.dst, tc.have))
		})
	}
}

func TestHomophones(t *testing.T) {
	testCases := []struct {
		name  string
		word  string
		other string
	}{
		{name: "ShouldShareKeyBetweenVisAndBis", word: "vis", other: "bis"},
		{name: "ShouldShareKeyBetweenVenenoAndBeneno", word: "veneno", other: "beneno"},
		{name: "ShouldShareKeyBetweenVelvetAndBelbet", word: "velvet", other: "belbet"},
		{name: "ShouldShareKeyBetweenIsabelAndIsavel", word: "isabel", other: "isavel"},
		{name: "ShouldShareKeyBetweenEllaAndEya", word: "ella", other: "eya"},
		{name: "ShouldShareKeyBetweenHableAndAble", word: "hable", other: "able"},
		{name: "ShouldShareKeyBetweenGoghAndGog", word: "gogh", other: "gog"},
		{name: "ShouldShareKeyBetweenIglesiasAndIglesia", word: "iglesias", other: "iglesia"},
		{name: "ShouldShareKeyBetweenBunburyAndBumbury", word: "bunbury", other: "bumbury"},
		{name: "ShouldShareKeyBetweenLlamasAndYamas", word: "llamas", other: "yamas"},
		{name: "ShouldShareKeyBetweenCeldaAndSelda", word: "celda", other: "selda"},
		{name: "ShouldShareKeyBetweenJoseAndHose", word: "jose", other: "hose"},
		{name: "ShouldShareKeyBetweenJefeAndHefe", word: "jefe", other: "hefe"},
		{name: "ShouldShareKeyBetweenJaponAndHapon", word: "japon", other: "hapon"},
		{name: "ShouldShareKeyBetweenGuerosAndWeros", word: "gueros", other: "weros"},
		{name: "ShouldShareKeyBetweenGuerosAndWueros", word: "gueros", other: "wueros"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := Encoder{}.Keys(nil, tc.word), Encoder{}.Keys(nil, tc.other)
			assert.True(t, shareKey(a, b), "%s %q and %s %q should share a key", tc.word, a, tc.other, b)
		})
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
		require.LessOrEqual(t, len(keys), 3)
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

	testCases := []struct {
		name     string
		have     string
		expected float64
	}{
		{name: "ShouldAllocateTwiceForIglesias", have: "iglesias", expected: 2},
		{name: "ShouldAllocateOnceForVolver", have: "volver", expected: 1},
		{name: "ShouldNotAllocateForDigits", have: "211"},
		{name: "ShouldAllocateTwiceForHola", have: "hola", expected: 2},
		{name: "ShouldNotAllocateForHay", have: "hay"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], tc.have) })
			assert.InDelta(t, tc.expected, got, 0, "only the returned keys allocate")
		})
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
