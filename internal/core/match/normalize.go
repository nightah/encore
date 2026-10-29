package match

import (
	"strconv"
	"strings"
	"unicode"

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
	toks = joinLetters(l.tokenize(s))

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
	var b strings.Builder

	// NFKD would make "8½" "81⁄2", so ½ is read as words first.
	s = strings.ReplaceAll(s, "½", " "+l.half+" ")

	b.Grow(len(s))

	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == '&':
			b.WriteString(" " + l.and + " ")
		case isApostrophe(r):
			if l.elide {
				b.WriteByte(' ')
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			r = unicode.ToLower(r)
			if f, ok := letterFolds[r]; ok {
				b.WriteString(f)
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteByte(' ')
		}
	}

	return strings.Fields(b.String())
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
