package match

import (
	"math/bits"
	"slices"
	"strings"
	"unicode/utf8"
)

// The similarity functions below follow RapidFuzz's definitions (fuzz.ratio, fuzz.partial_ratio, fuzz.token_set_ratio) but return values in [0, 1] instead of [0, 100].

func lcsLen(a, b string) int {
	if len(a) > len(b) {
		a, b = b, a
	}

	if a == "" {
		return 0
	}

	if len(a) <= 64 && isASCII(a) && isASCII(b) {
		var pm asciiMasks

		pm.set(a)

		return pm.lcs(len(a), b)
	}

	return lcsRunes([]rune(a), []rune(b))
}

func (pm *asciiMasks) set(p string) {
	for i := range len(p) {
		pm[p[i]] |= 1 << i
	}
}

// lcs is Hyyrö's bit-parallel LCS length of the pattern (n bytes) against s, which must be ASCII.
func (pm *asciiMasks) lcs(n int, s string) int {
	v := ^uint64(0)

	for i := range len(s) {
		u := v & pm[s[i]]
		v = (v + u) | (v - u)
	}

	mask := ^uint64(0)
	if n < 64 {
		mask = 1<<n - 1
	}

	return bits.OnesCount64(^v & mask)
}

func lcsRunes(a, b []rune) int {
	if len(a) < len(b) {
		a, b = b, a
	}

	if len(b) == 0 {
		return 0
	}

	row := make([]int, len(b)+1)

	for _, ra := range a {
		diag := 0
		for j, rb := range b {
			up := row[j+1]
			if ra == rb {
				row[j+1] = diag + 1
			} else if row[j] > up {
				row[j+1] = row[j]
			}

			diag = up
		}
	}

	return row[len(b)]
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}

	return true
}

// indelRatio is the normalised Indel similarity from an LCS length: 1 - (insertions + deletions) / (la + lb).
func indelRatio(lcs, la, lb int) float64 {
	if la+lb == 0 {
		return 1
	}

	return 2 * float64(lcs) / float64(la+lb)
}

// ratio is fuzz.ratio.
func ratio(a, b string) float64 {
	if isASCII(a) && isASCII(b) {
		return indelRatio(lcsLen(a, b), len(a), len(b))
	}

	ra, rb := []rune(a), []rune(b)

	return indelRatio(lcsRunes(ra, rb), len(ra), len(rb))
}

// partialRatio is fuzz.partial_ratio: the best ratio of the shorter string against any equally long window of the longer, including windows that hang off either end.
func partialRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) > len(rb) {
		ra, rb = rb, ra
	}

	if len(ra) == 0 {
		if len(rb) == 0 {
			return 1
		}

		return 0
	}

	best := slideWindows(ra, rb)
	if len(ra) == len(rb) {
		best = max(best, slideWindows(rb, ra))
	}

	return best
}

// slideWindows scores short against windows of long. Only windows whose outer character occurs in short can be optimal, which RapidFuzz exploits too.
func slideWindows(short, long []rune) float64 {
	n, m := len(short), len(long)

	lcs := func(w []rune) int { return lcsRunes(short, w) }

	if n <= 64 && asciiRunes(short) && asciiRunes(long) {
		var pm asciiMasks
		for i, r := range short {
			pm[r] |= 1 << i
		}

		lcs = func(w []rune) int {
			v := ^uint64(0)
			for _, r := range w {
				u := v & pm[r]
				v = (v + u) | (v - u)
			}

			mask := ^uint64(0)
			if n < 64 {
				mask = 1<<n - 1
			}

			return bits.OnesCount64(^v & mask)
		}
	}

	in := func(r rune) bool { return slices.Contains(short, r) }
	best := 0.0

	try := func(w []rune) bool {
		best = max(best, indelRatio(lcs(w), n, len(w)))

		return best == 1
	}

	for i := 1; i < n && i <= m; i++ {
		if in(long[i-1]) && try(long[:i]) {
			return 1
		}
	}

	for i := 0; i <= m-n; i++ {
		if in(long[i+n-1]) && try(long[i:i+n]) {
			return 1
		}
	}

	for i := max(m-n+1, 0); i < m; i++ {
		if in(long[i]) && try(long[i:]) {
			return 1
		}
	}

	return best
}

func asciiRunes(rs []rune) bool {
	for _, r := range rs {
		if r >= utf8.RuneSelf {
			return false
		}
	}

	return true
}

// tokenSetRatio is fuzz.token_set_ratio over pre-split tokens.
func tokenSetRatio(a, b []string) float64 {
	sa, sb := uniqueSorted(a), uniqueSorted(b)
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}

	var inter, diffAB, diffBA []string

	i, j := 0, 0
	for i < len(sa) || j < len(sb) {
		switch {
		case j == len(sb) || (i < len(sa) && sa[i] < sb[j]):
			diffAB = append(diffAB, sa[i])
			i++
		case i == len(sa) || sb[j] < sa[i]:
			diffBA = append(diffBA, sb[j])
			j++
		default:
			inter = append(inter, sa[i])
			i++
			j++
		}
	}

	if len(inter) > 0 && (len(diffAB) == 0 || len(diffBA) == 0) {
		return 1
	}

	ab, ba := strings.Join(diffAB, " "), strings.Join(diffBA, " ")
	abLen, baLen := runeLen(ab), runeLen(ba)
	if len(inter) == 0 {
		return ratio(ab, ba)
	}

	sectLen := runeLen(strings.Join(inter, " "))
	sectABLen, sectBALen := sectLen+1+abLen, sectLen+1+baLen

	// Comparing "sect ab" with "sect ba" costs only the diffs' Indel distance, normalised by the full lengths.
	dist := abLen + baLen - 2*lcsLen(ab, ba)
	result := 1 - float64(dist)/float64(sectABLen+sectBALen)

	// sect is a prefix of "sect ab", so their distance is just the length difference.
	sectAB := 1 - float64(1+abLen)/float64(sectLen+sectABLen)
	sectBA := 1 - float64(1+baLen)/float64(sectLen+sectBALen)

	return max(result, sectAB, sectBA)
}

func uniqueSorted(toks []string) []string {
	if slices.IsSortedFunc(toks, cmpStrict) {
		return toks
	}

	out := slices.Clone(toks)
	slices.Sort(out)

	return slices.Compact(out)
}

// cmpStrict orders strings and treats equal ones as out of order, so IsSortedFunc reports strictly increasing (sorted and unique) input.
func cmpStrict(a, b string) int {
	if a > b {
		return 1
	}

	return -1
}

func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
