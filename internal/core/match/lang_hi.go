package match

import (
	"strings"
	"unicode/utf8"

	"github.com/nightah/encore/internal/fuzzy/phonetic/en"
)

func hindi() *language {
	plain, long := hindiRomanizer{}, hindiRomanizer{long: true}

	// spell lists a Devanagari word's romanised forms: as romanize writes it and with long vowels doubled ("teen", "chaar"), the commonest Latin spellings.
	spell := func(dev ...string) []string {
		out := make([]string, 0, 2*len(dev))

		for _, d := range dev {
			for _, r := range []hindiRomanizer{plain, long} {
				out = append(out, r.romanize(d))
			}
		}

		return out
	}

	words := func(m map[string]int) map[string]int {
		out := make(map[string]int, len(m)*2)
		for dev, v := range m {
			for _, w := range spell(dev) {
				out[w] = v
			}
		}

		return out
	}

	return &language{
		code: "hi",
		and:  "aur",
		// Hindi says a half before the number ("साढ़े आठ"), which the shared ½ rule can't write, so it reads "8½" as the literal "8 aur adha".
		half:     "aur adha",
		articles: set("the", "a", "an", "da", "di"),
		stop: set(
			"ka", "ki", "ke", "ko", "se", "mein", "me", "men", "par", "aur", "ne",
			"the", "a", "an", "of", "and", "in", "on", "to", "for", "at", "with",
		),
		ordSuffixes: []string{"st", "nd", "rd", "th", "va", "vi", "ve", "van", "vin", "ven"},
		nums: numTable{
			units:      []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"},
			tens:       []string{"twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"},
			extraUnits: merge(words(hindiUnits), hindiLatinUnits),
			extraTen:   words(hindiTens),
			compounds:  words(hindiCompounds),
			hundreds:   append(spell("सौ"), "hundred"),
			thousands:  append(spell("हज़ार", "हजार"), "thousand"),
			scales: merge(
				suffixed(map[string]int{"lakh": 100_000, "lac": 100_000}, "", "s"),
				map[string]int{"crore": 10_000_000, "crores": 10_000_000},
				words(map[string]int{"लाख": 100_000, "करोड़": 10_000_000}),
			),
			ordinals: merge(words(hindiOrdinals()), map[string]int{
				"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9,
				"tenth": 10, "eleventh": 11, "twelfth": 12, "pehla": 1, "pehli": 1, "pehle": 1,
			}),
		}.build(),
		conj:     set("aur", "and"),
		parts:    set(append(spell("भाग", "पार्ट", "सीज़न", "सीजन", "खंड", "अध्याय", "चैप्टर", "वॉल्यूम"), "part", "chapter", "season", "volume", "vol")...),
		split:    hindiLetters,
		romanize: plain.romanize,
		phonetic: phoneticKeysOf(en.Encoder{}),
	}
}

// hindiUnits are the Hindi numbers below twenty, in Devanagari with their common variant spellings.
var hindiUnits = map[string]int{
	"शून्य": 0, "एक": 1, "दो": 2, "तीन": 3, "चार": 4, "पाँच": 5, "पांच": 5, "छह": 6, "छः": 6, "छे": 6, "छै": 6, "सात": 7,
	"आठ": 8, "नौ": 9, "दस": 10, "ग्यारह": 11, "बारह": 12, "तेरह": 13, "चौदह": 14, "पंद्रह": 15, "पन्द्रह": 15, "सोलह": 16,
	"सत्रह": 17, "अठारह": 18, "उन्नीस": 19,
	// English number words as Hindi writes them.
	"ज़ीरो": 0, "जीरो": 0, "टू": 2, "थ्री": 3, "फ़ाइव": 5, "फाइव": 5, "सिक्स": 6, "सेवन": 7, "नाइन": 9, "टेन": 10, "इलेवन": 11, "ट्वेल्व": 12,
}

// hindiTens are the Hindi tens, which tens counts as a tens word only when it is a whole ten.
var hindiTens = map[string]int{
	"बीस": 20, "तीस": 30, "चालीस": 40, "पचास": 50, "साठ": 60, "सत्तर": 70, "अस्सी": 80, "नब्बे": 90,
	"ट्वेंटी": 20, "थर्टी": 30, "फ़ोर्टी": 40, "फोर्टी": 40, "फ़िफ़्टी": 50, "फिफ्टी": 50,
}

