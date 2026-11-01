package match

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Normalize returns the canonical form of s for the given language or locale: folded to lowercase ASCII-ish letters and digits, punctuation dropped, leading articles removed and spoken numbers written as digits. Titles and queries are compared in this form.
func Normalize(lang, s string) string {
	return strings.Join(languageFor(lang).normalize(s, false), " ")
}

// normalize runs the pipeline and returns tokens. aggressive also converts single-letter roman numerals ("Rocky V"), which Normalize leaves alone so "Malcolm X" survives.
func (l *language) normalize(s string, aggressive bool) []string {
	toks, _ := l.normalizeSpelled(s, aggressive)

	return toks
}

// normalizeSpelled also returns the tokens before numbers were rewritten, joined without spaces, so compounds such as "Zweiohrküken" still meet "zwei ohr küken".
func (l *language) normalizeSpelled(s string, aggressive bool) (toks []string, spelled string) {
	toks = joinLetters(l.spellLetters(l.scriptWords(l.tokenize(s))))

	// Articles go first so a numeral straight after one ("The II") isn't taken as following a word.
	for len(toks) > 1 && l.articles[toks[0]] {
		toks = toks[1:]
	}

	spelled = strings.Join(toks, "")
	toks = romans(toks, aggressive)
	toks = l.numbers(toks)

	return l.dropParts(toks), spelled
}

func (l *language) tokenize(s string) []string {
	// NFKD would make "8½" "81⁄2", so ½ is read as words first.
	s = strings.ReplaceAll(s, "½", " "+l.half+" ")
	if l.respell != nil {
		s = l.respell.Replace(s)
	}

	b := make([]byte, 0, len(s))

	// cluster is where the last letter of a script that keeps its marks starts in b, or -1; kept is set once a mark follows it, so the letter and its marks are recomposed (か and U+3099 back to が).
	cluster, kept := -1, false

	for _, r := range norm.NFKD.String(s) {
		if unicode.Is(unicode.M, r) {
			switch {
			case cluster >= 0:
				b = utf8.AppendRune(b, r)
				kept = true
			case !unicode.Is(unicode.Mn, r):
				b = append(b, ' ')
			}

			continue
		}

		if kept {
			b = append(b[:cluster], norm.NFC.Bytes(b[cluster:])...)
		}

		cluster, kept = -1, false

		switch {
		case r == tatweel:
		case r == '&':
			b = append(b, " "+l.and+" "...)
		case isApostrophe(r):
			if l.elide {
				b = append(b, ' ')
			}
		case unicode.IsDigit(r):
			b = utf8.AppendRune(b, asciiDigit(r))
		case unicode.IsLetter(r), unicode.Is(unicode.Nl, r):
			if keepsMarks(r) {
				cluster = len(b)
			}

			r = unicode.ToLower(r)
			if f, ok := l.folds[r]; ok {
				b = append(b, f...)
			} else if f, ok := letterFolds[r]; ok {
				b = append(b, f...)
			} else if f, ok := arabicFolds[r]; ok {
				// Arabic is folded in every language's library, so Arabic text meets itself and the Arabic rules scriptWords applies.
				b = append(b, f...)
			} else {
				b = utf8.AppendRune(b, r)
			}
		default:
			b = append(b, ' ')
		}
	}

	if kept {
		b = append(b[:cluster], norm.NFC.Bytes(b[cluster:])...)
	}

	return strings.Fields(string(b))
}

// keepsMarks reports whether the combining marks after a letter are part of its spelling. Latin, Greek and Cyrillic marks are accents and Arabic and Hebrew ones are vowel points, which titles and speech recognition leave out as often as not, so they are dropped. In Devanagari and the other Indic scripts marks are vowels, nasalisation and virama, and in kana they voice the consonant (か, が), so they stay.
func keepsMarks(r rune) bool {
	if r < 0x0370 {
		return false
	}

	return !unicode.In(r, unicode.Latin, unicode.Greek, unicode.Cyrillic, unicode.Arabic, unicode.Hebrew, unicode.Common, unicode.Inherited)
}

// asciiDigit folds a decimal digit of any script ("٣", "७") to its ASCII digit. Unicode encodes each script's digits as a run of ten from zero, some scripts' runs back to back, so a digit's value is its offset from the start of the unbroken run of digits it is in, modulo ten.
func asciiDigit(r rune) rune {
	if r <= '9' {
		return r
	}

	lo := r
	for unicode.IsDigit(lo - 1) {
		lo--
	}

	return '0' + (r-lo)%10
}

