package en

import (
	"slices"
	"strings"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
)

func init() { phonetic.Register("en", Encoder{}) }

// Keys appends the Double Metaphone primary key, the alternate when it differs and, for a word ending in a vowel and "r", the primary without its final R, so non-rhotic "Peppa" and "pepper" meet.
func (Encoder) Keys(dst []string, word string) []string {
	if phonetic.IsDigits(word) {
		return append(dst, word)
	}

	start := len(dst)

	p, a := doubleMetaphone(word)
	if p != "" {
		dst = append(dst, p)
	}

	if a != "" && a != p {
		dst = append(dst, a)
	}

	if len(dst) == start {
		return append(dst, word)
	}

	first := dst[start]
	if n := len(word); len(first) > 1 && first[len(first)-1] == 'R' && n > 2 && word[n-1] == 'r' && strings.IndexByte("aeiouy", word[n-2]) >= 0 {
		if k := first[:len(first)-1]; !slices.Contains(dst[start:], k) {
			dst = append(dst, k)
		}
	}

	return dst
}
