package loanword

import (
	"strings"
)

// english reads a lowercase ASCII word as English, into phones. Common words whose spelling the rules don't follow are looked up (englishWords); a plural, possessive, past or -ing ending is read as the stem and the ending; anything else is read by spelling rules.
func english(w string) []phone {
	if p, ok := englishWords[w]; ok {
		return p
	}

	if head, tail, ok := compound(w); ok {
		return append(english(head), englishWords[tail]...)
	}

	if stem, end, ok := suffix(w); ok {
		p := english(stem)

		if end == "s" && len(p) > 0 && voiceless(p[len(p)-1]) {
			return append(p, "s")
		}

		if end == "s" && len(p) > 0 && sibilant(p[len(p)-1]) {
			return append(p, "i", "z")
		}

		if end == "s" {
			return append(p, "z")
		}

		if end == "ed" && len(p) > 0 && (p[len(p)-1] == "t" || p[len(p)-1] == "d") {
			return append(p, "i", "d")
		}

		if end == "ed" && len(p) > 0 && voiceless(p[len(p)-1]) {
			return append(p, "t")
		}

		if end == "ed" {
			return append(p, "d")
		}

		return append(p, "i", "ng")
	}

	return (&reader{w: w}).read()
}

// compound splits a word that ends in one of englishWords of four letters or more after at least three letters of its own ("braveheart" -> "brave" "heart", "godfather" -> "god" "father"), so each part is read as it is said alone.
func compound(w string) (head, tail string, ok bool) {
	for i := 3; i+4 <= len(w); i++ {
		if _, known := englishWords[w[i:]]; known && strings.ContainsAny(w[:i], "aeiouy") {
			return w[:i], w[i:], true
		}
	}

	return "", "", false
}

// suffix splits an ending off w: a plural or possessive s, a past ed or an -ing, with the stem as it is spelt alone ("rises" -> "rise", "named" -> "name", "making" -> "make").
func suffix(w string) (stem, end string, ok bool) {
	n := len(w)

	switch {
	case n > 3 && w[n-1] == 's' && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is") && !strings.HasSuffix(w, "ys"):
		stem = w[:n-1]
		if strings.HasSuffix(stem, "e") && !magicE(stem) {
			if s := stem[:len(stem)-1]; endsSibilant(s) {
				return s, "s", true
			}
		}

		return stem, "s", true
	case n > 4 && strings.HasSuffix(w, "ed") && !isVowel(w[n-3]):
		if s := w[:n-1]; magicE(s) {
			return s, "ed", true
		}

		return w[:n-2], "ed", true
	case n > 5 && strings.HasSuffix(w, "ing") && !isVowel(w[n-4]):
		s := w[:n-3]
		if len(s) >= 2 && s[len(s)-1] == s[len(s)-2] && s[len(s)-1] != 'l' && s[len(s)-1] != 's' {
			s = s[:len(s)-1]
		} else if vowelGroups(s) == 1 && magicE(s+"e") {
			s += "e"
		}

		return s, "ing", true
	}

	return "", "", false
}

func endsSibilant(s string) bool {
	for _, end := range []string{"s", "x", "z", "ch", "sh"} {
		if strings.HasSuffix(s, end) {
			return true
		}
	}

	return false
}

// magicE reports whether w ends in a vowel, one consonant and a silent e, which makes the vowel long ("game", "stone"), and has at most one other vowel before it.
func magicE(w string) bool {
	n := len(w)
	if n < 3 || w[n-1] != 'e' || isVowel(w[n-2]) || strings.IndexByte("xwyh", w[n-2]) >= 0 || !isVowelY(w[n-3]) {
		return false
	}

	if n >= 4 && isVowel(w[n-4]) {
		return false
	}

	return vowelGroups(w[:n-1]) <= 2
}

// vowelGroups counts the runs of vowel letters in w, y among them after a consonant.
func vowelGroups(w string) int {
	groups, in := 0, false

	for i := range len(w) {
		v := isVowel(w[i]) || w[i] == 'y' && i > 0 && !isVowel(w[i-1])
		if v && !in {
			groups++
		}

		in = v
	}

	return groups
}

