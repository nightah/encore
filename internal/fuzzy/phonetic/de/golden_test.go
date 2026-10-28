package de

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

// libraryDigest is exhaustiveDigest of the library's codes and keys, captured from github.com/f1monkey/phonetic v1.4.2 before it was removed.
const libraryDigest = "535ff632bec29bba0ee7140242e42b577d5230d9a2c7365f198b75b1c6fbe7ff"

// libraryWords are the words testdata/library.txt pins: random words over all letters and the German extras, over the letter groups the rules single out, mixed-case text with digits and punctuation, arbitrary bytes including invalid UTF-8, and long words. The generator is seeded, so the list never changes.
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

	letters := split("abcdefghijklmnopqrstuvwxyzäöüßéè")
	groups := append(split("abcdefghijklmnopqrstuvwxyz"), "sch", "ch", "chs", "cks", "ph", "dt", "tz", "ck", "cz", "qu", "ß")
	text := split("ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÜabcdefghijklmnopqrstuvwxyz0123456789 '-")

	words := make([]string, 0, 1603)
	for range 400 {
		b := make([]byte, r.IntN(33))
		for i := range b {
			b[i] = byte(r.UintN(256))
		}

		words = append(words, from(letters, 0, 24), from(groups, 1, 8), from(text, 0, 30), string(b))
	}

	return append(words, strings.Repeat("rechtsanwalt", 10), strings.Repeat("ß", 40), strings.Repeat("ab", 100))
}

// exhaustiveDigest hashes code(word) for every word of up to four letters from a-z and the German extras, in order.
func exhaustiveDigest(code func(string) string) string {
	h := sha256.New()
	alphabet := []rune("abcdefghijklmnopqrstuvwxyzäöüß")
	word := make([]rune, 0, 4)

	var walk func()
	walk = func() {
		w := string(word)
		h.Write([]byte(w + "\x00" + code(w) + "\n"))

		if len(word) == cap(word) {
			return
		}

		for _, r := range alphabet {
			word = append(word, r)
			walk()
			word = word[:len(word)-1]
		}
	}

	walk()

	return hex.EncodeToString(h.Sum(nil))
}

// portCode is what exhaustiveDigest hashes for the port: the code and the keys.
func portCode(w string) string {
	return string(cologne(nil, w)) + "\x00" + strings.Join(Encoder{}.Keys(nil, w), " ")
}

// TestLibraryExhaustive checks every word of up to four letters from a-z and the German extras against the library's codes and keys through their digest.
func TestLibraryExhaustive(t *testing.T) {
	assert.Equal(t, libraryDigest, exhaustiveDigest(portCode))
}

// TestLibraryGolden checks the port against the library's codes and keys in testdata/library.txt: per line, tab-separated and Go-quoted, a word, its code and its key.
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
		require.Len(t, fields, 3, line)

		want := make([]string, len(fields))
		for i, f := range fields {
			want[i], err = strconv.Unquote(f)
			require.NoError(t, err, line)
		}

		word := want[0]
		got = append(got, word)

		assert.Equal(t, want[1], string(cologne(nil, word)), "code of %q", word)
		assert.Equal(t, want[2:], Encoder{}.Keys(nil, word), "keys of %q", word)
	}

	require.NoError(t, sc.Err())
	assert.True(t, slices.Equal(libraryWords(), got), "testdata/library.txt holds libraryWords in order")
}
