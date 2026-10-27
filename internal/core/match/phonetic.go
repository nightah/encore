package match

import (
	"slices"
	"strings"

	dm "github.com/PhonoGrams/double_metaphone"
	"github.com/f1monkey/phonetic/cologne"
)

// metaphoneKeys uses uncapped Double Metaphone, returning the primary and, when different, the alternate code.
func metaphoneKeys(tok string) []string {
	if isDigits(tok) {
		return []string{tok}
	}

	p, a := dm.EncodeUncapped(tok)

	keys := make([]string, 0, 3)
	for _, k := range []string{p, a} {
		if k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}

	if len(keys) == 0 {
		return []string{tok}
	}

	// Non-rhotic speakers make "Peppa" and "pepper" sound alike, so a word ending in a vowel and R gets an R-less variant.
	if len(keys[0]) > 1 && strings.HasSuffix(keys[0], "R") && len(tok) > 2 && tok[len(tok)-1] == 'r' && strings.IndexByte("aeiouy", tok[len(tok)-2]) >= 0 {
		if k := strings.TrimSuffix(keys[0], "R"); !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}

	return keys
}

// cologne is stateless, so one encoder is shared.
var cologneEncoder = cologne.NewEncoder()

// cologneKeys uses Kölner Phonetik, which suits German spelling far better than Metaphone.
func cologneKeys(tok string) []string {
	if isDigits(tok) {
		return []string{tok}
	}

	if k := cologneEncoder.Encode(tok); k != "" {
		return []string{k}
	}

	return []string{tok}
}

func (l *language) phoneticKeys(toks []string) [][]string {
	out := make([][]string, len(toks))
	for i, t := range toks {
		out[i] = l.phonetic(t)
	}

	return out
}