// hindiCompounds are the Hindi numbers from 21 to 99, each its own word.
var hindiCompounds = map[string]int{
	"इक्कीस": 21, "बाईस": 22, "तेईस": 23, "चौबीस": 24, "पच्चीस": 25, "छब्बीस": 26, "सत्ताईस": 27, "अट्ठाईस": 28, "उनतीस": 29,
	"इकतीस": 31, "बत्तीस": 32, "तैंतीस": 33, "चौंतीस": 34, "चौतीस": 34, "पैंतीस": 35, "छत्तीस": 36, "सैंतीस": 37, "अड़तीस": 38,
	"उनतालीस": 39, "इकतालीस": 41, "बयालीस": 42, "तैंतालीस": 43, "चवालीस": 44, "चौवालीस": 44, "पैंतालीस": 45, "छियालीस": 46,
	"सैंतालीस": 47, "अड़तालीस": 48, "उनचास": 49, "इक्यावन": 51, "बावन": 52, "तिरपन": 53, "चौवन": 54, "पचपन": 55, "छप्पन": 56,
	"सत्तावन": 57, "अट्ठावन": 58, "उनसठ": 59, "इकसठ": 61, "बासठ": 62, "तिरसठ": 63, "चौंसठ": 64, "पैंसठ": 65, "छियासठ": 66,
	"सड़सठ": 67, "अड़सठ": 68, "उनहत्तर": 69, "इकहत्तर": 71, "बहत्तर": 72, "तिहत्तर": 73, "चौहत्तर": 74, "पचहत्तर": 75,
	"छिहत्तर": 76, "सतहत्तर": 77, "अठहत्तर": 78, "उन्यासी": 79, "उनासी": 79, "इक्यासी": 81, "बयासी": 82, "तिरासी": 83,
	"चौरासी": 84, "पचासी": 85, "छियासी": 86, "सत्तासी": 87, "अट्ठासी": 88, "नवासी": 89, "इक्यानबे": 91, "इक्यानवे": 91,
	"बानबे": 92, "बानवे": 92, "तिरानबे": 93, "तिरानवे": 93, "चौरानबे": 94, "चौरानवे": 94, "पचानबे": 95, "पचानवे": 95,
	"छियानबे": 96, "छियानवे": 96, "सत्तानबे": 97, "सत्तानवे": 97, "अट्ठानबे": 98, "अट्ठानवे": 98, "निन्यानबे": 99, "निन्यानवे": 99,
}

// hindiLatinUnits are Latin spellings of Hindi numbers below twenty that neither romanisation of the Devanagari gives.
var hindiLatinUnits = map[string]int{
	"che": 6, "chhe": 6, "chhah": 6, "chah": 6, "cheh": 6, "chhey": 6, "nao": 9, "gyara": 11, "bara": 12, "baara": 12,
	"tera": 13, "chauda": 14, "pandra": 15, "sola": 16, "satra": 17, "athara": 18, "attharah": 18, "dus": 10,
}

// hindiOrdinals are the Hindi ordinals in every gender and case ending (पहला, पहली, पहले), and -वाँ, -वीं and -वें after the cardinal from five on, with छठा for six.
func hindiOrdinals() map[string]int {
	m := map[string]int{}

	for stem, v := range map[string]int{"पहल": 1, "दूसर": 2, "तीसर": 3, "चौथ": 4, "छठ": 6} {
		for _, end := range []string{"ा", "ी", "े"} {
			m[stem+end] = v
		}
	}

	for dev, v := range merge(hindiUnits, hindiTens, hindiCompounds) {
		if v < 5 || v == 6 || !hindiNative(dev) {
			continue
		}

		for _, end := range []string{"वाँ", "वां", "वीं", "वी", "वें", "वे"} {
			m[dev+end] = v
		}
	}

	for _, end := range []string{"वाँ", "वां", "वीं", "वी", "वें", "वे"} {
		m["सौ"+end] = 100
	}

	return m
}

// hindiNative reports whether dev is a Hindi number word rather than an English one written in Devanagari.
func hindiNative(dev string) bool {
	switch dev {
	case "ज़ीरो", "जीरो", "टू", "थ्री", "फ़ाइव", "फाइव", "सिक्स", "सेवन", "नाइन", "टेन", "इलेवन", "ट्वेल्व", "ट्वेंटी", "थर्टी", "फ़ोर्टी", "फोर्टी", "फ़िफ़्टी", "फिफ्टी":
		return false
	}

	return true
}

// hindiLetterNames are the Devanagari names of the Latin letters, as the recogniser writes an initialism spelt out ("एम बी बी एस").
var hindiLetterNames = map[string]string{
	"ए": "a", "बी": "b", "सी": "c", "डी": "d", "ई": "e", "एफ": "f", "एफ़": "f", "जी": "g", "एच": "h", "आई": "i", "जे": "j",
	"के": "k", "एल": "l", "एम": "m", "एन": "n", "ओ": "o", "पी": "p", "क्यू": "q", "आर": "r", "एस": "s", "टी": "t", "यू": "u",
	"वी": "v", "डब्ल्यू": "w", "डबल्यू": "w", "एक्स": "x", "वाई": "y", "ज़ेड": "z", "जेड": "z",
}

