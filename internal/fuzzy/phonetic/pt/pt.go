package pt

import (
	"strings"
	"unicode/utf8"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	"github.com/nightah/encore/internal/fuzzy/phonetic/internal/skeleton"
)

func init() { phonetic.Register("pt", Encoder{}) }

// Keys appends the key of the word as a Brazilian says it, with a final r left out ("voltá" for "voltar"), and, when different, the key of the word as it is spelled, which keeps d, t, a syllable-final l and an English h as written.
func (Encoder) Keys(dst []string, word string) []string {
	if phonetic.IsDigits(word) {
		return append(dst, word)
	}

	n := len(word)
	if n > minSounded && word[n-1] == 'r' {
		n--
	}

	var primary, full skeleton.Key

	encode(&primary, word[:n], true)
	encode(&full, word, false)

	return skeleton.Append(dst, word, &primary, &full)
}

func isVowel(c byte) bool { return strings.IndexByte("aeiouwy", c) >= 0 }

// soft reports whether c softens a preceding c or g ("cidade", "gente").
func soft(c byte) bool { return c == 'e' || c == 'i' || c == 'y' }

// finalE reports whether w[i] is an e that ends the word, alone or before a plural s: unstressed, it is said as i ("cidade", "mutantes").
func finalE(w string, i int) bool {
	return skeleton.At(w, i) == 'e' && (i+1 == len(w) || i+2 == len(w) && w[i+1] == 's')
}

// palatal reports whether a d or t followed by w[i] is said as the affricate of "djavan" and "tchau": before i, before a final e, and before the epenthetic i Brazilians sound after a consonant that ends a syllable or a word ("ritmo", "advogado", "internet"). Before r and l the consonants form a cluster ("trem", "atlas"), and h makes "th".
func palatal(w string, i int) bool {
	switch c := skeleton.At(w, i); c {
	case 'i', 'y', 0:
		return true
	case 'e':
		return finalE(w, i)
	default:
		return !isVowel(c) && strings.IndexByte("rlh", c) < 0
	}
}

// affricate emits code for a d or t followed by w[i] when it is palatal, or plain otherwise, and returns where encoding goes on: past an i between the affricate and a consonant, the epenthetic one, so "dijavan" meets "djavan".
func affricate(k *skeleton.Key, w string, i int, code, plain byte) int {
	if !palatal(w, i) {
		k.Emit(plain)

		return i
	}

	k.Emit(code)

	if skeleton.At(w, i) == 'i' && i+1 < len(w) && !isVowel(w[i+1]) {
		return i + 1
	}

	return i
}

// nasalEnd reports whether w[i:] is the "ao" or "aos" that ends a word, -ão with its tilde folded away.
func nasalEnd(w string, i int) bool {
	return strings.HasPrefix(w[i:], "ao") && (i+2 == len(w) || i+3 == len(w) && w[i+2] == 's')
}

// encode keys w as Brazilians say it (spoken) or as it is spelled.
//
//nolint:gocyclo,cyclop,funlen // One case per letter keeps the rules readable against the table in doc.go.
func encode(k *skeleton.Key, w string, spoken bool) {
	at := func(i int) byte { return skeleton.At(w, i) }

	for i := 0; i < len(w); {
		c := w[i]
		i++

		switch {
		case isVowel(c):
			switch {
			case i == 1 && (c == 'e' || c == 'i') && at(1) == 's' && at(2) != 0 && !isVowel(at(2)) && at(2) != 's':
				// The vowel Brazilians put before an s and a consonant ("iskank" for Skank) is no sound of the word's own.
			case nasalEnd(w, i-1):
				// -ão ends in the nasal of -am ("chefão", "falam").
				k.Vowel()
				k.Emit('N')

				i++
			default:
				k.Vowel()
			}
		case c == 'c':
			switch {
			case at(i) == 'h':
				k.Emit('X')

				i++
			case soft(at(i)):
				k.Emit('S')
			default:
				k.Emit('K')
			}
		case c == 't' && at(i) == 'h':
			// An English th: Brazilians say the voiced one of "the" as their d, as in "de", and the other as t ("thrones", "trones").
			if spoken {
				i = affricate(k, w, i+1, 'J', 'D')
			} else {
				k.Emit('T')

				i++
			}
		case c == 'd' && spoken:
			i = affricate(k, w, i, 'J', 'D')
		case c == 't' && spoken:
			i = affricate(k, w, i, 'X', 'T')
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
		case c == 'h':
			// Silent in Portuguese spelling; an English title's h is said as the Brazilian r ("rouse of the dragon").
			if !spoken && i == 1 && isVowel(at(i)) {
				k.Emit('R')
			}
		case c == 'l':
			switch {
			case at(i) == 'h':
				k.Emit('L')

				i++
			case spoken && !isVowel(at(i)):
				// A syllable-final l is said as u ("Brasil", "Gil").
				k.Vowel()
			default:
				k.Emit('L')
			}
		case c == 'm' || c == 'n':
			switch {
			case c == 'n' && at(i) == 'h':
				k.Emit('N')

				i++
			case !isVowel(at(i)):
				// m and n that end a syllable only nasalise the vowel, so "Jobim" meets "Jobin".
				k.Emit('N')
			case c == 'm':
				k.Emit('M')
			default:
				k.Emit('N')
			}
		case c == 'p' && at(i) == 'h':
			k.Emit('F')

			i++
		case c == 'q':
			k.Emit('K')

			if at(i) == 'u' {
				i++
			}
		case c == 's' && at(i) == 'h':
			k.Emit('X')

			i++
		case c == 'x':
			switch {
			case i == 1:
				k.Emit('X')
			case at(i-2) == 'e' && (i == 2 || !isVowel(at(i))):
				// "exame", "texto", "extra".
				k.Emit('S')
			case i == len(w):
				k.Emit('K', 'S')
			default:
				k.Emit('X')
			}
		case c < utf8.RuneSelf && consonants[c] != 0:
			k.Emit(consonants[c])
		}
	}
}

// consonants maps the letters with a single sound, and d and t as spelled; h is handled above and absent, and z merges with s, which speech recognition writes for it and the other way round.
var consonants = [128]byte{
	'b': 'B', 'd': 'D', 'f': 'F', 'j': 'J', 'k': 'K', 'p': 'P', 'r': 'R', 's': 'S', 't': 'T', 'v': 'V', 'z': 'S',
}
