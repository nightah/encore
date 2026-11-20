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
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		// Silent endings: plural marks, final t/d/p, g/c after n, -er. The second key sounds the final consonant but never a plural mark.
		{name: "ShouldKeyBronzes", have: "bronzes", expected: []string{"PRNS"}},
		{name: "ShouldKeyBronzer", have: "bronzer", expected: []string{"PRNS", "PRNSR"}},
		{name: "ShouldKeyCent", have: "cent", expected: []string{"SN", "SNT"}},
		{name: "ShouldKeySang", have: "sang", expected: []string{"SN", "SNK"}},
		{name: "ShouldKeySans", have: "sans", expected: []string{"SN"}},
		{name: "ShouldKeyTaxis", have: "taxis", expected: []string{"TKS"}},
		{name: "ShouldKeyFlorette", have: "florette", expected: []string{"FLRT"}},
		{name: "ShouldKeyFloret", have: "floret", expected: []string{"FLR", "FLRT"}},
		{name: "ShouldKeyAllez", have: "allez", expected: []string{"AL"}},
		{name: "ShouldKeyAliases", have: "aliases", expected: []string{"ALS"}},
		{name: "ShouldKeyPieds", have: "pieds", expected: []string{"P", "PT"}},
		// An English past participle keeps its d.
		{name: "ShouldKeyPaused", have: "paused", expected: []string{"PST"}},
		{name: "ShouldKeyImported", have: "imported", expected: []string{"ANPRTT"}},
		// Short words keep their last consonant.
		{name: "ShouldKeyDit", have: "dit", expected: []string{"TT"}},
		{name: "ShouldKeyMer", have: "mer", expected: []string{"MR"}},
		// Silent h, and a leading vowel sound becomes A.
		{name: "ShouldKeyHallyday", have: "hallyday", expected: []string{"ALT"}},
		{name: "ShouldKeyHaine", have: "haine", expected: []string{"AN"}},
		// Digraphs and soft c/g.
		{name: "ShouldKeyChtis", have: "chtis", expected: []string{"XT"}},
		{name: "ShouldKeySchtroumpf", have: "schtroumpf", expected: []string{"XTRNPF"}},
		{name: "ShouldKeyChrist", have: "christ", expected: []string{"KRS", "KRST"}},
		{name: "ShouldKeyPhilippe", have: "philippe", expected: []string{"FLP"}},
		{name: "ShouldKeyAgneau", have: "agneau", expected: []string{"AN"}},
		{name: "ShouldKeyGuerre", have: "guerre", expected: []string{"KR"}},
		{name: "ShouldKeyGenou", have: "genou", expected: []string{"JN"}},
		{name: "ShouldKeyArnaqueur", have: "arnaqueur", expected: []string{"ARNKR"}},
		{name: "ShouldKeyAccent", have: "accent", expected: []string{"AKSN", "AKSNT"}},
		{name: "ShouldKeyNation", have: "nation", expected: []string{"NSN"}},
		{name: "ShouldKeyQuestion", have: "question", expected: []string{"KSTN"}},
		{name: "ShouldKeyChamp", have: "champ", expected: []string{"XN", "XNP"}},
		{name: "ShouldKeyXavier", have: "xavier", expected: []string{"SF", "SFR"}},
		{name: "ShouldKeyDoigts", have: "doigts", expected: []string{"T", "TKT"}},
		// A w after a velar is a glide.
		{name: "ShouldKeyKwe", have: "kwe", expected: []string{"K"}},
		{name: "ShouldKeyGwen", have: "gwen", expected: []string{"KN"}},
		{name: "ShouldKeyWagon", have: "wagon", expected: []string{"FKN"}},
		// Pass-through and fallback.
		{name: "ShouldPassDigitsThrough", have: "2049", expected: []string{"2049"}},
		{name: "ShouldFallBackToWordForLoneH", have: "h", expected: []string{"h"}},
		{name: "ShouldFallBackToEmptyWord", expected: []string{""}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Encoder{}.Keys(nil, tc.have))
		})
	}

	assert.Equal(t, []string{"x", "KML", "KMLT"}, Encoder{}.Keys([]string{"x"}, "camelot"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("fr-CA"))
}

// TestHomophones pins the French spellings of one sound that English Double Metaphone keeps apart.
func TestHomophones(t *testing.T) {
	testCases := []struct {
		name  string
		word  string
		other string
	}{
		{name: "ShouldShareKeyBetweenBronzesAndBronzer", word: "bronzes", other: "bronzer"},
		{name: "ShouldShareKeyBetweenCentAndSang", word: "cent", other: "sang"},
		{name: "ShouldShareKeyBetweenSangAndSans", word: "sang", other: "sans"},
		{name: "ShouldShareKeyBetweenVisiteursAndVisiteur", word: "visiteurs", other: "visiteur"},
		{name: "ShouldShareKeyBetweenHallydayAndAlliday", word: "hallyday", other: "alliday"},
		{name: "ShouldShareKeyBetweenHaineAndAine", word: "haine", other: "aine"},
		{name: "ShouldShareKeyBetweenCamelotAndKaamelott", word: "camelot", other: "kaamelott"},
		{name: "ShouldShareKeyBetweenFloretAndFlorette", word: "floret", other: "florette"},
		{name: "ShouldShareKeyBetweenEngrenagesAndEngrenage", word: "engrenages", other: "engrenage"},
		{name: "ShouldShareKeyBetweenChtisAndShtis", word: "chtis", other: "shtis"},
		{name: "ShouldShareKeyBetweenQuoiAndKwe", word: "quoi", other: "kwe"},
		{name: "ShouldShareKeyBetweenDoigtsAndDoes", word: "doigts", other: "does"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := Encoder{}.Keys(nil, tc.word), Encoder{}.Keys(nil, tc.other)
			assert.True(t, shareKey(a, b), "%s %q and %s %q should share a key", tc.word, a, tc.other, b)
		})
	}
}

// TestDistinct pins pairs that sound different in French but that sounding a plural mark, or dropping a sounded d, would merge.
func TestDistinct(t *testing.T) {
	testCases := []struct {
		name  string
		word  string
		other string
	}{
		{name: "ShouldKeepAllezAndAliasesApart", word: "allez", other: "aliases"},
		{name: "ShouldKeepPauseAndPausedApart", word: "pause", other: "paused"},
		{name: "ShouldKeepImporteeAndImportedApart", word: "importee", other: "imported"},
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

	testCases := []struct {
		name     string
		have     string
		expected float64
	}{
		{name: "ShouldAllocateTwiceForChrist", have: "christ", expected: 2},
		{name: "ShouldAllocateOnceForKaamelott", have: "kaamelott", expected: 1},
		{name: "ShouldNotAllocateForDigits", have: "2049"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], tc.have) })
			assert.InDelta(t, tc.expected, got, 0, "only the returned keys allocate")
		})
	}
}

// benchWords are French title words.
var benchWords = []string{
	"fabuleux", "destin", "amelie", "poulain", "intouchables", "bronzes", "font", "ski", "diner", "cons", "visiteurs",
	"bienvenue", "chez", "chtis", "choristes", "haine", "grande", "vadrouille", "asterix", "obelix", "mission",
	"cleopatre", "cite", "peur", "tontons", "flingueurs", "grand", "bleu", "caire", "espions", "taxi", "cyrano",
	"bergerac", "florette", "sources", "femmes", "arnacoeur", "petit", "nicolas", "kaamelott",
}

// BenchmarkKeys compares the French encoder with the English Double Metaphone that French titles would otherwise fall back to.
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