// hindiLetters writes a run of two or more Devanagari letter names as the Latin letters they name ("ए आर रहमान" -> "a r रहमान"), which joinLetters then joins. A letter name alone is a word ("के", "जी") and stays.
func hindiLetters(toks []string) []string {
	for i := 0; i < len(toks); {
		j := i
		for j < len(toks) && hindiLetterNames[toks[j]] != "" {
			j++
		}

		if j-i >= 2 {
			for k := i; k < j; k++ {
				toks[k] = hindiLetterNames[toks[k]]
			}
		}

		i = max(j, i+1)
	}

	return toks
}

func (u hindiUnit) voiced() bool { return u.vowel != "" && !u.deleted }

// romanize writes the Devanagari runs of tok in Latin letters and leaves anything else as it is.
func (h hindiRomanizer) romanize(tok string) string {
	if !hasDevanagari(tok) {
		return tok
	}

	if w, ok := hindiWords[tok]; ok {
		return w
	}

	var b strings.Builder

	for tok != "" {
		end := 0
		dev := isDevanagari(firstRune(tok))

		for end < len(tok) {
			r, n := utf8.DecodeRuneInString(tok[end:])
			if isDevanagari(r) != dev {
				break
			}

			end += n
		}

		if dev {
			b.WriteString(h.word(tok[:end]))
		} else {
			b.WriteString(tok[:end])
		}

		tok = tok[end:]
	}

	return b.String()
}

// hindiWords are words whose usual romanisation the rules don't give.
var hindiWords = map[string]string{"में": "mein", "मैं": "main", "हैं": "hain", "नहीं": "nahin"}

func (h hindiRomanizer) word(w string) string {
	units := h.syllables(w)
	deleteSchwas(units)

	var b strings.Builder

	for i, u := range units {
		b.WriteString(u.cons)

		switch {
		case u.voiced() && u.vowel == "oo" && i == len(units)-1 && !h.long:
			// ऊ is oo inside a word ("धूम" Dhoom) and u at its end ("राजू" Raju).
			b.WriteByte('u')
		case u.voiced():
			b.WriteString(u.vowel)
		}

		if u.nasal != 0 {
			b.WriteString(nasal(units, i))
		}

		if u.visarga {
			b.WriteByte('h')
		}
	}

	return b.String()
}

// nasal writes the anusvara or chandrabindu of units[i] as the nasal of the consonant after it: m before a labial, ng before h ("सिंह" Singh), n otherwise.
func nasal(units []hindiUnit, i int) string {
	if i+1 >= len(units) || units[i+1].cons == "" {
		return "n"
	}

	switch next := units[i+1].cons; {
	case next == "h":
		return "ng"
	case strings.IndexByte("pbm", next[0]) >= 0 || next == "f":
		return "m"
	default:
		return "n"
	}
}

// syllables splits a Devanagari word into written syllables.
//
//nolint:gocyclo,cyclop // One case per kind of sign.
func (h hindiRomanizer) syllables(w string) []hindiUnit {
	var units []hindiUnit

	last := func() *hindiUnit {
		if len(units) == 0 {
			units = append(units, hindiUnit{})
		}

		return &units[len(units)-1]
	}

	for _, r := range w {
		switch {
		case r == nukta:
			if u := last(); u.cons != "" {
				if f, ok := nuktaConsonants[u.cons]; ok {
					u.cons = f
				}
			}
		case r == virama:
			if u := last(); u.cons != "" {
				u.vowel, u.inherent = "", false
			}
		case r == anusvara, r == chandrabindu:
			last().nasal = r
		case r == visarga:
			last().visarga = true
		case hindiConsonants[r] != "":
			cons := hindiConsonants[r]
			// व after a consonant is w ("स्वदेस" Swades, "द्वार" dwar), and ज्ञ is said gy ("ज्ञान" gyan).
			if n := len(units); n > 0 && units[n-1].cons != "" && units[n-1].vowel == "" {
				switch {
				case cons == "v":
					cons = "w"
				case cons == "n" && units[n-1].cons == "j":
					units[n-1].cons, cons = "g", "y"
				}
			}

			units = append(units, hindiUnit{cons: cons, vowel: "a", inherent: true})
		case hindiMatras[r] != "":
			v := h.vowel(r, hindiMatras[r])
			if u := last(); u.cons != "" && u.inherent {
				u.vowel, u.inherent = v, false
			} else {
				units = append(units, hindiUnit{vowel: v})
			}
		case hindiVowels[r] != "":
			v := h.vowel(r, hindiVowels[r])
			// A vowel letter after a vowel takes a y glide where it starts with e ("गए" gaye, "जाएंगे" jayenge).
			if n := len(units); n > 0 && units[n-1].voiced() && v[0] == 'e' {
				v = "y" + v
			}

			units = append(units, hindiUnit{vowel: v})
		}
	}

	return units
}

