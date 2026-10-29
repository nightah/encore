package match

import (
	"slices"
	"strconv"
	"strings"
)

// numbers rewrites spoken numbers as digits ("twenty one" -> "21", "second" -> "2") and joins spoken years ("nineteen eighty four" -> "1984").
func (l *language) numbers(toks []string) []string {
	toks = l.splitFusedAll(toks)
	out := make([]string, 0, len(toks))
	// spoken marks out entries that came from cardinal words, the only ones eligible for year joining.
	spoken := make([]bool, 0, len(toks))

	for i := 0; i < len(toks); {
		v, n, ordinal := l.parseNumber(toks, i)
		if n == 0 {
			out = append(out, l.ordinalDigits(toks[i]))
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
	mill, total, cur := 0, 0, 0
	st := stStart
	j := i

loop:
	for j < len(toks) {
		w, ok := l.nums[toks[j]]
		if !ok {
			if l.conj[toks[j]] && j+1 < len(toks) && l.conjFollows(st, cur, toks[j+1]) {
				if st == stUnit {
					st = stUnitAnd
				}

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
			// From stUnitAnd the unit is already in cur ("neun und neunzig"), so the tens adds to it.
			if st == stUnit || st == stTens {
				break loop
			}

			cur += w.val
			st = stTens
		case numHundred:
			g := max(cur, 1)

			switch {
			case w.val > 100:
				// A whole number of hundreds ("duecento") starts a group of its own.
				if cur > 0 {
					break loop
				}

				cur = w.val
			case st == stTens || st == stHundred || g >= 100:
				break loop
			default:
				cur = g * 100
			}

			st = stHundred
		case numThousand:
			if st == stThousand || total > 0 {
				break loop
			}

			total = max(cur, 1) * 1000
			cur = 0
			st = stThousand
		case numMillion:
			if mill > 0 || total > 0 {
				break loop
			}

			mill = max(cur, 1) * 1_000_000
			cur = 0
			st = stMillion
		}

		j++

		if w.ordinal {
			return mill + total + cur, j - i, true
		}
	}

	return mill + total + cur, j - i, false
}

// conjFollows reports whether a conjunction in state st, with cur read so far, is part of the number, given the word after it ("hundred and one", "vingt et un", "neun und neunzig").
func (l *language) conjFollows(st numState, cur int, next string) bool {
	w, ok := l.nums[next]
	if !ok {
		return false
	}

	switch st {
	case stHundred, stThousand, stMillion:
		return w.kind == numUnit || w.kind == numTens || w.kind == numCompound
	case stTens:
		return w.kind == numUnit && w.val < 10
	case stUnit:
		return l.unitsFirst && w.kind == numTens && cur%100 < 10
	default:
		return false
	}
}

// splitFusedAll replaces each number written as one word in toks with the number words it is made of (splitFused). It returns toks itself when there are none.
func (l *language) splitFusedAll(toks []string) []string {
	if !l.fused {
		return toks
	}

	var out []string

	for i, tok := range toks {
		pieces := l.splitFused(tok)
		if pieces == nil {
			if out != nil {
				out = append(out, tok)
			}

			continue
		}

		if out == nil {
			out = append(make([]string, 0, len(toks)+len(pieces)), toks[:i]...)
		}

		out = append(out, pieces...)
	}

	if out == nil {
		return toks
	}

	return out
}

// splitFused splits a token made only of number words ("millenovecentonovantadue", "centottanta", "ottoottotre") into those words, in the fewest pieces, or returns nil. Every piece must be read as part of a number, so a conjunction or an article-like "un" cannot start one, and a word that merely begins with a number word ("treno") stays whole.
func (l *language) splitFused(tok string) []string {
	n := len(tok)
	if n > maxFused {
		return nil
	}

	if _, ok := l.nums[tok]; ok {
		return nil
	}

	// fewest[i] is the fewest pieces tok[i:] splits into, or 0 if it cannot be; end[i] is where the first of them ends.
	var fewest, end [maxFused + 1]uint8

	for i := n - 1; i >= 0; i-- {
		for j := i + 1; j <= n; j++ {
			if j < n && fewest[j] == 0 {
				continue
			}

			if _, ok := l.fusedPiece(tok[i:j], tok[j:]); ok && (fewest[i] == 0 || fewest[j]+1 < fewest[i]) {
				fewest[i], end[i] = fewest[j]+1, uint8(j)
			}
		}
	}

	if fewest[0] < 2 {
		return nil
	}

	pieces := make([]string, 0, fewest[0])
	for i := 0; i < n; i = int(end[i]) {
		p, _ := l.fusedPiece(tok[i:end[i]], tok[end[i]:])
		pieces = append(pieces, p)
	}

	for i := 0; i < len(pieces); {
		_, k, _ := l.parseNumber(pieces, i)
		if k == 0 {
			return nil
		}

		i += k
	}

	return pieces
}

// fusedPiece reports whether s can be one piece of a number written as one word, given what follows it, and returns the number word it stands for. Ordinals are left out: they end a number and are rarely joined to one.
func (l *language) fusedPiece(s, rest string) (string, bool) {
	if w, ok := l.nums[s]; ok {
		return s, !w.ordinal
	}

	if rest == "" {
		return "", false
	}

	if l.conj[s] {
		return s, true
	}

	if full, ok := l.elided[s]; ok && strings.IndexByte("aeiou", rest[0]) >= 0 {
		return full, true
	}

	return "", false
}

// ordinalDigits returns the digits of an ordinal written with them ("6th" -> "6", "5eme" -> "5"), or tok unchanged.
func (l *language) ordinalDigits(tok string) string {
	d := strings.IndexFunc(tok, func(r rune) bool { return r < '0' || r > '9' })
	if d <= 0 {
		return tok
	}

	if slices.Contains(l.ordSuffixes, tok[d:]) {
		return tok[:d]
	}

	return tok
}
