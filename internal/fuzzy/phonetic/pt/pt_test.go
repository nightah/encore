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
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		// Sibilants, ch and x.
		{name: "ShouldKeyCassia", have: "cassia", expected: []string{"KS"}},
		{name: "ShouldKeyCacia", have: "cacia", expected: []string{"KS"}},
		{name: "ShouldKeyBrazileiro", have: "brazileiro", expected: []string{"BRSLR"}},
		{name: "ShouldKeyXuxa", have: "xuxa", expected: []string{"XX"}},
		{name: "ShouldKeyChuchu", have: "chuchu", expected: []string{"XX"}},
		{name: "ShouldKeyShow", have: "show", expected: []string{"X"}},
		{name: "ShouldKeyTexto", have: "texto", expected: []string{"TST"}},
		{name: "ShouldKeyExame", have: "exame", expected: []string{"ASM"}},
		{name: "ShouldKeyTorax", have: "torax", expected: []string{"TRKS"}},
		{name: "ShouldKeyEnxame", have: "enxame", expected: []string{"ANXM"}},
		// lh, nh, l before a consonant or at the end.
		{name: "ShouldKeyFilhos", have: "filhos", expected: []string{"FLS"}},
		{name: "ShouldKeyMinha", have: "minha", expected: []string{"MN"}},
		{name: "ShouldKeyBrasil", have: "brasil", expected: []string{"BRS", "BRSL"}},
		{name: "ShouldKeyGil", have: "gil", expected: []string{"J", "JL"}},
		{name: "ShouldKeyAlto", have: "alto", expected: []string{"AT", "ALT"}},
		// Nasals.
		{name: "ShouldKeyJobim", have: "jobim", expected: []string{"JBN"}},
		{name: "ShouldKeyHomens", have: "homens", expected: []string{"AMNS", "RMNS"}},
		{name: "ShouldKeyChefao", have: "chefao", expected: []string{"XFN"}},
		{name: "ShouldKeyFalam", have: "falam", expected: []string{"FLN"}},
		// Affricates and the epenthetic i.
		{name: "ShouldKeyTim", have: "tim", expected: []string{"XN", "TN"}},
		{name: "ShouldKeyChim", have: "chim", expected: []string{"XN"}},
		{name: "ShouldKeyCidade", have: "cidade", expected: []string{"SDJ", "SDD"}},
		{name: "ShouldKeyMutantes", have: "mutantes", expected: []string{"MTNXS", "MTNTS"}},
		{name: "ShouldKeyDjavan", have: "djavan", expected: []string{"JVN", "DJVN"}},
		{name: "ShouldKeyDijavan", have: "dijavan", expected: []string{"JVN", "DJVN"}},
		{name: "ShouldKeyDiavan", have: "diavan", expected: []string{"JVN", "DVN"}},
		{name: "ShouldKeyRitmo", have: "ritmo", expected: []string{"RXM", "RTM"}},
		{name: "ShouldKeyTrem", have: "trem", expected: []string{"TRN"}},
		{name: "ShouldKeyAtlas", have: "atlas", expected: []string{"ATLS"}},
		{name: "ShouldKeyInternet", have: "internet", expected: []string{"ANTRNX", "ANTRNT"}},
		// g, j, gu, qu, th, h.
		{name: "ShouldKeyGente", have: "gente", expected: []string{"JNX", "JNT"}},
		{name: "ShouldKeyGuerra", have: "guerra", expected: []string{"GR"}},
		{name: "ShouldKeyQuatro", have: "quatro", expected: []string{"KTR"}},
		{name: "ShouldKeyPhil", have: "phil", expected: []string{"F", "FL"}},
		{name: "ShouldKeyThe", have: "the", expected: []string{"J", "T"}},
		{name: "ShouldKeyThrones", have: "thrones", expected: []string{"DRNS", "TRNS"}},
		{name: "ShouldKeyHoje", have: "hoje", expected: []string{"AJ", "RJ"}},
		{name: "ShouldKeyHouse", have: "house", expected: []string{"AS", "RS"}},
		{name: "ShouldFallBackToWordForTripleH", have: "hhh", expected: []string{"hhh"}},
		// The epenthetic vowel before s and a consonant, and the dropped final r.
		{name: "ShouldKeyIskank", have: "iskank", expected: []string{"SKNK"}},
		{name: "ShouldKeyEsse", have: "esse", expected: []string{"AS"}},
		{name: "ShouldKeyVoltar", have: "voltar", expected: []string{"VT", "VLTR"}},
		{name: "ShouldKeyDor", have: "dor", expected: []string{"DR"}},
		// Pass-through and fallback.
		{name: "ShouldPassDigitsThrough", have: "1984", expected: []string{"1984"}},
		{name: "ShouldFallBackToEmptyWord", expected: []string{""}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Encoder{}.Keys(nil, tc.have))
		})
	}

	assert.Equal(t, []string{"x", "XX"}, Encoder{}.Keys([]string{"x"}, "xuxa"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("pt-BR"))
}

