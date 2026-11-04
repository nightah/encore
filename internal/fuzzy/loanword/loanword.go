package loanword

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Native returns the forms a recogniser for lang ("ja", "hi", "ar", or a locale such as "ja-JP") writes for s when it is said aloud: its Latin-script words written in the language's script, everything else as it is. It returns nil for a language it has no writer for and for s without Latin letters. There are at most a few forms, the likeliest first: Japanese adds one without a leading "The", and Hindi one that reads each word as a romanisation of Hindi.
func Native(lang, s string) []string {
	wr, ok := writers[base(lang)]
	if !ok {
		return nil
	}

	pieces := split(s)
	if !hasWords(pieces) {
		return nil
	}

	var out []string

	add := func(f string) {
		f = strings.Join(strings.Fields(f), " ")
		if f != "" && f != s && len(out) < maxForms && !contains(out, f) {
			out = append(out, f)
		}
	}

	variants := []func(string) string{func(w string) string { return wr.english(english(w)) }}
	if wr.romanised != nil {
		variants = append(variants, wr.romanised)

		if romanisedTitle(pieces) {
			variants[0], variants[1] = variants[1], variants[0]
		}
	}

	for _, v := range variants {
		f := wr.write(pieces, v)
		add(f)

		if wr.dropThe {
			if rest, ok := withoutThe(pieces); ok {
				add(wr.write(rest, v))
			}
		}
	}

	return out
}

// romanisedTitle reports whether a title's words look more like a romanisation of Hindi than like English ("Yeh Jawaani Hai Deewani", not "Stranger Things"), so the romanised reading is the likelier.
func romanisedTitle(pieces []piece) bool {
	score := 0

	for _, p := range pieces {
		if p.w != nil && !p.w.letters && !p.w.keep {
			score += romanisedScore(fold(p.w.text))
		}
	}

	return score > 0
}

// romanisedScore counts the spellings in w that romanised Hindi uses and English doesn't (aa, kh, bh, a final i), less those English uses and romanised Hindi doesn't (tion, igh, ck, a c not before h, a silent final e).
func romanisedScore(w string) int {
	score := 0

	for _, s := range []string{"aa", "kh", "bh", "dh", "jh", "chh", "ii", "uu"} {
		if strings.Contains(w, s) {
			score++
		}
	}

	if strings.HasSuffix(w, "i") && len(w) > 2 {
		score++
	}

	for _, s := range []string{"tion", "igh", "ck", "qu", "x", "wh", "ea", "ou", "ow", "th"} {
		if strings.Contains(w, s) {
			score--
		}
	}

	if i := strings.IndexByte(w, 'c'); i >= 0 && !strings.HasPrefix(w[i:], "ch") {
		score--
	}

	if n := len(w); n > 2 && w[n-1] == 'e' && !isVowelY(w[n-2]) {
		score--
	}

	return score
}

// Supports reports whether Native writes lang.
func Supports(lang string) bool {
	_, ok := writers[base(lang)]

	return ok
}

func base(lang string) string {
	lang = strings.ToLower(lang)
	if i := strings.IndexAny(lang, "-_"); i >= 0 {
		lang = lang[:i]
	}

	return lang
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}

	return false
}

func hasWords(pieces []piece) bool {
	for _, p := range pieces {
		if p.w != nil && !p.w.keep {
			return true
		}
	}

	return false
}

// withoutThe returns the pieces after a leading "The", when other words follow it.
func withoutThe(pieces []piece) ([]piece, bool) {
	i := 0
	for i < len(pieces) && pieces[i].w == nil && strings.TrimSpace(pieces[i].text) == "" {
		i++
	}

	if i >= len(pieces) || pieces[i].w == nil || !strings.EqualFold(pieces[i].w.text, "the") || !hasWords(pieces[i+1:]) {
		return nil, false
	}

	return pieces[i+1:], true
}

// split breaks s into Latin words and the text around them. A word is a run of Latin letters, an apostrophe inside it ("Philosopher's") left out. A single letter followed by a full stop is a letter of an initialism ("E.T.", "K.G.F") and the stop is dropped.
func split(s string) []piece {
	var (
		out   []piece
		other strings.Builder
		cur   strings.Builder
	)

	flushOther := func() {
		if other.Len() > 0 {
			out = append(out, piece{text: other.String()})
			other.Reset()
		}
	}

	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if !isLatin(r) {
			other.WriteRune(r)

			continue
		}

		flushOther()
		cur.Reset()

		for ; i < len(runes); i++ {
			r = runes[i]
			if isLatin(r) {
				cur.WriteRune(r)

				continue
			}

			if isApostrophe(r) && i+1 < len(runes) && isLatin(runes[i+1]) {
				continue
			}

			break
		}

		text := cur.String()
		dotted := i < len(runes) && runes[i] == '.' && utf8.RuneCountInString(text) == 1

		if dotted {
			i++
		}

		last := !strings.ContainsFunc(string(runes[i:]), isLatin)
		out = append(out, piece{w: classify(text, dotted, out, last)})
		i--
	}

	flushOther()

	return out
}

