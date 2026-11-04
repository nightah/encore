package loanword

import "strings"

// devanagari writes English phones as Hindi writes an English loanword: t and d as the retroflex ट and ड, th as थ and the th of "the" as द, z as ज, the way Hindi speakers say it and recognisers write it, f as फ, v and w as व, and the short o of "hot" and the long one of "law" as ॉ. A consonant before another takes a virama; the inherent vowel stands for schwa, and an r after a vowel is written र.
func devanagari(ps []phone) string {
	var b strings.Builder

	ps = without(ps, geminate)

	for i, p := range ps {
		if isVowelPhone(p) {
			switch {
			case i > 0 && (ps[i-1] == "i" || ps[i-1] == "ii") && (p == schwa || p == "a"):
				// A vowel after i glides with y ("India" इंडिया).
				b.WriteString("या")
			case i > 0 && !isVowelPhone(ps[i-1]) && p == schwa && i+1 == len(ps) && vowels(ps) > 1:
				// A final a is written long, as Hindi says it ("Pushpa", "Sofia").
				b.WriteString("ा")
			case i > 0 && !isVowelPhone(ps[i-1]):
				b.WriteString(devMatras[p])
			default:
				b.WriteString(devVowels[p])
			}

			continue
		}

		next := at(ps, i+1)

		if (p == "n" || p == "m") && next != "" && !isVowelPhone(next) && next != "y" && next != "h" && i > 0 {
			// n and m before a consonant are the anusvara ("India" इंडिया, "Impossible" इंपॉसिबल).
			b.WriteString("ं")

			continue
		}

		if p == "z" && i+1 == len(ps) && i > 0 && !isVowelPhone(ps[i-1]) {
			// A plural s after a consonant is written स, as it is heard ("Beatles" बीटल्स).
			p = "s"
		}

		c := devConsonant(p)
		if p == "ng" {
			b.WriteString("ं")

			if next := at(ps, i+1); next == "k" || next == "g" {
				continue
			}

			c = "ग"
		}

		b.WriteString(c)

		if next != "" && !isVowelPhone(next) && (p != postR || vowels(ps[i:]) == 0) {
			b.WriteString("्")
		}
	}

	return b.String()
}

// without returns ps without the phone x.
func without(ps []phone, x phone) []phone {
	out := make([]phone, 0, len(ps))
	for _, p := range ps {
		if p != x {
			out = append(out, p)
		}
	}

	return out
}

// vowels counts the vowels in ps.
func vowels(ps []phone) int {
	n := 0
	for _, p := range ps {
		if isVowelPhone(p) {
			n++
		}
	}

	return n
}

func devConsonant(p phone) string {
	if p == postR {
		return "र"
	}

	return devConsonants[p]
}

// devConsonants are the Devanagari letters Hindi writes English consonants with.
var devConsonants = map[phone]string{
	"p": "प", "b": "ब", "t": "ट", "d": "ड", "k": "क", "g": "ग", "f": "फ", "v": "व", "w": "व", "s": "स", "z": "ज", "sh": "श",
	"zh": "ज", "ch": "च", "j": "ज", "th": "थ", "dh": "द", "m": "म", "n": "न", "h": "ह", "l": "ल", "r": "र", "y": "य",
}

// devVowels are the vowel letters written where no consonant comes before.
var devVowels = map[phone]string{
	"a": "अ", schwa: "अ", "ae": "ऐ", "e": "ए", "i": "इ", "o": "ऑ", "u": "उ", "aa": "आ", "ii": "ई", "uu": "ऊ", "oo": "ऑ",
	"er": "अ", "ei": "ए", "ai": "आइ", "oi": "ऑय", "au": "आउ", "ou": "ओ", "ia": "इय", "ea": "ए",
}

// devMatras are the vowel signs written after a consonant; schwa and the vowel of "bird" are the inherent vowel.
var devMatras = map[phone]string{
	"a": "", schwa: "", "ae": "ै", "e": "े", "i": "ि", "o": "ॉ", "u": "ु", "aa": "ा", "ii": "ी", "uu": "ू", "oo": "ॉ",
	"er": "", "ei": "े", "ai": "ाइ", "oi": "ॉय", "au": "ाउ", "ou": "ो", "ia": "िय", "ea": "े",
}

