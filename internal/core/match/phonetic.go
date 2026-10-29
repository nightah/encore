package match

import (
	"strings"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	"github.com/nightah/encore/internal/fuzzy/phonetic/de"
)

// phoneticKeysOf adapts an internal/fuzzy encoder to a phoneticFunc. Languages name their encoder rather than resolving one with phonetic.For, so which encoder keys a language never depends on which language packages happen to be linked in.
func phoneticKeysOf(enc phonetic.Encoder) phoneticFunc {
	return func(tok string) []string { return enc.Keys(make([]string, 0, 3), tok) }
}

// germanKeys keys German words with Kölner Phonetik after a final "-er" of a word of four or more letters loses its r. Spoken German makes that ending a vowel, and speech recognition writes it as one ("lieda" for "Lieder", "es war somma"), which Kölner Phonetik keys without the r's 7. The rule lives here rather than in the encoder, which keeps the library's codes.
func germanKeys(enc de.Encoder) phoneticFunc {
	return func(tok string) []string {
		if len(tok) >= 4 && strings.HasSuffix(tok, "er") {
			tok = tok[:len(tok)-1]
		}

		return enc.Keys(make([]string, 0, 1), tok)
	}
}

func (l *language) phoneticKeys(toks []string) [][]string {
	out := make([][]string, len(toks))
	for i, t := range toks {
		out[i] = l.phonetic(t)
	}

	return out
}