func isVowel(c byte) bool { return strings.IndexByte("aeiou", c) >= 0 }

func isVowelY(c byte) bool { return isVowel(c) || c == 'y' }

func voiceless(p phone) bool {
	switch p {
	case "p", "t", "k", "f", "th":
		return true
	}

	return false
}

func sibilant(p phone) bool {
	switch p {
	case "s", "z", "sh", "zh", "ch", "j":
		return true
	}

	return false
}

// isVowelPhone reports whether p is a vowel or schwa.
func isVowelPhone(p phone) bool {
	switch p {
	case "a", "ae", "e", "i", "o", "u", "aa", "ii", "uu", "oo", "er", "ei", "ai", "oi", "au", "ou", "ia", "ea", schwa:
		return true
	}

	return false
}

func (r *reader) read() []phone {
	n := len(r.w)
	r.long = -1

	if magicE(r.w) {
		r.long = n - 3
	}

	r.silent = n >= 3 && r.w[n-1] == 'e' && !isVowelY(r.w[n-2]) && vowelGroups(r.w[:n-1]) > 0

	for r.i < n {
		if isVowelY(r.w[r.i]) && !r.consonantY() {
			r.vowel()
		} else {
			r.consonant()
		}
	}

	return r.out
}

func (r *reader) emit(p ...phone) { r.out = append(r.out, p...) }

// has reports whether the word continues with s at the current letter.
func (r *reader) has(s string) bool { return strings.HasPrefix(r.w[r.i:], s) }

func (r *reader) at(i int) byte {
	if i < 0 || i >= len(r.w) {
		return 0
	}

	return r.w[i]
}

// final reports whether the letters from i on are only a silent final e or nothing.
func (r *reader) final(i int) bool {
	return i >= len(r.w) || i == len(r.w)-1 && r.silent
}

// consonantY reports whether the y at the current letter is a consonant: one that starts the word or follows a vowel letter, before a vowel.
func (r *reader) consonantY() bool {
	return r.w[r.i] == 'y' && isVowel(r.at(r.i+1)) && (r.i == 0 || !isVowel(r.at(r.i-1)))
}

// rhotic reports whether the r at i closes the vowel before it: no vowel follows it.
func (r *reader) rhotic(i int) bool {
	return r.at(i) == 'r' && r.at(i+1) != 'r' && (!isVowelY(r.at(i+1)) || r.final(i+1))
}

