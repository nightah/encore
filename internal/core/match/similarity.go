package match

import "slices"

// Jaro-Winkler and optimal string alignment distance work on runes, so an accented or non-Latin letter counts as one character. Strings up to stackRunes runes are decoded into stack buffers, which keeps scoring free of allocations.

// jaroWinkler is the Jaro similarity of a and b, in [0, 1], raised by a tenth of the remaining gap per shared leading rune (at most 4). Two empty strings score 0. It computes in float32 so scores, and the thresholds tuned on them, match the values in similarity_test.go.
func jaroWinkler(a, b string) float64 {
	var ab, bb [stackRunes]rune

	s, t := appendRunes(ab[:0], a), appendRunes(bb[:0], b)

	j := jaro(s, t)
	if j == 0 || j == 1 {
		return float64(j)
	}

	prefix := 0
	for prefix < min(len(s), len(t), 4) && s[prefix] == t[prefix] {
		prefix++
	}

	return float64(j + 0.1*float32(prefix)*(1-j))
}

func jaro(s, t []rune) float32 {
	if len(s) == 0 || len(t) == 0 {
		return 0
	}

	if slices.Equal(s, t) {
		return 1
	}

	var sb, tb [stackRunes]bool

	sm, tm := flags(sb[:], len(s)), flags(tb[:], len(t))

	// Runes match when equal and no further apart than half the longer string, less one.
	window := max(len(s), len(t))/2 - 1
	m := 0

	for i, r := range s {
		for j := max(0, i-window); j < min(len(t), i+window+1); j++ {
			if !tm[j] && r == t[j] {
				sm[i], tm[j] = true, true
				m++

				break
			}
		}
	}

	if m == 0 {
		return 0
	}

	// Half the matched runes that appear in a different order are transpositions.
	var half float32

	p := 0

	for i, r := range s {
		if !sm[i] {
			continue
		}

		for !tm[p] {
			p++
		}

		if r != t[p] {
			half++
		}

		p++
	}

	half /= 2

	fm := float32(m)

	return (fm/float32(len(s)) + fm/float32(len(t)) + (fm-half)/fm) / 3.0
}

// osaDistance is the optimal string alignment distance between a and b: the fewest rune insertions, deletions, substitutions and swaps of adjacent runes that turn one into the other, editing each substring at most once.
func osaDistance(a, b string) int {
	var ab, bb [stackRunes]rune

	s, t := appendRunes(ab[:0], a), appendRunes(bb[:0], b)

	// Rows run over the longer string so the three rolling rows are as short as possible.
	if len(s) < len(t) {
		s, t = t, s
	}

	n := len(t)

	switch {
	case n == 0:
		return len(s)
	case slices.Equal(s, t):
		return 0
	}

	var buf [3 * (stackRunes + 1)]int

	rows := buf[:]
	if 3*(n+1) > len(rows) {
		rows = make([]int, 3*(n+1))
	}

	// prev2 is two rows up, for swaps.
	prev2, prev, cur := rows[:n+1], rows[n+1:2*(n+1)], rows[2*(n+1):3*(n+1)]

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(s); i++ {
		cur[0] = i

		for j := 1; j <= n; j++ {
			cost := 1
			if s[i-1] == t[j-1] {
				cost = 0
			}

			d := min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && s[i-1] == t[j-2] && s[i-2] == t[j-1] {
				d = min(d, prev2[j-2]+1)
			}

			cur[j] = d
		}

		prev2, prev, cur = prev, cur, prev2
	}

	return prev[n]
}

func appendRunes(dst []rune, s string) []rune {
	for _, r := range s {
		dst = append(dst, r)
	}

	return dst
}

// flags returns n cleared flags, from buf when it is big enough.
func flags(buf []bool, n int) []bool {
	if n <= len(buf) {
		return buf[:n]
	}

	return make([]bool, n)
}

// osaWithin is the optimal string alignment distance between a and b when it is at most maxD, else -1. It gives up as soon as a whole row exceeds maxD.
func osaWithin(a []rune, b string, maxD int) int {
	var bb [stackRunes]rune

	t := appendRunes(bb[:0], b)
	if len(a)-len(t) > maxD || len(t)-len(a) > maxD {
		return -1
	}

	n := len(t)

	var buf [3 * (stackRunes + 1)]int

	rows := buf[:]
	if 3*(n+1) > len(rows) {
		rows = make([]int, 3*(n+1))
	}

	prev2, prev, cur := rows[:n+1], rows[n+1:2*(n+1)], rows[2*(n+1):3*(n+1)]

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		cur[0] = i
		best := i

		for j := 1; j <= n; j++ {
			cost := 1
			if a[i-1] == t[j-1] {
				cost = 0
			}

			d := min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == t[j-2] && a[i-2] == t[j-1] {
				d = min(d, prev2[j-2]+1)
			}

			cur[j] = d
			best = min(best, d)
		}

		// A swap reaches back two rows, so a row can only be abandoned once the one before it is past maxD too.
		if best > maxD && (i == 1 || slices.Min(prev) > maxD) {
			return -1
		}

		prev2, prev, cur = prev, cur, prev2
	}

	if d := prev[n]; d <= maxD {
		return d
	}

	return -1
}
