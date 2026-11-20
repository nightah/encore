package ar

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
		// Double Metaphone alone: the merged key is the same.
		{name: "ShouldKeyTitanic", have: "titanic", expected: []string{"TTNK"}},
		{name: "ShouldKeyTaytank", have: "taytank", expected: []string{"TTNK"}},
		{name: "ShouldKeyKlsum", have: "klsum", expected: []string{"KLSM"}},
		// j as k, after Double Metaphone's primary key and before its others.
		{name: "ShouldKeyJim", have: "jim", expected: []string{"JM", "KM", "AM"}},
		{name: "ShouldKeyYjabl", have: "yjabl", expected: []string{"AJPL", "AKPL", "AHPL"}},
		// th as s, with Double Metaphone's t reading kept while there is room.
		{name: "ShouldKeyKlthum", have: "klthum", expected: []string{"KL0M", "KLSM", "KLTM"}},
		{name: "ShouldKeyThib", have: "thib", expected: []string{"0P", "SP", "TP"}},
		// kh as h.
		{name: "ShouldKeyAkhtiar", have: "akhtiar", expected: []string{"AKXR", "AXR", "AKX"}},
		{name: "ShouldKeyKhaled", have: "khaled", expected: []string{"KLT", "HLT"}},
		// q as a glottal stop.
		{name: "ShouldKeyQsr", have: "qsr", expected: []string{"KSR", "ASR"}},
		{name: "ShouldKeyQlbi", have: "qlbi", expected: []string{"KLP", "ALP"}},
		{name: "ShouldKeyYqabl", have: "yqabl", expected: []string{"AKPL", "APL"}},
		// Pass-through and fallback.
		{name: "ShouldPassDigitsThrough", have: "211", expected: []string{"211"}},
		{name: "ShouldFallBackToWordForLoneH", have: "h", expected: []string{"h"}},
		{name: "ShouldFallBackToEmptyWord", expected: []string{""}},
		// A merged reading with nothing to key adds no key.
		{name: "ShouldAddNoKeyForMergedReadingWithNothingToKey", have: "kh", expected: []string{"K"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Encoder{}.Keys(nil, tc.have))
		})
	}

	assert.Equal(t, []string{"x", "TTNK"}, Encoder{}.Keys([]string{"x"}, "titanic"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("ar-SA"))
}

// TestHomophones pins the Arabic pronunciations of one word, as the matcher writes them in Latin letters, and the English titles asked for in Arabic letters, that share a key.
func TestHomophones(t *testing.T) {
	testCases := []struct {
		name  string
		word  string
		other string
	}{
		// ق as g and ج as g.
		{name: "ShouldShareKeyBetweenYqablAndYjabl", word: "yqabl", other: "yjabl"},
		{name: "ShouldShareKeyBetweenQimAndJim", word: "qim", other: "jim"},
		{name: "ShouldShareKeyBetweenJimAndGame", word: "jim", other: "game"},
		{name: "ShouldShareKeyBetweenJudfazrAndGodfather", word: "judfazr", other: "godfather"},
		// Interdentals as stops and as sibilants.
		{name: "ShouldShareKeyBetweenKlthumAndKltum", word: "klthum", other: "kltum"},
		{name: "ShouldShareKeyBetweenKlthumAndKlsum", word: "klthum", other: "klsum"},
		{name: "ShouldShareKeyBetweenKathmAndKazm", word: "kathm", other: "kazm"},
		{name: "ShouldShareKeyBetweenThibAndDib", word: "thib", other: "dib"},
		// خ heard as ح.
		{name: "ShouldShareKeyBetweenAkhtiarAndAhtiar", word: "akhtiar", other: "ahtiar"},
		// ق as a glottal stop.
		{name: "ShouldShareKeyBetweenQsrAndAsr", word: "qsr", other: "asr"},
		{name: "ShouldShareKeyBetweenQlbiAndAlbi", word: "qlbi", other: "albi"},
		// English titles in Arabic letters: no p, v, e or o.
		{name: "ShouldShareKeyBetweenTaytankAndTitanic", word: "taytank", other: "titanic"},
		{name: "ShouldShareKeyBetweenBabilAndPapel", word: "babil", other: "papel"},
		{name: "ShouldShareKeyBetweenAfnjrzAndAvengers", word: "afnjrz", other: "avengers"},
		{name: "ShouldShareKeyBetweenAnsbshnAndInception", word: "ansbshn", other: "inception"},
		{name: "ShouldShareKeyBetweenFruznAndFrozen", word: "fruzn", other: "frozen"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := Encoder{}.Keys(nil, tc.word), Encoder{}.Keys(nil, tc.other)
			assert.True(t, shareKey(a, b), "%s %q and %s %q should share a key", tc.word, a, tc.other, b)
		})
	}
}