// scriptWords breaks tokens into words and writes them in Latin letters. Runs of tokens in a script another language reads (Japanese, Devanagari, Arabic) go through that language's rules, whatever the library's language, so a Japanese request meets a Latin or Japanese title in an English library; a run of tokens of digits alone stays with the run before it. Everything else goes through this language's own rules, so a library in Japanese, Hindi or Arabic reads its own script as before.
func (l *language) scriptWords(toks []string) []string {
	toks = l.scriptPieces(toks)
	out := make([]string, 0, len(toks))

	var owner *language

	from := 0

	for i, tok := range toks {
		o := l.foreignOwner(tok)

		if o == nil && i > 0 && isDigits(tok) {
			o = owner
		}

		if i > 0 && o != owner {
			out = append(out, l.words(owner, toks[from:i], from == 0)...)
			from = i
		}

		owner = o
	}

	return append(out, l.words(owner, toks[from:], from == 0)...)
}

// words is a run of tokens through the rules of the language whose script they are in (o), or through this language's own when o is nil. A foreign run also has its own articles (when it starts the text), number words and parts handled, which this language does not know, and then this language's hooks.
func (l *language) words(o *language, toks []string, first bool) []string {
	if o == nil {
		return l.ownWords(toks)
	}

	toks = o.foreignWords(toks)
	for first && len(toks) > 1 && o.articles[toks[0]] {
		toks = toks[1:]
	}

	// This language's own hooks then see the Latin words as they would on a second pass, which keeps normalising idempotent.
	return l.ownWords(o.dropParts(o.numbers(toks)))
}

// ownWords splits, strips fused articles from and romanizes tokens with this language's hooks.
func (l *language) ownWords(toks []string) []string {
	return l.splitWords(toks, l.romanize)
}

// foreignWords is ownWords for tokens in this language's script read in another language's library, romanized with romanizeForeign when the language has one.
func (l *language) foreignWords(toks []string) []string {
	if l.romanizeForeign != nil {
		return l.splitWords(toks, l.romanizeForeign)
	}

	return l.ownWords(toks)
}

func (l *language) splitWords(toks []string, romanize func(string) string) []string {
	if l.split != nil {
		toks = l.split(toks)
	}

	return romanizeAll(l.stripPrefixes(toks), romanize)
}

// scriptPieces splits a token where a script another language reads starts or ends ("abcदंगल" -> "abc" "दंगल"), so each piece goes through one language's rules. Digits, marks and anything else no language reads its own way stay with the piece before them; a token in no other language's script is left whole.
func (l *language) scriptPieces(toks []string) []string {
	out := make([]string, 0, len(toks))

	for _, tok := range toks {
		var owner *language

		from, started := 0, false

		for j, r := range tok {
			o := l.foreignRune(r)
			if o == nil && !unicode.IsLetter(r) {
				continue
			}

			if started && o != owner {
				out = append(out, tok[from:j])
				from = j
			}

			owner, started = o, true
		}

		out = append(out, tok[from:])
	}

	return out
}

// foreignOwner is the language whose rules read the script tok is in, when that is not this language, or nil.
func (l *language) foreignOwner(tok string) *language {
	for _, r := range tok {
		if o := l.foreignRune(r); o != nil {
			return o
		}
	}

	return nil
}

// foreignRune is the language whose rules read r's script, when that is not this language, or nil.
func (l *language) foreignRune(r rune) *language {
	// Arabic, the first of these scripts, starts at U+0600, so Latin, Greek and Cyrillic text skips the lookups.
	if r < 0x0600 {
		return nil
	}

	var o *language

	switch {
	case isJapanese(r):
		o = languages["ja"]
	case isDevanagari(r):
		o = languages["hi"]
	case isArabicLetter(r):
		o = languages["ar"]
	}

	if o == l {
		return nil
	}

	return o
}

// romanizeAll writes each token as romanize writes it, dropping a token that romanizes to nothing and splitting one that romanizes to several words. A nil romanize leaves the tokens as they are.
func romanizeAll(toks []string, romanize func(string) string) []string {
	if romanize == nil {
		return toks
	}

	out := make([]string, 0, len(toks))
	for _, tok := range toks {
		out = append(out, strings.Fields(romanize(tok))...)
	}

	return out
}

// stripPrefixes removes the longest of the language's articlePrefixes from each token that keeps at least articlePrefixMin runes, and always at least one, without it.
func (l *language) stripPrefixes(toks []string) []string {
	if len(l.articlePrefixes) == 0 {
		return toks
	}

	least := max(l.articlePrefixMin, 1)

	for i, tok := range toks {
		cut := 0

		for _, p := range l.articlePrefixes {
			if len(p) > cut && strings.HasPrefix(tok, p) && utf8.RuneCountInString(tok[len(p):]) >= least {
				cut = len(p)
			}
		}

		toks[i] = tok[cut:]
	}

	return toks
}