//nolint:gocyclo,cyclop,funlen // One case per spelling.
func (r *reader) consonant() {
	w, i := r.w, r.i
	c := w[i]
	next := r.at(i + 1)

	switch {
	case r.has("tch"):
		r.emit("ch")
		r.i += 3
	case r.has("sch"):
		r.emit("s", "k")
		r.i += 3
	case r.has("tion"), r.has("ssion"), r.has("cian"):
		if c == 's' {
			r.shortBefore()
			r.i++
		}

		r.emit("sh", schwa, "n")
		r.i += 4
	case r.has("sion"):
		if isVowel(r.at(i - 1)) {
			r.emit("zh", schwa, "n")
		} else {
			r.emit("sh", schwa, "n")
		}

		r.i += 4
	case r.has("ture"):
		r.emit("ch", schwa, postR)
		r.i += 4
	case c == 'p' && next == 'h':
		r.emit("f")
		r.i += 2
	case c == 'g' && next == 'h':
		if i == 0 {
			r.emit("g")
		}

		r.i += 2
	case c == 'c' && next == 'h':
		if r.at(i+2) == 'r' {
			r.emit("k")
		} else {
			r.emit("ch")
		}

		r.i += 2
	case c == 's' && next == 'h':
		r.emit("sh")
		r.i += 2
	case c == 't' && next == 'h':
		r.emit("th")
		r.i += 2
	case c == 'w' && next == 'h':
		r.emit("w")
		r.i += 2
	case c == 'w' && next == 'r' && i == 0, c == 'k' && next == 'n' && i == 0, c == 'g' && next == 'n' && (i == 0 || r.final(i+2)), c == 'p' && next == 's' && i == 0:
		r.i++
	case c == 'm' && (next == 'b' || next == 'n') && r.final(i+2):
		r.emit("m")
		r.i += 2
	case c == 'c' && next == 'k':
		r.shortBefore()
		r.emit("k")
		r.i += 2
	case c == 'c' && next == 'c' && strings.IndexByte("eiy", r.at(i+2)) >= 0:
		r.emit("k", "s")
		r.i += 2
	case c == 'c' && strings.IndexByte("eiy", next) >= 0:
		r.emit("s")
		r.i++
	case c == 'd' && next == 'g' && strings.IndexByte("eiy", r.at(i+2)) >= 0:
		r.emit("j")
		r.i += 2
	case c == 'g' && strings.IndexByte("eiy", next) >= 0 && i > 0 && (r.at(i-1) == 'a' || next == 'e' && r.final(i+1) && r.at(i-1) != 'g'):
		r.emit("j")
		r.i++
	case c == 'n' && next == 'g' && strings.IndexByte("eiy", r.at(i+2)) >= 0 && !r.has("ngs") && !strings.HasPrefix(w[i+2:], "ing"):
		r.emit("n", "j")
		r.i += 2
	case c == 'n' && next == 'g':
		r.emit("ng")
		r.i += 2
	case c == 'n' && next == 'k':
		r.emit("ng", "k")
		r.i += 2
	case c == 'q':
		r.emit("k", "w")

		r.i++
		if next == 'u' {
			r.i++
		}
	case c == 'x':
		if i == 0 {
			r.emit("z")
		} else {
			r.emit("k", "s")
		}

		r.i++
	case c == 'l' && next == 'e' && i > 0 && !isVowelY(r.at(i-1)) && i+2 == len(w):
		r.emit(schwa, "l")
		r.i += 2
	case c == 's' && isVowelY(r.at(i-1)) && isVowel(next) && !r.final(i+1) && i > 0:
		r.emit("z")
		r.i++
	case c == 'h' && !isVowelY(next):
		r.i++
	case c == 'w' || c == 'y':
		r.emit(string(c))
		r.i++
	default:
		if next == c && strings.IndexByte("ptkbdgs", c) >= 0 {
			r.shortBefore()
		}

		r.emit(consonantPhone(c))

		r.i++
		if next == c {
			r.i++
		}
	}
}

// shortBefore marks the consonant about to be written as doubled when a short vowel comes before it.
func (r *reader) shortBefore() {
	if n := len(r.out); n > 0 && short(r.out[n-1]) {
		r.emit(geminate)
	}
}

// consonantPhone is a consonant letter's usual sound.
func consonantPhone(c byte) phone {
	switch c {
	case 'c':
		return "k"
	case 'j':
		return "j"
	}

	return string(c)
}

