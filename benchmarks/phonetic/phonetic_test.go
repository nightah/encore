package phonetic

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	dm "github.com/PhonoGrams/double_metaphone"
	"github.com/f1monkey/phonetic/cologne"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	"github.com/nightah/encore/internal/fuzzy/phonetic/de"
	"github.com/nightah/encore/internal/fuzzy/phonetic/en"
)

// libraryEnglish keys a word as internal/core/match does, with PhonoGrams/double_metaphone in place of the in-tree encoder: numbers are their own key; otherwise the primary and alternate codes, deduplicated, then the primary without a final R when the word ends in a vowel and r ("lover", "star"), which speakers often drop; the word itself when there is no code.
func libraryEnglish(tok string) []string {
	if phonetic.IsDigits(tok) {
		return []string{tok}
	}

	p, a := dm.EncodeUncapped(tok)

	keys := make([]string, 0, 3)
	for _, k := range []string{p, a} {
		if k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}

	if len(keys) == 0 {
		return []string{tok}
	}

	if len(keys[0]) > 1 && strings.HasSuffix(keys[0], "R") && len(tok) > 2 && tok[len(tok)-1] == 'r' && strings.IndexByte("aeiouy", tok[len(tok)-2]) >= 0 {
		if k := strings.TrimSuffix(keys[0], "R"); !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}

	return keys
}

var cologneEncoder = cologne.NewEncoder()

// libraryGerman keys a word as internal/core/match does, with f1monkey/phonetic's Kölner Phonetik in place of the in-tree encoder: numbers are their own key, otherwise the code, or the word itself when there is none.
func libraryGerman(tok string) []string {
	if phonetic.IsDigits(tok) {
		return []string{tok}
	}

	if k := cologneEncoder.Encode(tok); k != "" {
		return []string{k}
	}

	return []string{tok}
}

// fataler is what same needs from *testing.T and *rapid.T.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

func sameEnglish(t fataler, word string) {
	t.Helper()

	if got, want := (en.Encoder{}).Keys(nil, word), libraryEnglish(word); !slices.Equal(got, want) {
		t.Fatalf("en.Keys(%q) = %q; double_metaphone %q", word, got, want)
	}
}

func sameGerman(t fataler, word string) {
	t.Helper()

	if got, want := (de.Encoder{}).Keys(nil, word), libraryGerman(word); !slices.Equal(got, want) {
		t.Fatalf("de.Keys(%q) = %q; f1monkey/phonetic %q", word, got, want)
	}
}

// eachWord calls fn with every word of up to n letters from alphabet, the empty word included.
func eachWord(alphabet []rune, n int, fn func(string)) {
	word := make([]rune, 0, n)

	var walk func()
	walk = func() {
		fn(string(word))

		if len(word) == n {
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

// TestParityExhaustive compares every word of up to four letters: a to z for English, and with ä, ö, ü and ß for German.
func TestParityExhaustive(t *testing.T) {
	eachWord([]rune("abcdefghijklmnopqrstuvwxyz"), 4, func(w string) { sameEnglish(t, w) })
	eachWord([]rune("abcdefghijklmnopqrstuvwxyzäöüß"), 4, func(w string) { sameGerman(t, w) })
}

// genWord draws arbitrary strings, lowercase words with accented letters, the letter groups Double Metaphone and Kölner Phonetik single out, and mixed text with digits.
func genWord() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.String(),
		rapid.StringMatching(`[a-zçñäöüß]{0,24}`),
		rapid.StringMatching(`[aceghijlmnorstwxyzbckpsdgu]{1,12}`),
		rapid.StringMatching(`(sch|ch|gh|gn|cc|ll|th|wicz|witz|cia|tion|ough|mb|ps|wr|kn|jose|van|von|san|sugar|caesar|chs|cks|dt|pf|ph|qu|x|[a-z]){1,6}`),
		rapid.StringMatching(`[A-Za-z0-9 '-]{0,30}`),
	)
}

func TestParityGenerated(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		w := genWord().Draw(t, "word")
		sameEnglish(t, w)
		sameGerman(t, w)
	})
}

