package en

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libraryDigest is exhaustiveDigest of the library's codes and keys, captured from github.com/PhonoGrams/double_metaphone v0.1.0.
const libraryDigest = "e08ae020a4d3dac972d533ba95f18448905d931097cfbd35ab74bdaed5a875af"

// libraryWords are the words testdata/library.txt pins: random words over all letters, over the letters with context rules and over the digraphs and names the rules single out, mixed-case text with digits and punctuation, arbitrary bytes including invalid UTF-8, and long words. The generator is seeded, so the list never changes.
func libraryWords() []string {
	r := rand.New(rand.NewPCG(1, 2))

	from := func(alphabet []string, lo, hi int) string {
		var b strings.Builder
		for range lo + r.IntN(hi-lo+1) {
			b.WriteString(alphabet[r.IntN(len(alphabet))])
		}

		return b.String()
	}

	split := func(s string) []string { return strings.Split(s, "") }

	letters := split("abcdefghijklmnopqrstuvwxyzçñäöüß")
	ruly := split(rulyAlphabet)
	digraphs := append(split("abcdefghijklmnopqrstuvwxyz"), "sch", "ch", "gh", "gn", "cc", "ll", "th", "wicz", "witz", "cia", "tion", "ough", "mb", "ps", "wr", "kn", "jose", "van", "von", "san", "sugar", "caesar")
	text := split("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 '-")

	words := make([]string, 0, 2004)
	for range 400 {
		b := make([]byte, r.IntN(33))
		for i := range b {
			b[i] = byte(r.UintN(256))
		}

		words = append(words, from(letters, 0, 24), from(ruly, 1, 12), from(digraphs, 1, 6), from(text, 0, 30), string(b))
	}

	return append(words, strings.Repeat("schwarzenegger", 8), strings.Repeat("thompson", 20), strings.Repeat("a", 200), strings.Repeat("ç", 60))
}

// exhaustiveDigest hashes code(word) for every lowercase word of up to four letters, in order.
func exhaustiveDigest(code func(string) string) string {
	h := sha256.New()

	var word [4]byte

	var walk func(n int)
	walk = func(n int) {
		w := string(word[:n])
		h.Write([]byte(w + "\x00" + code(w) + "\n"))

		if n == len(word) {
			return
		}

		for c := byte('a'); c <= 'z'; c++ {
			word[n] = c
			walk(n + 1)
		}
	}

	walk(0)

	return hex.EncodeToString(h.Sum(nil))
}

// portCode is what exhaustiveDigest hashes for the port: both codes and the keys.
func portCode(w string) string {
	p, a := doubleMetaphone(w)

	return p + "\x00" + a + "\x00" + strings.Join(Encoder{}.Keys(nil, w), " ")
}

// TestLibraryExhaustive checks every lowercase word of up to four letters against the library's codes and keys through their digest.
func TestLibraryExhaustive(t *testing.T) {
	assert.Equal(t, libraryDigest, exhaustiveDigest(portCode))
}

// TestLibraryGolden checks the encoder against the library's codes and keys in testdata/library.txt: per line, tab-separated and Go-quoted, a word, its primary and alternate codes and its keys.
func TestLibraryGolden(t *testing.T) {
	f, err := os.Open("testdata/library.txt")
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	var got []string

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, "\t")
		require.GreaterOrEqual(t, len(fields), 4, line)

		want := make([]string, len(fields))
		for i, f := range fields {
			want[i], err = strconv.Unquote(f)
			require.NoError(t, err, line)
		}

		word := want[0]
		got = append(got, word)

		p, a := doubleMetaphone(word)
		assert.Equal(t, want[1], p, "primary of %q", word)
		assert.Equal(t, want[2], a, "alternate of %q", word)
		assert.Equal(t, want[3:], Encoder{}.Keys(nil, word), "keys of %q", word)
	}

	require.NoError(t, sc.Err())
	assert.True(t, slices.Equal(libraryWords(), got), "testdata/library.txt holds libraryWords in order")
}
