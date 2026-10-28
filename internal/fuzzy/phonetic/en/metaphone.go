package en

import "unicode"

// doubleMetaphone returns the uncapped Double Metaphone primary and alternate codes, with the alternate empty when it equals the primary. It reproduces github.com/PhonoGrams/double_metaphone v0.1.0's EncodeUncapped exactly, quirks included, minus the branches that test for spaces, which cannot occur because only letters are kept.
func doubleMetaphone(word string) (primary, alternate string) {
	var runes [stackRunes]rune

	m := metaphone{s: runes[:0]}

	for _, r := range word {
		if r = unicode.ToUpper(r); unicode.IsLetter(r) {
			m.s = append(m.s, r)
		}
	}

	m.n = len(m.s)
	if m.n == 0 {
		return "", ""
	}

	m.last = m.n - 1
	m.slavo = m.slavoGermanic()

	if m.stringAt(0, "GN", "KN", "PN", "WR", "PS") {
		m.i = 1
	}

	if m.at(0) == 'X' {
		m.add("S", "S")
		m.i = 1
	}

	for m.i < m.n {
		m.step()
	}

	primary = string(m.pri.bytes())
	if alt := m.alt.bytes(); string(alt) != primary {
		alternate = string(alt)
	}

	return primary, alternate
}

func (m *metaphone) slavoGermanic() bool {
	for i, r := range m.s {
		if r == 'W' || r == 'K' || (r == 'C' && m.at(i+1) == 'Z') {
			return true
		}
	}

	return false
}

func (m *metaphone) at(i int) rune {
	if i < 0 || i >= m.n {
		return 0
	}

	return m.s[i]
}

func (m *metaphone) isVowel(i int) bool {
	switch m.at(i) {
	case 'A', 'E', 'I', 'O', 'U', 'Y':
		return true
	}

	return false
}

// stringAt reports whether any of the ASCII tokens occurs at start.
func (m *metaphone) stringAt(start int, tokens ...string) bool {
	if start < 0 {
		return false
	}

next:
	for _, tok := range tokens {
		if start+len(tok) > m.n {
			continue
		}

		for k := range len(tok) {
			if m.s[start+k] != rune(tok[k]) {
				continue next
			}
		}

		return true
	}

	return false
}

// add appends to both codes; an empty alternate means the same as the primary.
func (m *metaphone) add(p, a string) {
	if a == "" {
		a = p
	}

	m.pri.add(p)
	m.alt.add(a)
}

// add appends s, moving to the heap once the stack buffer is full. It grows by hand rather than with append: storing append's result back through c would make every metaphone buffer escape.
func (c *code) add(s string) {
	if c.heap == nil && c.n+len(s) <= len(c.buf) {
		c.n += copy(c.buf[c.n:], s)

		return
	}

	if old := c.bytes(); c.heap == nil || len(old)+len(s) > cap(old) {
		grown := make([]byte, len(old), 2*(len(old)+len(s)))
		copy(grown, old)
		c.heap = grown
	}

	n := len(c.heap)
	c.heap = c.heap[:n+len(s)]
	copy(c.heap[n:], s)
}

func (c *code) bytes() []byte {
	if c.heap != nil {
		return c.heap
	}

	return c.buf[:c.n]
}

// skip advances past the current letter, and past the next one too when it is double.
func (m *metaphone) skip(double rune) {
	if m.at(m.i+1) == double {
		m.i += 2
	} else {
		m.i++
	}
}