func TestParityInvalidUTF8(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		w := string(rapid.SliceOfN(rapid.Byte(), 0, 32).Draw(t, "bytes"))
		sameEnglish(t, w)
		sameGerman(t, w)
	})
}

func TestParityLong(t *testing.T) {
	for _, w := range []string{strings.Repeat("schwarzenegger", 8), strings.Repeat("thompson", 20), strings.Repeat("a", 200), strings.Repeat("ç", 60)} {
		sameEnglish(t, w)
	}

	for _, w := range []string{strings.Repeat("rechtsanwalt", 10), strings.Repeat("ß", 40), strings.Repeat("ab", 100)} {
		sameGerman(t, w)
	}
}

// corpusWords returns every distinct lowercase word in the matcher's ASR, phonetic and grown corpora: real titles in five languages and what speech recognition heard for them.
func corpusWords(tb testing.TB) []string {
	tb.Helper()

	files, err := filepath.Glob("../../internal/core/match/testdata/*.yaml")
	require.NoError(tb, err)

	for _, dir := range []string{"phonetic", "grown"} {
		more, err := filepath.Glob("../../internal/core/match/testdata/" + dir + "/*.yaml")
		require.NoError(tb, err)
		require.NotEmpty(tb, more, dir)

		files = append(files, more...)
	}

	seen := map[string]bool{}

	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(tb, err)

		for w := range strings.FieldsFuncSeq(strings.ToLower(string(raw)), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			seen[w] = true
		}
	}

	words := make([]string, 0, len(seen))
	for w := range seen {
		words = append(words, w)
	}

	slices.Sort(words)
	require.Greater(tb, len(words), 4000)

	return words
}

// TestParityCorpus compares every word of the matcher's test corpora.
func TestParityCorpus(t *testing.T) {
	for _, w := range corpusWords(t) {
		sameEnglish(t, w)
		sameGerman(t, w)
	}
}

// Title words of the kind the matcher indexes, as in the encoders' own benchmarks.
var (
	englishWords = []string{
		"star", "wars", "empire", "strikes", "back", "lord", "rings", "fellowship", "matrix", "reloaded", "harry", "potter",
		"philosophers", "stone", "shawshank", "redemption", "silence", "lambs", "inception", "interstellar", "seinfeld",
		"mandalorian", "schwarzenegger", "bluey", "peppa", "pig", "ocean", "eleven", "godfather", "dalmatians", "beyonce",
		"knowles", "kendrick", "thriller", "michael", "jackson", "chianti", "psychology", "witz", "gnocchi",
	}
	germanWords = []string{
		"das", "boot", "schuh", "manitu", "lenin", "lola", "rennt", "leben", "anderen", "brucke", "untergang", "unendliche",
		"geschichte", "wunder", "bern", "fack", "gohte", "keinohrhasen", "zweiohrkuken", "kommissar", "anfanger", "fruher",
		"stirbt", "westen", "nichts", "neues", "tatort", "sendung", "maus", "biene", "maja", "fragezeichen", "arzte",
		"fantastischen", "rammstein", "herbert", "gronemeyer", "feuerzangenbowle", "systemsprenger", "kokowaah",
	}
)

var sink []string

// BenchmarkKeys times keying one word, with the in-tree encoder appending to a reused slice as the matcher does, and with the library through libraryEnglish and libraryGerman.
func BenchmarkKeys(b *testing.B) {
	for _, tc := range []struct {
		lang   string
		words  []string
		intree phonetic.Encoder
		lib    func(string) []string
	}{
		{"en", englishWords, en.Encoder{}, libraryEnglish},
		{"de", germanWords, de.Encoder{}, libraryGerman},
	} {
		b.Run("lang="+tc.lang+"/impl=library", func(b *testing.B) {
			b.ReportAllocs()

			for i := range b.N {
				sink = tc.lib(tc.words[i%len(tc.words)])
			}
		})

		b.Run("lang="+tc.lang+"/impl=intree", func(b *testing.B) {
			dst := make([]string, 0, 3)

			b.ReportAllocs()

			for i := range b.N {
				dst = tc.intree.Keys(dst[:0], tc.words[i%len(tc.words)])
			}

			sink = dst
		})
	}
}
