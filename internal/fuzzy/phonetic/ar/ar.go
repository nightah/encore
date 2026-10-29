package ar

import (
	"slices"
	"strings"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	"github.com/nightah/encore/internal/fuzzy/phonetic/en"
)

func init() { phonetic.Register("ar", Encoder{}) }

// Keys appends Double Metaphone's primary key for the word, the key with the Arabic mergers (j as k, th as s, kh as h), the key with q as a glottal stop when the word has a q, and Double Metaphone's other keys, at most three in all and without repeats.
func (Encoder) Keys(dst []string, word string) []string {
	if phonetic.IsDigits(word) {
		return append(dst, word)
	}

	var buf [maxKeys]string

	dm := en.Encoder{}.Keys(buf[:0], word)
	if dm[0] == word {
		// Double Metaphone found nothing to key.
		return append(dst, word)
	}

	start := len(dst)
	dst = appendKey(dst, start, dm[0])

	merged := dm[0]
	if strings.Contains(word, "kh") {
		merged = primary(strings.ReplaceAll(word, "kh", "h"))
	}

	dst = appendKey(dst, start, mergeCodes(merged))

	if strings.IndexByte(word, 'q') >= 0 {
		dst = appendKey(dst, start, primary(strings.ReplaceAll(word, "q", "a")))
	}

	for _, k := range dm[1:] {
		dst = appendKey(dst, start, k)
	}

	return dst
}

// primary is Double Metaphone's primary key for word, or "" when it has none.
func primary(word string) string {
	var buf [maxKeys]string

	k := (en.Encoder{}).Keys(buf[:0], word)[0]
	if k == word {
		return ""
	}

	return k
}

// mergeCodes keys a Double Metaphone key's J as K and its θ (0) as S.
func mergeCodes(k string) string {
	if !strings.ContainsAny(k, "J0") {
		return k
	}

	b := []byte(k)
	for i, c := range b {
		switch c {
		case 'J':
			b[i] = 'K'
		case '0':
			b[i] = 'S'
		}
	}

	return string(b)
}

// appendKey appends k to dst unless it is empty, already among the keys from start, or there are maxKeys of them.
func appendKey(dst []string, start int, k string) []string {
	if k == "" || len(dst)-start >= maxKeys || slices.Contains(dst[start:], k) {
		return dst
	}

	return append(dst, k)
}
