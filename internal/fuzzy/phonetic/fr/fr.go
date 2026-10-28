package fr

import (
	"strings"
	"unicode/utf8"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	"github.com/nightah/encore/internal/fuzzy/phonetic/internal/skeleton"
)

func init() { phonetic.Register("fr", Encoder{}) }

// Keys appends the key of the word as French says it, with its silent ending left out, and, when different, the key with its final consonant sounded, which catches names that keep it ("Farmer", "Brest"). Neither key sounds a plural s, x or z, so a word's sounded consonant never meets another word's plural ("allez", "aliases").
func (Encoder) Keys(dst []string, word string) []string {
	if phonetic.IsDigits(word) {
		return append(dst, word)
	}

	var primary, full skeleton.Key

	singular, silent := sounded(word)
	encode(&primary, word[:silent])
	encode(&full, word[:singular])

	return skeleton.Append(dst, word, &primary, &full)
}

// sounded returns the length of word without a plural s, x or z (singular), and without its whole silent ending (silent): the plural mark, then a final t, d or p, then a g or c after n ("sang", "blanc"), then the r of a final -er ("dîner", "bronzer"). A consonant then "ed" keeps its d: that is an English past participle ("paused", "imported"), and French words end in "ed" only after a vowel ("pied"). At least three letters stay, so short words ("dit", "mot", "lit") keep the consonant that tells them apart.
func sounded(w string) (singular, silent int) {
	n := len(w)
	strip := func(set string) {
		if n > minSounded && strings.IndexByte(set, w[n-1]) >= 0 {
			n--
		}
	}

	strip("sxz")
	singular = n

	if participle := n > minSounded && w[n-1] == 'd' && w[n-2] == 'e' && !isVowel(w[n-3]); !participle {
		strip("tdp")
	}

	if n > 1 && w[n-2] == 'n' {
		strip("gc")
	}

	if n > 1 && w[n-2] == 'e' {
		strip("r")
	}

	return singular, n
}

func isVowel(c byte) bool { return strings.IndexByte("aeiouy", c) >= 0 }

// soft reports whether c softens a preceding c or g ("cent", "genou").
func soft(c byte) bool { return c == 'e' || c == 'i' || c == 'y' }

//nolint:gocyclo,cyclop,funlen // One case per letter keeps the rules readable against the table in doc.go.
func encode(k *skeleton.Key, w string) {
	at := func(i int) byte { return skeleton.At(w, i) }

	for i := 0; i < len(w); {
		c := w[i]
		i++

		switch {
		case isVowel(c):
			k.Vowel()
		case c == 'c':
			switch {
			case at(i) == 'h' && (at(i+1) == 'r' || at(i+1) == 'l'):
				k.Emit('K')
				i++
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
			case at(i) == 'n':
				k.Emit('N')
				i++
			case at(i) == 'u' && soft(at(i+1)):
				k.Emit('K')
				i++
			case soft(at(i)):
				k.Emit('J')
			default:
				k.Emit('K')
			}
		case c == 'p' && at(i) == 'h':
			k.Emit('F')
			i++
		case c == 'q':
			k.Emit('K')

			if at(i) == 'u' {
				i++
			}
		case c == 's' && at(i) == 'c' && at(i+1) == 'h':
			k.Emit('X')
			i += 2
		case c == 's' && at(i) == 'h':
			k.Emit('X')
			i++
		case c == 't' && at(i) == 'i' && isVowel(at(i+1)) && i > 1 && at(i-2) != 's':
			// -tion, -tiel: the t says s, except after s ("question").
			k.Emit('S')
		case c == 'x':
			if i == 1 {
				k.Emit('S')
			} else {
				k.Emit('K', 'S')
			}
		case c == 'm' && (at(i) == 'b' || at(i) == 'p' || i == len(w)) && isVowel(at(i-2)):
			// A nasal vowel spelt with m before b or p or at the end ("ensemble", "champ", "parfum") sounds like one spelt with n.
			k.Emit('N')
		case c < utf8.RuneSelf && consonants[c] != 0:
			k.Emit(consonants[c])
		}
	}
}

// consonants maps the letters with a single sound; h is silent and absent.
var consonants = [128]byte{
	'b': 'P', 'd': 'T', 'f': 'F', 'j': 'J', 'k': 'K', 'l': 'L', 'm': 'M', 'n': 'N', 'p': 'P',
	'r': 'R', 's': 'S', 't': 'T', 'v': 'F', 'w': 'F', 'z': 'S',
}
