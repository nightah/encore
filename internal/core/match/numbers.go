package match

import "strconv"

// numbers rewrites spoken numbers as digits ("twenty one" -> "21", "second" -> "2") and joins spoken years ("nineteen eighty four" -> "1984").
func (l *language) numbers(toks []string) []string {
	out := make([]string, 0, len(toks))
	// spoken marks out entries that came from cardinal words, the only ones eligible for year joining.
	spoken := make([]bool, 0, len(toks))

	for i := 0; i < len(toks); {
		v, n, ordinal := l.parseNumber(toks, i)
		if n == 0 {
			out = append(out, toks[i])
			spoken = append(spoken, false)
			i++

			continue
		}

		if k := len(out) - 1; k >= 0 && spoken[k] && !ordinal && v >= 10 && v <= 99 {
			if prev, _ := strconv.Atoi(out[k]); prev >= 10 && prev <= 20 {
				out[k] = strconv.Itoa(prev*100 + v)
				spoken[k] = false
				i += n

				continue
			}
		}

		out = append(out, strconv.Itoa(v))
		spoken = append(spoken, !ordinal)
		i += n
	}

	return out
}

// parseNumber reads the longest number starting at toks[i], returning its value, the tokens consumed (0 if none) and whether it ended on an ordinal.
func (l *language) parseNumber(toks []string, i int) (val, n int, ordinal bool) {
	total, cur := 0, 0
	st := stStart
	j := i

loop:
	for j < len(toks) {
		w, ok := l.nums[toks[j]]
		if !ok {
			if l.conj[toks[j]] && j+1 < len(toks) && l.conjFollows(st, toks[j+1]) {
				j++

				continue
			}

			break
		}

		if w.cont && st == stStart {
			break
		}

		switch w.kind {
		case numUnit:
			if st == stUnit || (st == stTens && w.val >= 10) {
				break loop
			}

			cur += w.val
			st = stUnit
		case numCompound:
			if st == stUnit || st == stTens {
				break loop
			}

			cur += w.val
			st = stUnit
		case numTens:
			if st == stUnit || st == stTens {
				break loop
			}

			cur += w.val
			st = stTens
		case numHundred:
			g := max(cur, 1)
			if st == stTens || st == stHundred || g >= 100 {
				break loop
			}

			cur = g * 100
			st = stHundred
		case numThousand:
			if st == stThousand || total > 0 {
				break loop
			}

			total = max(cur, 1) * 1000
			cur = 0
			st = stThousand
		}

		j++

		if w.ordinal {
			return total + cur, j - i, true
		}
	}

	return total + cur, j - i, false
}

// conjFollows reports whether a conjunction in state st is part of the number, given the word after it ("hundred and one", "vingt et un").
func (l *language) conjFollows(st numState, next string) bool {
	w, ok := l.nums[next]
	if !ok {
		return false
	}

	switch st {
	case stHundred, stThousand:
		return w.kind == numUnit || w.kind == numTens || w.kind == numCompound
	case stTens:
		return w.kind == numUnit && w.val < 10
	default:
		return false
	}
}