//nolint:gocyclo,cyclop,funlen // One case per letter, as in Philips's description.
func (m *metaphone) step() {
	switch m.at(m.i) {
	case 'A', 'E', 'I', 'O', 'U', 'Y':
		if m.i == 0 {
			m.add("A", "A")
		}

		m.i++
	case 'B':
		m.add("P", "P")
		m.skip('B')
	case 'Ç':
		m.add("S", "S")
		m.i++
	case 'C':
		m.stepC()
	case 'D':
		m.stepD()
	case 'F':
		m.add("F", "F")
		m.skip('F')
	case 'G':
		m.stepG()
	case 'H':
		if (m.i == 0 || m.isVowel(m.i-1)) && m.isVowel(m.i+1) {
			m.add("H", "H")
			m.i += 2
		} else {
			m.i++
		}
	case 'J':
		m.stepJ()
	case 'K':
		m.add("K", "K")
		m.skip('K')
	case 'L':
		m.stepL()
	case 'M':
		if (m.stringAt(m.i-1, "UMB") && (m.i+1 == m.last || m.stringAt(m.i+2, "ER"))) || m.at(m.i+1) == 'M' {
			m.i += 2
		} else {
			m.i++
		}

		m.add("M", "M")
	case 'N':
		m.add("N", "N")
		m.skip('N')
	case 'Ñ':
		m.add("N", "N")
		m.i++
	case 'P':
		if m.at(m.i+1) == 'H' {
			m.add("F", "F")
			m.i += 2

			return
		}

		m.add("P", "P")

		if m.at(m.i+1) == 'P' || m.at(m.i+1) == 'B' {
			m.i += 2
		} else {
			m.i++
		}
	case 'Q':
		m.add("K", "K")
		m.skip('Q')
	case 'R':
		if m.i == m.last && !m.slavo && m.stringAt(m.i-2, "IE") && !m.stringAt(m.i-4, "ME", "MA") {
			m.add("", "R")
		} else {
			m.add("R", "R")
		}

		m.skip('R')
	case 'S':
		m.stepS()
	case 'T':
		m.stepT()
	case 'V':
		m.add("F", "F")
		m.skip('V')
	case 'W':
		m.stepW()
	case 'X':
		m.stepX()
	case 'Z':
		m.stepZ()
	default:
		m.i++
	}
}

func (m *metaphone) stepL() {
	if m.at(m.i+1) != 'L' {
		m.i++
		m.add("L", "L")

		return
	}

	if (m.i == m.n-3 && m.stringAt(m.i-1, "ILLO", "ILLA", "ALLE")) ||
		((m.stringAt(m.last-1, "AS", "OS") || m.at(m.last) == 'A' || m.at(m.last) == 'O') && m.stringAt(m.i-1, "ALLE")) {
		m.add("L", "")
		m.i += 2

		return
	}

	m.i += 2
	m.add("L", "L")
}

//nolint:gocyclo,cyclop,funlen // Transcribed rule for rule so it stays checkable against the reference.
func (m *metaphone) stepC() {
	if m.i > 1 && !m.isVowel(m.i-2) && m.stringAt(m.i-1, "ACH") &&
		(m.at(m.i+2) != 'I' && (m.at(m.i+2) != 'E' || m.stringAt(m.i-2, "BACHER", "MACHER"))) {
		m.add("K", "K")
		m.i += 2

		return
	}

	if m.i == 0 && m.stringAt(m.i, "CAESAR") {
		m.add("S", "S")
		m.i += 2

		return
	}

	if m.stringAt(m.i, "CHIA") {
		m.add("K", "K")
		m.i += 2

		return
	}

	if m.stringAt(m.i, "CH") {
		m.stepCH()

		return
	}

	if m.stringAt(m.i, "CZ") && !m.stringAt(m.i-2, "WICZ") {
		m.add("S", "X")
		m.i += 2

		return
	}

	if m.stringAt(m.i+1, "CIA") {
		m.add("X", "X")
		m.i += 3

		return
	}

	if m.stringAt(m.i, "CC") && (m.i != 1 || m.at(0) != 'M') {
		if m.stringAt(m.i+2, "I", "E", "H") && !m.stringAt(m.i+2, "HU") {
			if (m.i == 1 && m.at(m.i-1) == 'A') || m.stringAt(m.i-1, "UCCEE", "UCCES") {
				m.add("KS", "KS")
			} else {
				m.add("X", "X")
			}

			m.i += 3

			return
		}

		m.add("K", "K")
		m.i += 2

		return
	}

	if m.stringAt(m.i, "CK", "CG", "CQ") {
		m.add("K", "K")
		m.i += 2

		return
	}

	if m.stringAt(m.i, "CI", "CE", "CY") {
		if m.stringAt(m.i, "CIO", "CIE", "CIA") {
			m.add("S", "X")
		} else {
			m.add("S", "S")
		}

		m.i += 2

		return
	}

	m.add("K", "K")

	if m.stringAt(m.i+1, "C", "K", "Q") && !m.stringAt(m.i+1, "CE", "CI") {
		m.i += 2
	} else {
		m.i++
	}
}