// classify decides how a Latin word is written: spelt out by letter name, kept or read.
func classify(text string, dotted bool, before []piece, last bool) *word {
	w := &word{text: text}
	upper := strings.ToUpper(text) == text

	switch n := utf8.RuneCountInString(text); {
	case romanNumerals[text]:
		w.keep = true
	case n == 1 && (text == "V" || text == "X") && last && hasWords(before):
		w.keep = true
	case n == 1:
		w.letters = dotted || !strings.EqualFold(text, "a") && !strings.EqualFold(text, "i")
	case upper && unsayable(fold(text)):
		w.letters = true
	}

	return w
}

// unsayable reports whether a word in capitals is an initialism: it has no vowel, it is two letters and not an English word ("OK" is said by its letters), or it is a short word with two consonants together that no English syllable has ("AKB"); an s may follow any consonant ("DREAMS").
func unsayable(w string) bool {
	if !strings.ContainsAny(w, "aeiouy") {
		return true
	}

	if len(w) == 2 {
		return !twoLetterWords[w]
	}

	if len(w) > 4 {
		return false
	}

	for i := 0; i+1 < len(w); i++ {
		if !isVowelY(w[i]) && !isVowelY(w[i+1]) && w[i] != w[i+1] && w[i+1] != 's' && w[i] != 's' && !codas[w[i:i+2]] {
			return true
		}
	}

	return false
}

// twoLetterWords are the English words of two letters a title in capitals may hold ("ONE OK ROCK").
var twoLetterWords = map[string]bool{
	"am": true, "an": true, "as": true, "at": true, "be": true, "by": true, "do": true, "go": true, "he": true, "hi": true,
	"if": true, "in": true, "is": true, "it": true, "me": true, "my": true, "no": true, "of": true, "oh": true,
	"on": true, "or": true, "so": true, "to": true, "up": true, "us": true, "we": true, "yo": true,
}

func isLatin(r rune) bool {
	return r < utf8.RuneSelf && unicode.IsLetter(r) || r >= utf8.RuneSelf && unicode.Is(unicode.Latin, r)
}

func isApostrophe(r rune) bool { return r == '\'' || r == '’' || r == 'ʼ' }

// fold writes a Latin word in lowercase ASCII, accents dropped and letters NFKD doesn't decompose folded ("Amélie" -> "amelie", "Straße" -> "strasse").
func fold(w string) string {
	var b strings.Builder

	for _, r := range norm.NFKD.String(strings.ToLower(w)) {
		switch {
		case r < utf8.RuneSelf && unicode.IsLetter(r):
			b.WriteRune(r)
		case r == 'ß':
			b.WriteString("ss")
		case r == 'æ':
			b.WriteString("ae")
		case r == 'œ':
			b.WriteString("oe")
		case r == 'ø':
			b.WriteByte('o')
		case r == 'ł':
			b.WriteByte('l')
		case r == 'đ', r == 'ð':
			b.WriteByte('d')
		case r == 'ı':
			b.WriteByte('i')
		case r == 'þ':
			b.WriteString("th")
		}
	}

	return b.String()
}

// write writes the pieces with read for each word, spelling initialisms out and keeping numerals. Letters of one initialism are written together where the language joins them.
func (wr *writer) write(pieces []piece, read func(string) string) string {
	var b strings.Builder

	for i, p := range pieces {
		switch {
		case p.w == nil:
			if wr.joined && i > 0 && i+1 < len(pieces) && pieces[i-1].w != nil && pieces[i-1].w.letters && pieces[i+1].w != nil && pieces[i+1].w.letters && strings.TrimSpace(p.text) == "" {
				continue
			}

			b.WriteString(p.text)
		case p.w.keep:
			b.WriteString(p.w.text)
		case p.w.letters:
			b.WriteString(wr.spell(p.w.text, i > 0 && pieces[i-1].w != nil && pieces[i-1].w.letters))
		default:
			b.WriteString(read(fold(p.w.text)))
		}
	}

	return b.String()
}

// spell writes an initialism's letters by name. after is set when it follows another initialism's letter, which a language that spaces letter names is spaced from.
func (wr *writer) spell(w string, after bool) string {
	names := make([]string, 0, len(w))

	for _, c := range []byte(fold(w)) {
		if n, ok := wr.letters[c]; ok {
			names = append(names, n)
		}
	}

	sep := " "
	if wr.joined {
		sep = ""
	}

	s := strings.Join(names, sep)
	if after && !wr.joined {
		s = " " + s
	}

	return s
}
