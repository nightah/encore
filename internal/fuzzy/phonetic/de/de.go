package de

import "github.com/nightah/encore/internal/fuzzy/phonetic"

func init() { phonetic.Register("de", Encoder{}) }

// Keys appends the word's Kölner Phonetik code, or the word itself when it has no code.
func (Encoder) Keys(dst []string, word string) []string {
	if phonetic.IsDigits(word) {
		return append(dst, word)
	}

	var buf [stackCode]byte
	if code := cologne(buf[:0], word); len(code) > 0 {
		return append(dst, string(code))
	}

	return append(dst, word)
}