func (m *metaphone) stepCH() {
	defer func() { m.i += 2 }()

	if m.i > 0 && m.stringAt(m.i, "CHAE") {
		m.add("K", "X")

		return
	}

	if m.i == 0 && (m.stringAt(m.i+1, "HARAC", "HARIS") || m.stringAt(m.i+1, "HOR", "HYM", "HIA", "HEM")) && !m.stringAt(0, "CHORE") {
		m.add("K", "K")

		return
	}

	next2 := m.at(m.i + 2)
	prev := m.at(m.i - 1)

	switch {
	case m.stringAt(0, "SCH") || m.stringAt(m.i-2, "ORCHES", "ARCHIT", "ORCHID") || next2 == 'T' || next2 == 'S' ||
		((prev == 'A' || prev == 'O' || prev == 'U' || prev == 'E' || m.i == 0) &&
			(next2 == 'L' || next2 == 'R' || next2 == 'N' || next2 == 'M' || next2 == 'B' || next2 == 'H' || next2 == 'F' || next2 == 'V' || next2 == 'W' || next2 == 0)):
		m.add("K", "K")
	case m.i > 0 && m.stringAt(0, "MC"):
		m.add("K", "K")
	case m.i > 0:
		m.add("X", "K")
	default:
		m.add("X", "X")
	}
}

func (m *metaphone) stepD() {
	if m.stringAt(m.i, "DG") {
		if m.stringAt(m.i+2, "I", "E", "Y") {
			m.add("J", "J")
			m.i += 3

			return
		}

		m.add("TK", "TK")
		m.i += 2

		return
	}

	if m.stringAt(m.i, "DT", "DD") {
		m.add("T", "T")
		m.i += 2

		return
	}

	m.add("T", "T")
	m.i++
}

//nolint:gocyclo,cyclop,funlen // Transcribed rule for rule so it stays checkable against the reference.
func (m *metaphone) stepG() {
	if m.at(m.i+1) == 'H' {
		m.stepGH()

		return
	}

	if m.at(m.i+1) == 'N' {
		switch {
		case m.i == 1 && m.isVowel(0) && !m.slavo:
			m.add("KN", "N")
		case !m.stringAt(m.i+2, "EY") && !m.slavo:
			m.add("N", "KN")
		default:
			m.add("KN", "KN")
		}

		m.i += 2

		return
	}

	if m.stringAt(m.i+1, "LI") && !m.slavo {
		m.add("KL", "L")
		m.i += 2

		return
	}

	if m.i == 0 && (m.at(m.i+1) == 'Y' || m.stringAt(m.i+1, "ES", "EP", "EB", "EL", "EY", "IB", "IL", "IN", "IE", "EI", "ER")) {
		m.add("K", "J")
		m.i += 2

		return
	}

	if (m.stringAt(m.i+1, "ER") || m.at(m.i+1) == 'Y') &&
		!m.stringAt(0, "DANGER", "RANGER", "MANGER") &&
		!m.stringAt(m.i-1, "E", "I") && !m.stringAt(m.i-1, "RGY", "OGY") {
		m.add("K", "J")
		m.i += 2

		return
	}

	if m.stringAt(m.i+1, "E", "I", "Y") || m.stringAt(m.i-1, "AGGI", "OGGI") {
		switch {
		case m.stringAt(0, "SCH") || m.stringAt(m.i+1, "ET"):
			m.add("K", "K")
		case m.stringAt(m.i+1, "IER"):
			m.add("J", "J")
		default:
			m.add("J", "K")
		}

		m.i += 2

		return
	}

	m.skip('G')
	m.add("K", "K")
}

func (m *metaphone) stepGH() {
	defer func() { m.i += 2 }()

	if m.i > 0 && !m.isVowel(m.i-1) {
		m.add("K", "K")

		return
	}

	if m.i == 0 {
		if m.at(m.i+2) == 'I' {
			m.add("J", "J")
		} else {
			m.add("K", "K")
		}

		return
	}

	bhd := func(r rune) bool { return r == 'B' || r == 'H' || r == 'D' }
	if (m.i > 1 && bhd(m.at(m.i-2))) || (m.i > 2 && bhd(m.at(m.i-3))) || (m.i > 3 && (m.at(m.i-4) == 'B' || m.at(m.i-4) == 'H')) {
		return
	}

	switch c := m.at(m.i - 3); {
	case m.i > 2 && m.at(m.i-1) == 'U' && (c == 'C' || c == 'G' || c == 'L' || c == 'R' || c == 'T'):
		m.add("F", "F")
	case m.i > 0 && m.at(m.i-1) != 'I':
		m.add("K", "K")
	}
}

func (m *metaphone) stepJ() {
	if m.stringAt(m.i, "JOSE") {
		m.add("J", "H")
		m.i++

		return
	}

	switch {
	case m.i == 0:
		m.add("J", "A")
	case m.isVowel(m.i-1) && !m.slavo && (m.at(m.i+1) == 'A' || m.at(m.i+1) == 'O'):
		m.add("J", "H")
	case m.i == m.last:
		m.add("J", "")
	case !m.stringAt(m.i+1, "L", "T", "K", "S", "N", "M", "B", "Z") && !m.stringAt(m.i-1, "S", "K", "L"):
		m.add("J", "J")
	}

	m.skip('J')
}