// romanHindi writes a word romanised the way Hindi film titles are ("Deewaar", "Chhota", "Zanjeer") back in Devanagari, letter by letter: doubled vowels are long, aspirates are written with h, a final a is आ, n before a consonant is the anusvara and z is ज, as recognisers write it.
//
//nolint:gocyclo,cyclop // One case per kind of letter.
func romanHindi(w string) string {
	var b strings.Builder

	// cons is set while the last thing written is a consonant without a vowel sign.
	cons := false

	for i := 0; i < len(w); {
		if v, n := romanVowel(w[i:], i+1 == len(w) || i+2 == len(w) && w[i] == w[i+1]); n > 0 {
			if cons {
				b.WriteString(v.matra)
			} else {
				b.WriteString(v.letter)
			}

			cons = false
			i += n

			continue
		}

		if w[i] == 'h' && i > 0 && i+1 == len(w) && w[i-1] == 'e' {
			break
		}

		if w[i] == 'n' && i+1 < len(w) && !isVowelY(w[i+1]) && w[i+1] != 'h' && !cons && i > 0 {
			b.WriteString("ं")
			i++

			continue
		}

		c, n := romanConsonant(w[i:])
		if n == 0 {
			i++

			continue
		}

		if cons {
			b.WriteString("्")
		}

		b.WriteString(c)

		cons = true
		i += n

		if i < len(w) && w[i] == w[i-1] && n == 1 && !isVowel(w[i]) {
			b.WriteString("्")
			b.WriteString(c)
			i++
		}
	}

	return b.String()
}

// romanVowel reads the vowel s starts with, as its letters' count; last is set when it ends the word, where a is आ and i ई.
func romanVowel(s string, last bool) (devVowel, int) {
	for _, v := range romanVowels {
		if strings.HasPrefix(s, v.spelling) {
			if last && v.spelling == "a" {
				return devVowel{"आ", "ा"}, 1
			}

			if last && v.spelling == "i" {
				return devVowel{"ई", "ी"}, 1
			}

			return v.devVowel, len(v.spelling)
		}
	}

	return devVowel{}, 0
}

// romanVowels are the vowel spellings of romanised Hindi, longest first.
var romanVowels = []struct {
	spelling string
	devVowel
}{
	{"aa", devVowel{"आ", "ा"}},
	{"ee", devVowel{"ई", "ी"}},
	{"ii", devVowel{"ई", "ी"}},
	{"oo", devVowel{"ऊ", "ू"}},
	{"uu", devVowel{"ऊ", "ू"}},
	{"ai", devVowel{"ऐ", "ै"}},
	{"ei", devVowel{"ऐ", "ै"}},
	{"au", devVowel{"औ", "ौ"}},
	{"ou", devVowel{"औ", "ौ"}},
	{"a", devVowel{"अ", ""}},
	{"i", devVowel{"इ", "ि"}},
	{"u", devVowel{"उ", "ु"}},
	{"e", devVowel{"ए", "े"}},
	{"o", devVowel{"ओ", "ो"}},
}

// romanConsonant reads the consonant s starts with, as its letters' count.
func romanConsonant(s string) (string, int) {
	for _, c := range romanConsonants {
		if strings.HasPrefix(s, c[0]) {
			return c[1], len(c[0])
		}
	}

	return "", 0
}

// romanConsonants are the consonant spellings of romanised Hindi, longest first.
var romanConsonants = [][2]string{
	{"chh", "छ"},
	{"kh", "ख"},
	{"gh", "घ"},
	{"ch", "च"},
	{"jh", "झ"},
	{"th", "थ"},
	{"dh", "ध"},
	{"ph", "फ"},
	{"bh", "भ"},
	{"sh", "श"},
	{"k", "क"},
	{"g", "ग"},
	{"c", "क"},
	{"j", "ज"},
	{"t", "त"},
	{"d", "द"},
	{"n", "न"},
	{"p", "प"},
	{"f", "फ"},
	{"b", "ब"},
	{"m", "म"},
	{"y", "य"},
	{"r", "र"},
	{"l", "ल"},
	{"v", "व"},
	{"w", "व"},
	{"s", "स"},
	{"z", "ज"},
	{"h", "ह"},
	{"q", "क"},
	{"x", "क्स"},
}

// devanagariLetters are the Latin letters' names in Devanagari, as Hindi speakers say an initialism.
var devanagariLetters = map[byte]string{
	'a': "ए", 'b': "बी", 'c': "सी", 'd': "डी", 'e': "ई", 'f': "एफ", 'g': "जी", 'h': "एच", 'i': "आई", 'j': "जे", 'k': "के",
	'l': "एल", 'm': "एम", 'n': "एन", 'o': "ओ", 'p': "पी", 'q': "क्यू", 'r': "आर", 's': "एस", 't': "टी", 'u': "यू", 'v': "वी",
	'w': "डब्ल्यू", 'x': "एक्स", 'y': "वाई", 'z': "जेड",
}