// TestDistinct pins words the mergers must keep apart.
func TestDistinct(t *testing.T) {
	testCases := []struct {
		name  string
		word  string
		other string
	}{
		{name: "ShouldKeepSamAndTamApart", word: "sam", other: "tam"},
		{name: "ShouldKeepJmlAndHmlApart", word: "jml", other: "hml"},
		{name: "ShouldKeepBaytAndZaytApart", word: "bayt", other: "zayt"},
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
		word := rapid.OneOf(rapid.String(), rapid.StringMatching(`[a-z]{0,20}`), rapid.StringMatching(`[a-z0-9]{0,20}`), rapid.StringMatching(`(kh|th|q|j|[a-z]){0,10}`)).Draw(t, "word")
		keys := Encoder{}.Keys(nil, word)

		require.NotEmpty(t, keys)
		require.LessOrEqual(t, len(keys), maxKeys)
		require.Equal(t, keys, Encoder{}.Keys(nil, word), "deterministic")

		for i, k := range keys {
			require.False(t, slices.Contains(keys[:i], k), "duplicate key %q", k)

			if k == word {
				continue
			}

			require.NotEmpty(t, k)

			for j := range len(k) {
				require.True(t, k[j] >= 'A' && k[j] <= 'Z' || k[j] == '0', "key %q", k)
			}
		}

		if phonetic.IsDigits(word) {
			require.Equal(t, []string{word}, keys)
		}

		// Double Metaphone's primary key always leads.
		require.Equal(t, en.Encoder{}.Keys(nil, word)[0], keys[0])
	})
}

func TestKeysAllocations(t *testing.T) {
	dst := make([]string, 0, maxKeys)

	testCases := []struct {
		name     string
		have     string
		expected float64
	}{
		{name: "ShouldAllocateOnceForTitanic", have: "titanic", expected: 1},
		{name: "ShouldNotAllocateForDigits", have: "211"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], tc.have) })
			assert.InDelta(t, tc.expected, got, 0, "only the returned keys allocate")
		})
	}
}

// benchWords are title words as the matcher writes Arabic in Latin letters, and English titles.
var benchWords = []string{
	"wjda", "thib", "bab", "hdid", "arhab", "kbab", "amara", "yaqubian", "lmbi", "nathr", "saidi", "jamaa", "amrikia",
	"hmam", "amstrdam", "fil", "azrq", "kfrnahum", "skr", "bnat", "msir", "ard", "msamir", "brka", "yqabl", "shms",
	"maarf", "risala", "omr", "mkhtar", "titanic", "avengers", "endgame", "inception", "godfather", "frozen", "joker",
	"interstellar", "breaking", "thrones",
}

// BenchmarkKeys compares the Arabic encoder with the English Double Metaphone it builds on.
func BenchmarkKeys(b *testing.B) {
	for _, impl := range []struct {
		name string
		enc  phonetic.Encoder
	}{{"ar", Encoder{}}, {"en", en.Encoder{}}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			dst := make([]string, 0, maxKeys)

			b.ReportAllocs()

			for i := range b.N {
				dst = impl.enc.Keys(dst[:0], benchWords[i%len(benchWords)])
			}
		})
	}
}