//nolint:gocyclo,cyclop,funlen // One case per spelling.
func (r *reader) vowel() {
	w, i := r.w, r.i
	c := w[i]
	afterW := r.at(i-1) == 'w' || i >= 2 && w[i-2:i] == "qu"

	switch {
	case r.has("eigh"):
		r.emit("ei")
		r.i += 4
	case r.has("igh"):
		r.emit("ai")
		r.i += 3
	case r.has("ought"):
		r.emit("oo", "t")
		r.i += 5
	case r.has("ough"):
		if r.final(i+4) && i > 0 && strings.IndexByte("rtn", w[i-1]) >= 0 {
			r.emit("a", "f")
		} else {
			r.emit("ou")
		}

		r.i += 4
	case r.has("augh"):
		r.emit("oo")
		r.i += 4
	case r.has("air"), r.has("are") && r.final(i+3):
		r.emit("ea", postR)
		r.i += 3
	case r.has("earth"):
		r.emit("er", postR)
		r.i += 3
	case r.has("eart"):
		r.emit("aa", postR)
		r.i += 3
	case r.has("ear") && strings.IndexByte("nlc", r.at(i+3)) >= 0:
		r.emit("er", postR)
		r.i += 3
	case r.has("ear"), r.has("eer"), r.has("ier") && r.final(i+3):
		r.emit("ia", postR)
		r.i += 3
	case r.has("ire") && r.final(i+3):
		r.emit("ai", schwa, postR)
		r.i += 3
	case r.has("ure") && r.final(i+3):
		r.u(true)
		r.emit(schwa, postR)
		r.i += 3
	case r.has("our"):
		if strings.IndexByte("fypc", r.at(i-1)) >= 0 {
			r.emit("oo", postR)
		} else {
			r.emit("au", schwa, postR)
		}

		r.i += 3
	case r.has("oor"):
		r.emit("oo", postR)
		r.i += 3
	case c == 'o' && r.rhotic(i+1):
		if afterW {
			r.emit("er", postR)
		} else {
			r.emit("oo", postR)
		}

		r.i += 2
	case c == 'a' && r.rhotic(i+1):
		if afterW {
			r.emit("oo", postR)
		} else {
			r.emit("aa", postR)
		}

		r.i += 2
	case strings.IndexByte("eiuy", c) >= 0 && r.rhotic(i+1):
		r.emit("er", postR)
		r.i += 2
	case r.has("ai"), r.has("ay"):
		r.emit("ei")
		r.i += 2
	case r.has("au"), r.has("aw"):
		r.emit("oo")
		r.i += 2
	case r.has("ead") && r.final(i+3), r.has("eath"), r.has("ealth"), r.has("eav") && (r.has("eavy") || r.has("eaven")):
		r.emit("e")
		r.i += 2
	case r.has("ea"), r.has("ee"):
		r.emit("ii")
		r.i += 2
	case r.has("ei"):
		r.emit("ei")
		r.i += 2
	case r.has("ey"):
		if i+2 == len(w) && vowelGroups(w) > 1 {
			r.emit("ii")
		} else {
			r.emit("ei")
		}

		r.i += 2
	case r.has("eu"), r.has("ew"):
		r.u(false)
		r.i += 2
	case r.has("ie"):
		if i+2 == len(w) && len(w) <= 3 {
			r.emit("ai")
		} else {
			r.emit("ii")
		}

		r.i += 2
	case r.has("oa"), r.has("oe") && i+2 == len(w):
		r.emit("ou")
		r.i += 2
	case r.has("oi"), r.has("oy"):
		r.emit("oi")
		r.i += 2
	case r.has("oo"):
		if r.at(i+2) == 'k' {
			r.emit("u")
		} else {
			r.emit("uu")
		}

		r.i += 2
	case r.has("ous") && i+3 == len(w) && i > 0:
		// "-ous" is unstressed ("furious", "famous").
		r.emit(schwa)
		r.i += 2
	case r.has("ou"):
		r.emit("au")
		r.i += 2
	case r.has("ow"):
		if i+2 == len(w) && i > 1 || r.has("own") && ownWords[w] {
			r.emit("ou")
		} else {
			r.emit("au")
		}

		r.i += 2
	case r.has("ue"):
		r.u(false)
		r.i += 2
	case r.has("ui"):
		if strings.IndexByte("sjr", r.at(i-1)) >= 0 {
			r.emit("uu")
		} else {
			r.emit("i")
		}

		r.i += 2
	case r.has("uy"):
		r.emit("ai")
		r.i += 2
	case i == r.long:
		r.longVowel(c)
		r.i++
	case c == 'e' && r.final(i+1) && i > 0:
		if vowelGroups(w) == 1 {
			r.emit("ii")
		}

		r.i++
	default:
		r.shortVowel(c, afterW)
		r.i++
	}
}

