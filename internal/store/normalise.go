package store

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Normalise folds a spoken name for comparison: case and accents are dropped, apostrophes removed, other punctuation treated as spaces and whitespace collapsed. "Kid's  Room", "kids room" and "KIDS-ROOM" all normalise to "kids room". It is locale-insensitive on purpose: Alexa's transcription of the same room varies more than any locale rule would fix.
func Normalise(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	space := false

	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// Combining marks left over from decomposing accented letters.
		case r == '\'' || r == '’' || r == '‘':
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}

			space = false

			b.WriteRune(unicode.ToLower(r))
		default:
			space = true
		}
	}

	return b.String()
}