// TestHomophones pins the Brazilian Portuguese spellings of one sound, and the ways speech recognition writes them, that English Double Metaphone keeps apart.
func TestHomophones(t *testing.T) {
	testCases := []struct {
		name  string
		word  string
		other string
	}{
		{name: "ShouldShareKeyBetweenChuchaAndXuxa", word: "chucha", other: "xuxa"},
		{name: "ShouldShareKeyBetweenXicoAndChico", word: "xico", other: "chico"},
		{name: "ShouldShareKeyBetweenSeichasAndSeixas", word: "seichas", other: "seixas"},
		{name: "ShouldShareKeyBetweenGauAndGal", word: "gau", other: "gal"},
		{name: "ShouldShareKeyBetweenBrasiuAndBrasil", word: "brasiu", other: "brasil"},
		{name: "ShouldShareKeyBetweenTonAndTom", word: "ton", other: "tom"},
		{name: "ShouldShareKeyBetweenJobinAndJobim", word: "jobin", other: "jobim"},
		{name: "ShouldShareKeyBetweenTinAndTim", word: "tin", other: "tim"},
		{name: "ShouldShareKeyBetweenChimAndTim", word: "chim", other: "tim"},
		{name: "ShouldShareKeyBetweenChitasAndTitas", word: "chitas", other: "titas"},
		{name: "ShouldShareKeyBetweenDiavanAndDjavan", word: "diavan", other: "djavan"},
		{name: "ShouldShareKeyBetweenJavanAndDjavan", word: "javan", other: "djavan"},
		{name: "ShouldShareKeyBetweenDijavanAndDjavan", word: "dijavan", other: "djavan"},
		{name: "ShouldShareKeyBetweenMariguelaAndMarighella", word: "mariguela", other: "marighella"},
		{name: "ShouldShareKeyBetweenIskankAndSkank", word: "iskank", other: "skank"},
		{name: "ShouldShareKeyBetweenDjokerAndJoker", word: "djoker", other: "joker"},
		{name: "ShouldShareKeyBetweenCoudpleiAndColdplay", word: "coudplei", other: "coldplay"},
		{name: "ShouldShareKeyBetweenElitiAndElite", word: "eliti", other: "elite"},
		{name: "ShouldShareKeyBetweenCaroselAndCarrossel", word: "carosel", other: "carrossel"},
		{name: "ShouldShareKeyBetweenRouseAndHouse", word: "rouse", other: "house"},
		{name: "ShouldShareKeyBetweenDeAndThe", word: "de", other: "the"},
		{name: "ShouldShareKeyBetweenTronesAndThrones", word: "trones", other: "thrones"},
		{name: "ShouldShareKeyBetweenVoltaAndVoltar", word: "volta", other: "voltar"},
		{name: "ShouldShareKeyBetweenRegiaoAndRegiam", word: "regiao", other: "regiam"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := Encoder{}.Keys(nil, tc.word), Encoder{}.Keys(nil, tc.other)
			assert.True(t, shareKey(a, b), "%s %q and %s %q should share a key", tc.word, a, tc.other, b)
		})
	}
}

// TestDistinct pins sounds Brazilian Portuguese keeps apart, which a looser encoder would merge.
func TestDistinct(t *testing.T) {
	testCases := []struct {
		name  string
		word  string
		other string
	}{
		{name: "ShouldKeepVilaAndBilaApart", word: "vila", other: "bila"},
		{name: "ShouldKeepFacaAndVacaApart", word: "faca", other: "vaca"},
		{name: "ShouldKeepPatoAndBatoApart", word: "pato", other: "bato"},
		{name: "ShouldKeepCasaAndGasaApart", word: "casa", other: "gasa"},
		{name: "ShouldKeepFilhosAndFiosApart", word: "filhos", other: "fios"},
		{name: "ShouldKeepMalaAndManaApart", word: "mala", other: "mana"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := Encoder{}.Keys(nil, tc.word), Encoder{}.Keys(nil, tc.other)
			assert.False(t, shareKey(a, b), "%s %q and %s %q should not share a key", tc.word, a, tc.other, b)
		})
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

	testCases := []struct {
		name     string
		have     string
		expected float64
	}{
		{name: "ShouldAllocateTwiceForCidade", have: "cidade", expected: 2},
		{name: "ShouldAllocateOnceForXuxa", have: "xuxa", expected: 1},
		{name: "ShouldNotAllocateForDigits", have: "1984"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], tc.have) })
			assert.InDelta(t, tc.expected, got, 0, "only the returned keys allocate")
		})
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