// open reports whether the vowel at i ends its syllable: one consonant follows it, then a vowel that isn't a silent final e.
func (r *reader) open(i int) bool {
	next, after := r.at(i+1), r.at(i+2)

	return next != 0 && !isVowelY(next) && next != 'x' && next != 'w' && isVowelY(after) && !r.final(i+2)
}

// flyWords reports whether w ends in a word of one syllable that ends in y after two consonants ("superfly", "skyfall" aside), whose y is said as in "my".
func flyWords(w string) bool {
	for _, end := range []string{"fly", "sky", "cry", "dry", "try", "spy", "shy", "sly", "fry", "pry", "why"} {
		if strings.HasSuffix(w, end) {
			return true
		}
	}

	return false
}

// u writes a long u: oo after r, l, j, s, ch and sh ("rude", "super") and yoo elsewhere ("music"). force writes it after a vowel too.
func (r *reader) u(force bool) {
	if n := len(r.out); n > 0 && !force && isVowelPhone(r.out[n-1]) {
		r.emit("uu")

		return
	}

	if n := len(r.out); n > 0 {
		switch r.out[n-1] {
		case "r", "l", "j", "s", "ch", "sh", "y":
			r.emit("uu")

			return
		}
	}

	r.emit("y", "uu")
}

func (r *reader) longVowel(c byte) {
	switch c {
	case 'a':
		r.emit("ei")
	case 'e':
		r.emit("ii")
	case 'i', 'y':
		r.emit("ai")
	case 'o':
		if r.at(r.i+1) == 'v' && (r.at(r.i-1) == 'l' || r.at(r.i-1) == 'b') {
			r.emit("a")
		} else {
			r.emit("ou")
		}
	case 'u':
		r.u(false)
	}
}

//nolint:gocyclo,cyclop // One case per spelling.
func (r *reader) shortVowel(c byte, afterW bool) {
	w, i := r.w, r.i
	next, after := r.at(i+1), r.at(i+2)
	last := i == len(w)-1

	switch c {
	case 'a':
		switch {
		case last:
			r.emit(schwa)
		case next == 'l' && (after == 'l' || after == 't' || after == 'k'):
			r.emit("oo")

			if after == 'k' {
				r.i++
			}
		case next == 'l' && after == 'm':
			r.emit("aa")
			r.i++
		case afterW && !isVowelY(next):
			r.emit("o")
		default:
			r.emit("ae")
		}
	case 'e':
		if r.final(i+2) && next == 'n' && strings.IndexByte("szv", r.at(i-1)) >= 0 && i > 0 {
			r.emit(schwa)
		} else {
			r.emit("e")
		}
	case 'i':
		switch {
		case last:
			r.emit("ii")
		case r.open(i) && next != 'v' && r.w[i+2:] == "er":
			// "spider", "tiger".
			r.emit("ai")
		case (next == 'n' || next == 'l') && after == 'd' && r.final(i+3):
			r.emit("ai")
		default:
			r.emit("i")
		}
	case 'o':
		switch {
		case last:
			r.emit("ou")
		case next == 'l' && (after == 'd' || after == 't'), next == 's' && after == 't' && strings.IndexByte("hmp", r.at(i-1)) >= 0 && i > 0:
			r.emit("ou")
		case r.open(i) && vowelGroups(w) == 2:
			// A vowel that ends the first of two syllables is long ("frozen", "hotel").
			r.emit("ou")
		default:
			r.emit("o")
		}
	case 'u':
		switch {
		case !isVowelY(next) && next != 0 && isVowelY(after) && !r.final(i+2) || last:
			r.u(false)
		case (r.at(i-1) == 'p' || r.at(i-1) == 'b' || r.at(i-1) == 'f') && (next == 's' && after == 'h' || next == 'l' && after == 'l' || next == 't'):
			r.emit("u")
		default:
			r.emit("a")
		}
	case 'y':
		switch {
		case last && (vowelGroups(w) == 1 || flyWords(w)):
			r.emit("ai")
		case last:
			r.emit("ii")
		default:
			r.emit("i")
		}
	}
}