// vowel is the Latin vowel for a Devanagari vowel sign or letter, doubled for a long one when h.long is set.
func (h hindiRomanizer) vowel(r rune, v string) string {
	if !h.long {
		return v
	}

	switch r {
	case 'आ', 'ा':
		return "aa"
	case 'ई', 'ी':
		return "ee"
	}

	return v
}

// deleteSchwas drops the inherent vowels Hindi speech leaves out, working right to left as the rule in Ohala, Aspects of Hindi Phonology (1983), does and as Narasimhan, Sproat and Kiraz, "Schwa-deletion in Hindi text-to-speech synthesis" (2004), apply it: the last one, unless the word is one syllable or ends in a consonant cluster closed by y, r, l or v ("मित्र" mitra), and one between a vowel and a consonant that has a vowel of its own ("धड़कन" dhadkan, "दिलवाले" dilvale). A vowel nasalised with an anusvara counts as closed by a consonant, so "ज़िंदगी" keeps its schwa (zindagi, not zindgi).
func deleteSchwas(units []hindiUnit) {
	last := len(units) - 1

	for i := last; i > 0; i-- {
		u := &units[i]
		if !u.inherent || u.nasal != 0 || u.visarga {
			continue
		}

		prev := units[i-1]

		if i == last {
			u.deleted = prev.cons == "" || prev.vowel != "" || !semivowel(u.cons)

			continue
		}

		next := units[i+1]
		u.deleted = prev.voiced() && prev.nasal != anusvara && !prev.visarga && next.cons != "" && next.voiced()
	}
}

func semivowel(cons string) bool {
	switch cons {
	case "y", "r", "l", "v", "w":
		return true
	}

	return false
}

func isDevanagari(r rune) bool { return r >= 0x0900 && r <= 0x097F }

func hasDevanagari(s string) bool { return strings.ContainsFunc(s, isDevanagari) }

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)

	return r
}

// hindiConsonants are the Latin spellings of the Devanagari consonants: aspirates with h, retroflex and dental alike, श and ष both sh.
var hindiConsonants = map[rune]string{
	'क': "k", 'ख': "kh", 'ग': "g", 'घ': "gh", 'ङ': "n", 'च': "ch", 'छ': "chh", 'ज': "j", 'झ': "jh", 'ञ': "n",
	'ट': "t", 'ठ': "th", 'ड': "d", 'ढ': "dh", 'ण': "n", 'त': "t", 'थ': "th", 'द': "d", 'ध': "dh", 'न': "n", 'ऩ': "n",
	'प': "p", 'फ': "ph", 'ब': "b", 'भ': "bh", 'म': "m", 'य': "y", 'र': "r", 'ऱ': "r", 'ल': "l", 'ळ': "l", 'ऴ': "l",
	'व': "v", 'श': "sh", 'ष': "sh", 'स': "s", 'ह': "h",
}

// nuktaConsonants are what a nukta makes of a consonant's Latin spelling: the Perso-Arabic sounds, and ड़ and ढ़ as d and dh, as most titles write them ("धड़कन" Dhadkan, "उड़ता" Udta).
var nuktaConsonants = map[string]string{"k": "q", "kh": "kh", "g": "gh", "j": "z", "jh": "zh", "ph": "f", "d": "d", "dh": "dh", "y": "y"}

// hindiMatras are the vowel signs.
var hindiMatras = map[rune]string{
	'ा': "a", 'ि': "i", 'ी': "i", 'ु': "u", 'ू': "oo", 'ृ': "ri", 'ॄ': "ri", 'ॅ': "e", 'ॆ': "e", 'े': "e", 'ै': "ai",
	'ॉ': "o", 'ॊ': "o", 'ो': "o", 'ौ': "au", 'ॢ': "li", 'ॣ': "li",
}

// hindiVowels are the vowel letters.
var hindiVowels = map[rune]string{
	'ऄ': "a", 'अ': "a", 'आ': "a", 'इ': "i", 'ई': "i", 'उ': "u", 'ऊ': "oo", 'ऋ': "ri", 'ऌ': "li", 'ऍ': "e", 'ऎ': "e",
	'ए': "e", 'ऐ': "ai", 'ऑ': "o", 'ऒ': "o", 'ओ': "o", 'औ': "au", 'ॠ': "ri", 'ॡ': "li", 'ॲ': "e", 'ॐ': "om",
}
