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
	for _, tc := range []struct {
		word string
		want []string
	}{
		// Double Metaphone alone: the merged key is the same.
		{"titanic", []string{"TTNK"}},
		{"taytank", []string{"TTNK"}},
		{"klsum", []string{"KLSM"}},
		// j as k, after Double Metaphone's primary key and before its others.
		{"jim", []string{"JM", "KM", "AM"}},
		{"yjabl", []string{"AJPL", "AKPL", "AHPL"}},
		// th as s, with Double Metaphone's t reading kept while there is room.
		{"klthum", []string{"KL0M", "KLSM", "KLTM"}},
		{"thib", []string{"0P", "SP", "TP"}},
		// kh as h.
		{"akhtiar", []string{"AKXR", "AXR", "AKX"}},
		{"khaled", []string{"KLT", "HLT"}},
		// q as a glottal stop.
		{"qsr", []string{"KSR", "ASR"}},
		{"qlbi", []string{"KLP", "ALP"}},
		{"yqabl", []string{"AKPL", "APL"}},
		// Pass-through and fallback.
		{"211", []string{"211"}},
		{"h", []string{"h"}},
		{"", []string{""}},
		// A merged reading with nothing to key adds no key.
		{"kh", []string{"K"}},
	} {
		assert.Equal(t, tc.want, Encoder{}.Keys(nil, tc.word), tc.word)
	}

	assert.Equal(t, []string{"x", "TTNK"}, Encoder{}.Keys([]string{"x"}, "titanic"), "Keys appends to dst")
	assert.Equal(t, Encoder{}, phonetic.For("ar-SA"))
}

// TestHomophones pins the Arabic pronunciations of one word, as the matcher writes them in Latin letters, and the English titles asked for in Arabic letters, that share a key.
func TestHomophones(t *testing.T) {
	for _, pair := range [][2]string{
		// ق as g and ج as g.
		{"yqabl", "yjabl"},
		{"qim", "jim"},
		{"jim", "game"},
		{"judfazr", "godfather"},
		// Interdentals as stops and as sibilants.
		{"klthum", "kltum"},
		{"klthum", "klsum"},
		{"kathm", "kazm"},
		{"thib", "dib"},
		// خ heard as ح.
		{"akhtiar", "ahtiar"},
		// ق as a glottal stop.
		{"qsr", "asr"},
		{"qlbi", "albi"},
		// English titles in Arabic letters: no p, v, e or o.
		{"taytank", "titanic"},
		{"babil", "papel"},
		{"afnjrz", "avengers"},
		{"ansbshn", "inception"},
		{"fruzn", "frozen"},
	} {
		a, b := Encoder{}.Keys(nil, pair[0]), Encoder{}.Keys(nil, pair[1])
		assert.True(t, shareKey(a, b), "%s %q and %s %q should share a key", pair[0], a, pair[1], b)
	}
}

// TestDistinct pins words the mergers must keep apart.
func TestDistinct(t *testing.T) {
	for _, pair := range [][2]string{
		{"sam", "tam"},
		{"jml", "hml"},
		{"bayt", "zayt"},
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

	for word, want := range map[string]float64{"titanic": 1, "211": 0} {
		got := testing.AllocsPerRun(100, func() { dst = Encoder{}.Keys(dst[:0], word) })
		assert.InDelta(t, want, got, 0, "%s: only the returned keys allocate", word)
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
