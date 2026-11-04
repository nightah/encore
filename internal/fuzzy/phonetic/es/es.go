package es

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	"github.com/nightah/encore/internal/fuzzy/phonetic/internal/skeleton"
)

func init() { phonetic.Register("es", Encoder{}) }

// Keys appends the key of the word as Spanish says it, with a final s or d left out (southern speakers aspirate or drop them: "iglesia(s)", "Madri(d)"), and, when different, the key with every letter sounded. A word starting with an h or a w before a vowel also gets the key of the sound a recogniser may have written that way (see soundedOnset).
func (Encoder) Keys(dst []string, word string) []string {
	if phonetic.IsDigits(word) {
		return append(dst, word)
	}

	// At least three letters stay, so short words ("las", "dos") keep their final consonant.
	n := len(word)
	if n > 3 && (word[n-1] == 's' || word[n-1] == 'd') {
		n--
	}

	var primary, full skeleton.Key

	encode(&primary, word[:n])
	encode(&full, word)

	start := len(dst)
	dst = skeleton.Append(dst, word, &primary, &full)

	if onset := soundedOnset(word); onset != "" {
		// The respelt word is built on the stack: only a key that is new allocates.
		var buf [skeleton.MaxLen]byte

		var alt skeleton.Key

		encode(&alt, string(append(append(buf[:0], onset...), word[1:n]...)))

		if k := alt.Bytes(); !slices.ContainsFunc(dst[start:], func(d string) bool { return d == string(k) }) {
			dst = append(dst, string(k))
		}
	}

	return dst
}

// soundedOnset returns what a word's first letter stands for when a recogniser may have written it for another sound, or "": the soft jota of Mexican, Caribbean and US speech for an h before a vowel ("hosé" for "José"), and the gu of güe and gua for a w ("weros" for "güeros").
func soundedOnset(word string) string {
	if len(word) < 2 || !isVowel(word[1]) {
		return ""
	}

	switch word[0] {
	case 'h':
		return "j"
	case 'w':
		return "gu"
	}

	return ""
}

func isVowel(c byte) bool { return strings.IndexByte("aeiou", c) >= 0 }

// soft reports whether c softens a preceding c or g ("cielo", "gente").
func soft(c byte) bool { return c == 'e' || c == 'i' }

//nolint:gocyclo,cyclop // One case per letter keeps the rules readable against the table in doc.go.
func encode(k *skeleton.Key, w string) {
	at := func(i int) byte { return skeleton.At(w, i) }

	for i := 0; i < len(w); {
		c := w[i]
		i++

		switch {
		case isVowel(c), c == 'y' && !isVowel(at(i)):
			k.Vowel()
		case c == 'c':
			switch {
			case at(i) == 'h':
				k.Emit('X')
				i++
			case at(i) == 'c' && soft(at(i+1)):
				k.Emit('K', 'S')
				i++
			case soft(at(i)):
				k.Emit('S')
			default:
				k.Emit('K')
			}
		case c == 'g':
			switch {
			case at(i) == 'u' && soft(at(i+1)):
				k.Emit('G')
				i++
			case soft(at(i)):
				k.Emit('J')
			default:
				k.Emit('G')
			}
		case c == 'q':
			k.Emit('K')

			if at(i) == 'u' {
				i++
			}
		case c == 'l' && at(i) == 'l', c == 'y':
			// Yeísmo: ll and a consonant y are one sound.
			k.Emit('Y')

			if c == 'l' {
				i++
			}
		case c == 'n' && strings.IndexByte("bvpm", at(i)) >= 0:
			// n assimilates to m before a labial ("Bunbury" said "Bumbury").
			k.Emit('M')
		case c == 'x':
			if i == 1 {
				k.Emit('S')
			} else {
				k.Emit('K', 'S')
			}
		case c < utf8.RuneSelf && consonants[c] != 0:
			k.Emit(consonants[c])
		}
	}
}

// consonants maps the letters with a single sound; h is silent and absent, and b, v and w are one sound (betacism).
var consonants = [128]byte{
	'b': 'B', 'd': 'T', 'f': 'F', 'j': 'J', 'k': 'K', 'l': 'L', 'm': 'M', 'n': 'N', 'p': 'P',
	'r': 'R', 's': 'S', 't': 'T', 'v': 'B', 'w': 'B', 'z': 'S',
}