// letterFolds covers letters NFKD does not decompose.
var letterFolds = map[rune]string{
	'ß': "ss", 'æ': "ae", 'œ': "oe", 'ø': "o", 'đ': "d", 'ł': "l", 'þ': "th", 'ð': "d", 'ı': "i",
}

func isApostrophe(r rune) bool {
	switch r {
	case '\'', '’', '‘', '`', '´', 'ʼ':
		return true
	}

	return false
}

// umlautVariant spells German umlauts out (ü -> ue) so both spellings of a title can be found.
func umlautVariant(s string) string {
	return umlautReplacer.Replace(s)
}

var umlautReplacer = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "Ae", "Ö", "Oe", "Ü", "Ue")

// spellLetters writes each run of letter names that reads as a spelled acronym as its letters (see language.letterNames), for joinLetters to merge.
func (l *language) spellLetters(toks []string) []string {
	if l.letterNames == nil {
		return toks
	}

	isName := func(tok string) bool { _, ok := l.letterNames[tok]; return ok || isSingleLetter(tok) }

	for i := 0; i < len(toks); {
		j := i
		for j < len(toks) && isName(toks[j]) {
			j++
		}

		start := i
		if start > 0 && j-start > 2 && l.letterWords[toks[start]] && l.spells(toks[start+1:j]) {
			start++
		}

		if l.spells(toks[start:j]) {
			for k := start; k < j; k++ {
				if letter, ok := l.letterNames[toks[k]]; ok {
					toks[k] = letter
				}
			}
		}

		i = max(j, i+1)
	}

	return toks
}

// spells reports whether a run of letter names reads as a spelled acronym: two or more names that are no other word, or three or more names none of which is a single letter.
func (l *language) spells(run []string) bool {
	names, letters := 0, 0

	for _, tok := range run {
		switch {
		case len(tok) == 1:
			letters++
		case !l.letterWords[tok]:
			names++
		}
	}

	return len(run) >= 2 && names >= 2 || len(run) >= 3 && letters == 0
}

// joinLetters merges runs of two or more single-letter tokens, so "E.T." and "M*A*S*H" become "et" and "mash".
func joinLetters(toks []string) []string {
	out := toks[:0:0]

	for i := 0; i < len(toks); {
		j := i
		for j < len(toks) && isSingleLetter(toks[j]) {
			j++
		}

		if j-i >= 2 {
			out = append(out, strings.Join(toks[i:j], ""))
			i = j

			continue
		}

		out = append(out, toks[i])
		i++
	}

	return out
}

func isSingleLetter(s string) bool {
	return len(s) == 1 && s[0] >= 'a' && s[0] <= 'z'
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}

	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

// romanValues are the numerals I to XX in canonical form.
var romanValues = func() map[string]int {
	numerals := []string{"i", "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x", "xi", "xii", "xiii", "xiv", "xv", "xvi", "xvii", "xviii", "xix", "xx"}

	m := make(map[string]int, len(numerals))
	for i, n := range numerals {
		m[n] = i + 1
	}

	return m
}()

// romans converts standalone numerals that follow another word. Single letters are only converted when aggressive.
func romans(toks []string, aggressive bool) []string {
	for i := 1; i < len(toks); i++ {
		if v, ok := romanValues[toks[i]]; ok && (aggressive || len(toks[i]) > 1) {
			toks[i] = strconv.Itoa(v)
		}
	}

	return toks
}

// dropParts removes "part", "chapter" and friends before a number, working right to left so "part part 2" settles in one pass.
func (l *language) dropParts(toks []string) []string {
	out := make([]string, 0, len(toks))

	for i := len(toks) - 1; i >= 0; i-- {
		if l.parts[toks[i]] && len(out) > 0 && isDigits(out[len(out)-1]) {
			continue
		}

		out = append(out, toks[i])
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}

	return out
}

// StopWord reports whether word is one of the language's articles or stop words, such as "the", "of" or Japanese "の", which a title search treats as filler.
func StopWord(lang, word string) bool {
	l := languageFor(lang)
	toks := l.normalize(word, false)

	return len(toks) == 1 && (l.stop[toks[0]] || l.articles[toks[0]])
}

// Article reports whether word is one of the language's articles, such as "the" or "la", which may start a title.
func Article(lang, word string) bool {
	l := languageFor(lang)
	toks := l.normalize(word, false)

	return len(toks) == 1 && l.articles[toks[0]]
}
