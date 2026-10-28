package match

import "github.com/nightah/encore/internal/fuzzy/phonetic"

// phoneticKeysOf adapts an internal/fuzzy encoder to a phoneticFunc. Languages name their encoder rather than resolving one with phonetic.For, so which encoder keys a language never depends on which language packages happen to be linked in.
func phoneticKeysOf(enc phonetic.Encoder) phoneticFunc {
	return func(tok string) []string { return enc.Keys(make([]string, 0, 3), tok) }
}

func (l *language) phoneticKeys(toks []string) [][]string {
	out := make([][]string, len(toks))
	for i, t := range toks {
		out[i] = l.phonetic(t)
	}

	return out
}
