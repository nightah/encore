package match

import (
	"slices"
	"strings"
)

// scoreAlias rates how well alias a matches query q, in [0, 1]. It gives up early, returning a negative score, once the alias provably can't beat floor.
func (ix *Index) scoreAlias(q *form, a *alias, floor float64) scored {
	if !a.derived && q.norm == a.norm {
		return scored{score: 1, exact: true, bd: Breakdown{Alias: a.text, TokenSet: 1, JaroWinkler: 1, OSA: 1, Phonetic: 1}}
	}

	compact := ratio(q.compact, a.compact)
	// Whole-string shortcuts ignore word boundaries, and phonetic keys ignore digits, so they only apply when the numbers agree: "toy story 4" is not "Toy Story".
	numbers := numbersAgree(q, &a.form)
	// A query number the alias does not carry marks a missing sequel ("crocodile dundee 2" / "Crocodile Dundee").
	numbersDiffer := !numbers && !digitsWithin(q, &a.form)

	if q.spelled != "" || a.spelled != "" {
		// Matching as spelled out means the numbers were words in both ("zwei ohr küken" / "Zweiohrküken").
		if spelled := ratio(q.spelledCompact(), a.spelledCompact()); spelled > compact {
			compact = spelled
			numbers = numbers || spelled >= ix.cfg.CompactCoverage
			// A number word that sounds like a title word ("tears four fears") is no sequel, but a written digit still is ("uno 2" / "uno").
			numbersDiffer = numbersDiffer && (!numbers || strings.ContainsAny(q.spelledCompact(), "0123456789"))
		}
	}

	s := max(tokenSetRatio(q.toks, a.toks), compact)

	// Sounding the same as a whole is the typical ASR failure, so it counts as near-identical text.
	homophone := numbers && keysIntersect(q.compactKeys, a.compactKeys)
	soundAlike := homophone && q.compact != a.compact

	if homophone {
		s = max(s, homophoneSet)
	}

	partial := len(q.norm) < len(a.norm) && s < partialFactor

	upper := wSet*s + wJaro + wOSA + wPhonetic
	if partial {
		upper = wSet*partialFactor + wJaro + wOSA + wPhonetic
	}

	if a.derived {
		upper *= ix.cfg.DerivedPenalty
	}

	if upper <= floor {
		return scored{score: -1}
	}

	if partial {
		s = max(s, partialFactor*partialRatio(q.norm, a.norm))
	}

	jw := jaroWinkler(q.norm, a.norm)
	osa := osaSimilarity(q.norm, a.norm)

	if q.sorted != "" || a.sorted != "" {
		qs, as := q.sortedNorm(), a.sortedNorm()
		jw = max(jw, jaroWinkler(qs, as))
		osa = max(osa, osaSimilarity(qs, as))
	}

	ph := phoneticOverlap(q, &a.form, homophone)

	f := wSet*s + wJaro*jw + wOSA*osa + wPhonetic*ph

	cov := 1.0
	if (compact < ix.cfg.CompactCoverage || !numbers) && !homophone {
		cov = coverage(q, &a.form, ix.lang.unspaced)
	}

	f *= 1 - ix.cfg.CoveragePenalty*(1-cov)

	if a.derived {
		f *= ix.cfg.DerivedPenalty
	}

	return scored{
		score:         min(f, nonExactCap),
		soundAlike:    soundAlike,
		numbersDiffer: numbersDiffer,
		bd:            Breakdown{Alias: a.text, TokenSet: s, JaroWinkler: jw, OSA: osa, Phonetic: ph},
	}
}

func osaSimilarity(a, b string) float64 {
	n := max(runeLen(a), runeLen(b))
	if n == 0 {
		return 1
	}

	return 1 - float64(osaDistance(a, b))/float64(n)
}

// phoneticOverlap is the share of tokens on each side whose phonetic keys appear on the other, weighted towards the query, or 1 when the whole strings sound alike.
func phoneticOverlap(q, a *form, homophone bool) float64 {
	if homophone {
		return 1
	}

	return 0.7*keyShare(q.keys, a.keys) + 0.3*keyShare(a.keys, q.keys)
}

// keyShare is the fraction of from's tokens with a key in to.
func keyShare(from, to [][]string) float64 {
	if len(from) == 0 {
		return 0
	}

	hit := 0

	for _, fk := range from {
		for _, tk := range to {
			if keysIntersect(fk, tk) {
				hit++

				break
			}
		}
	}

	return float64(hit) / float64(len(from))
}

func keysIntersect(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}

	return false
}

// numbersAgree reports whether two forms carry the same numbers. A spoken "1" also agrees with no number at all, since first instalments rarely carry one.
func numbersAgree(q, a *form) bool {
	return q.digits == a.digits || (q.digits == "1" && a.digits == "")
}

// digitsWithin reports whether every number in q is also one of a's ("uno la llegada" / "uno 2: La llegada").
func digitsWithin(q, a *form) bool {
	for _, t := range q.toks {
		if isDigits(t) && !slices.Contains(a.toks, t) {
			return false
		}
	}

	return true
}

// coverage is the weighted share of query tokens that something in the alias accounts for. Numbers must match exactly (or be a "1" against an unnumbered title); words may be a small typo or a phonetic match away. In a language written without spaces (unspaced), whose word boundaries are a segmenter's guess, a word the alias spells with its spaces removed counts too ("toisutori" in "toi sutori 3").
func coverage(q, a *form, unspaced bool) float64 {
	total, hit := 0, 0

	for i, t := range q.toks {
		w := runeLen(t)
		if isDigits(t) {
			w = digitWeight

			if t == "1" && a.digits == "" {
				total += w
				hit += w

				continue
			}
		}

		total += w

		if unspaced && !isDigits(t) && runeLen(t) >= minUnspacedCover && strings.Contains(a.compact, t) {
			hit += w

			continue
		}

		for j, u := range a.toks {
			if tokenMatches(t, u, q.keys[i], a.keys[j]) {
				hit += w

				break
			}
		}
	}

	if total == 0 {
		return 1
	}

	return float64(hit) / float64(total)
}

func tokenMatches(t, u string, tk, uk []string) bool {
	if t == u {
		return true
	}

	if isDigits(t) || isDigits(u) {
		return false
	}

	n := runeLen(t)

	switch {
	case n >= 8 && osaDistance(t, u) <= 2:
		return true
	case n >= 4 && osaDistance(t, u) <= 1:
		return true
	case n >= 3 && keysIntersect(tk, uk):
		return true
	}

	return false
}
