package match

import (
	"slices"
)

// queryTokens are the tokens worth searching for: stopwords only add noise unless they are all there is.
func (l *language) queryTokens(q *form) []string {
	out := make([]string, 0, len(q.toks))
	for _, t := range q.toks {
		if !l.stop[t] && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}

	if len(out) == 0 {
		return q.toks
	}

	return out
}

// queryKeys are the distinct phonetic keys of the query terms, then of the whole query with spaces removed.
func queryKeys(q *form, terms []string) []string {
	keys := make([]string, 0, len(terms)+1)

	for i, t := range q.toks {
		if !slices.Contains(terms, t) {
			continue
		}

		for _, k := range q.keys[i] {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}

	for _, k := range q.compactKeys {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}

	return keys
}
