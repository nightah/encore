package loanword

import "strings"

// arabic writes English phones as Arabic writes an English loanword, the way Gulf writers and the ar-SA recogniser do: a consonant skeleton with the long vowels and diphthongs as ا, و and ي and the short vowels mostly left out, p as ب, v as ف, g as ق, ch as تش, th as ث and the th of "the" as ذ. A word that starts with a vowel starts with an alef; a word ends in the letter of its last vowel.
func arabic(ps []phone) string {
	var b strings.Builder

	ps = without(ps, geminate)

	for i, p := range ps {
		if !isVowelPhone(p) {
			if p == "ng" {
				b.WriteString("ن")

				if next := at(ps, i+1); next != "k" && next != "g" {
					b.WriteString("ق")
				}

				continue
			}

			if p == postR {
				p = "r"
			}

			b.WriteString(arConsonants[p])

			continue
		}

		switch {
		case i == 0:
			b.WriteString(arInitial[p])
		case i == len(ps)-1:
			b.WriteString(arFinal[p])
		default:
			b.WriteString(arMedial[p])
		}
	}

	return b.String()
}

// arConsonants are the Arabic letters English consonants are written with.
var arConsonants = map[phone]string{
	"p": "ب", "b": "ب", "t": "ت", "d": "د", "k": "ك", "g": "ق", "f": "ف", "v": "ف", "s": "س", "z": "ز", "sh": "ش", "zh": "ج",
	"ch": "تش", "j": "ج", "th": "ث", "dh": "ذ", "m": "م", "n": "ن", "h": "ه", "l": "ل", "r": "ر", "w": "و", "y": "ي",
}

// arInitial are the vowels starting a word, on an alef.
var arInitial = map[phone]string{
	"a": "ا", schwa: "ا", "ae": "ا", "e": "ا", "i": "ا", "u": "ا", "er": "ا", "aa": "ا", "ii": "اي", "ei": "اي", "ai": "اي",
	"ia": "اي", "ea": "اي", "o": "او", "oo": "او", "ou": "او", "uu": "او", "au": "او", "oi": "اوي",
}

// arMedial are the vowels inside a word: the long ones and diphthongs as letters, short a as ا and short e and i as ي, schwa, the u of "cup" and the vowel of "bird" left out.
var arMedial = map[phone]string{
	"a": "", schwa: "", "ae": "ا", "e": "ي", "i": "ي", "u": "و", "er": "", "aa": "ا", "ii": "ي", "ei": "ي", "ai": "اي",
	"ia": "يا", "ea": "ي", "o": "و", "oo": "و", "ou": "و", "uu": "و", "au": "او", "oi": "وي",
}

// arFinal are the vowels ending a word, each written with its letter.
var arFinal = map[phone]string{
	"a": "ا", schwa: "ا", "ae": "ا", "e": "ي", "i": "ي", "u": "و", "er": "ر", "aa": "ا", "ii": "ي", "ei": "ي", "ai": "اي",
	"ia": "يا", "ea": "ير", "o": "و", "oo": "و", "ou": "و", "uu": "و", "au": "او", "oi": "وي",
}

// arabicLetters are the Latin letters' names in Arabic script, as an initialism is said.
var arabicLetters = map[byte]string{
	'a': "اي", 'b': "بي", 'c': "سي", 'd': "دي", 'e': "اي", 'f': "اف", 'g': "جي", 'h': "اتش", 'i': "اي", 'j': "جي", 'k': "كي",
	'l': "ال", 'm': "ام", 'n': "ان", 'o': "او", 'p': "بي", 'q': "كيو", 'r': "ار", 's': "اس", 't': "تي", 'u': "يو", 'v': "في",
	'w': "دبليو", 'x': "اكس", 'y': "واي", 'z': "زد",
}

// writers are the languages Native writes.
var writers = map[string]*writer{
	"ja": {english: katakana, letters: katakanaLetters, joined: true, dropThe: true},
	"hi": {english: devanagari, letters: devanagariLetters, joined: true, romanised: romanHindi},
	"ar": {english: arabic, letters: arabicLetters},
}
