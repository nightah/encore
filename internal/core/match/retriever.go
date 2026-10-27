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