//nolint:gocyclo,cyclop,funlen // Transcribed rule for rule so it stays checkable against the reference.
func (m *metaphone) stepS() {
	if m.stringAt(m.i-1, "ISL", "YSL") {
		m.i++

		return
	}

	if m.i == 0 && m.stringAt(m.i, "SUGAR") {
		m.add("X", "S")
		m.i++

		return
	}

	if m.stringAt(m.i, "SH") {
		if m.stringAt(m.i+1, "HEIM", "HOEK", "HOLM", "HOLZ") {
			m.add("S", "S")
		} else {
			m.add("X", "X")
		}

		m.i += 2

		return
	}

	if m.stringAt(m.i, "SIO", "SIA") {
		if !m.slavo {
			m.add("S", "X")
		} else {
			m.add("S", "S")
		}

		m.i += 3

		return
	}

	if (m.i == 0 && m.stringAt(m.i+1, "M", "N", "L", "W")) || m.stringAt(m.i+1, "Z") {
		m.add("S", "X")
		m.skip('Z')

		return
	}

	if m.stringAt(m.i, "SC") {
		switch {
		case m.at(m.i+2) == 'H':
			switch {
			case m.stringAt(m.i+3, "ER", "EN"):
				m.add("X", "SK")
			case m.stringAt(m.i+3, "OO", "UY", "ED", "EM"):
				m.add("SK", "SK")
			case m.i == 0 && !m.isVowel(3) && m.at(3) != 'W':
				m.add("X", "S")
			default:
				m.add("X", "X")
			}
		case m.stringAt(m.i+2, "I", "E", "Y"):
			m.add("S", "S")
		default:
			m.add("SK", "SK")
		}

		m.i += 3

		return
	}

	if m.i == m.last && m.stringAt(m.i-2, "AI", "OI") {
		m.add("", "S")
	} else {
		m.add("S", "S")
	}

	if m.stringAt(m.i+1, "S", "Z") {
		m.i += 2
	} else {
		m.i++
	}
}

func (m *metaphone) stepT() {
	if m.stringAt(m.i, "TION", "TIA", "TCH") {
		m.add("X", "X")
		m.i += 3

		return
	}

	if m.stringAt(m.i, "TH", "TTH") {
		if m.stringAt(m.i+2, "OM", "AM") || m.stringAt(0, "SCH") {
			m.add("T", "T")
		} else {
			m.add("0", "T")
		}

		m.i += 2

		return
	}

	m.add("T", "T")

	if m.stringAt(m.i+1, "T", "D") {
		m.i += 2
	} else {
		m.i++
	}
}

func (m *metaphone) stepW() {
	if m.stringAt(m.i, "WR") {
		m.add("R", "R")
		m.i += 2

		return
	}

	if m.i == 0 && (m.isVowel(m.i+1) || m.stringAt(m.i, "WH")) {
		if m.isVowel(m.i + 1) {
			m.add("A", "F")
		} else {
			m.add("A", "A")
		}
	}

	if (m.i == m.last && m.isVowel(m.i-1)) || m.stringAt(m.i-1, "EWSKI", "EWSKY", "OWSKI", "OWSKY") || m.stringAt(0, "SCH") {
		m.add("", "F")
		m.i++

		return
	}

	if m.stringAt(m.i, "WICZ", "WITZ") {
		m.add("TS", "FX")
		m.i += 4

		return
	}

	m.i++
}

func (m *metaphone) stepX() {
	if m.i == 0 {
		m.add("S", "S")
		m.i++

		return
	}

	if m.i != m.last || (!m.stringAt(m.i-3, "IAU", "EAU") && !m.stringAt(m.i-2, "AU", "OU")) {
		m.add("KS", "KS")
	}

	if m.stringAt(m.i+1, "C", "X") {
		m.i += 2
	} else {
		m.i++
	}
}

func (m *metaphone) stepZ() {
	if m.at(m.i+1) == 'H' {
		m.add("J", "J")
		m.i += 2

		return
	}

	if m.stringAt(m.i+1, "ZO", "ZI", "ZA") || (m.slavo && m.i > 0 && m.at(m.i-1) != 'T') {
		m.add("S", "TS")
	} else {
		m.add("S", "S")
	}

	m.skip('Z')
}
