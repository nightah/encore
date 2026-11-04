package tinyseg

import (
	"strings"
	"unicode/utf8"
)

// Segment splits s into words. Each word is a substring of s, and joined in order they make s again; an empty s has no words. It is safe for concurrent use.
func Segment(s string) []string {
	return Append(nil, s)
}

// Append appends the words of s to dst, as Segment does, and returns the extended slice.
func Append(dst []string, s string) []string {
	if s == "" {
		return dst
	}

	n := utf8.RuneCountInString(s)

	var (
		chars [stackChars]rune
		types [stackChars]byte
		start [stackChars]int
	)

	// seg and ctype have three padding entries on each side, the original's B3 B2 B1 and E1 E2 E3; start holds each character's byte offset in s.
	seg, ctype, at := chars[:0], types[:0], start[:0]
	if n+2*pad > stackChars {
		seg, ctype, at = make([]rune, 0, n+2*pad), make([]byte, 0, n+2*pad), make([]int, 0, n)
	}

	seg = append(seg, tagB3, tagB2, tagB1)
	ctype = append(ctype, 'O', 'O', 'O')

	for i, r := range s {
		at = append(at, i)
		seg = append(seg, r)
		ctype = append(ctype, charType(r))
	}

	seg = append(seg, tagE1, tagE2, tagE3)
	ctype = append(ctype, 'O', 'O', 'O')

	p1, p2, p3 := byte('U'), byte('U'), byte('U')
	from := 0

	for i := pad + 1; i < len(seg)-pad; i++ {
		w1, w2, w3, w4, w5, w6 := seg[i-3], seg[i-2], seg[i-1], seg[i], seg[i+1], seg[i+2]
		c1, c2, c3, c4, c5, c6 := ctype[i-3], ctype[i-2], ctype[i-1], ctype[i], ctype[i+1], ctype[i+2]

		score := int32(bias)
		score += m.up[0][p1] + m.up[1][p2] + m.up[2][p3]
		score += m.bp[0][tags(p1, p2)] + m.bp[1][tags(p2, p3)]
		score += m.uw[0][w1] + m.uw[1][w2] + m.uw[2][w3] + m.uw[3][w4] + m.uw[4][w5] + m.uw[5][w6]
		score += m.bw[0][chars2(w2, w3)] + m.bw[1][chars2(w3, w4)] + m.bw[2][chars2(w4, w5)]
		score += m.tw[0][chars3(w1, w2, w3)] + m.tw[1][chars3(w2, w3, w4)] + m.tw[2][chars3(w3, w4, w5)] + m.tw[3][chars3(w4, w5, w6)]
		score += m.uc[0][c1] + m.uc[1][c2] + m.uc[2][c3] + m.uc[3][c4] + m.uc[4][c5] + m.uc[5][c6]
		score += m.bc[0][tags(c2, c3)] + m.bc[1][tags(c3, c4)] + m.bc[2][tags(c4, c5)]
		score += m.tc[0][tags(c1, c2, c3)] + m.tc[1][tags(c2, c3, c4)] + m.tc[2][tags(c3, c4, c5)] + m.tc[3][tags(c4, c5, c6)]
		score += m.uq[0][tags(p1, c1)] + m.uq[1][tags(p2, c2)] + m.uq[2][tags(p3, c3)]
		score += m.bq[0][tags(p2, c2, c3)] + m.bq[1][tags(p2, c3, c4)] + m.bq[2][tags(p3, c2, c3)] + m.bq[3][tags(p3, c3, c4)]
		score += m.tq[0][tags(p2, c1, c2, c3)] + m.tq[1][tags(p2, c2, c3, c4)] + m.tq[2][tags(p3, c1, c2, c3)] + m.tq[3][tags(p3, c2, c3, c4)]

		p := byte('O')

		if score > 0 {
			cut := at[i-pad]
			dst = append(dst, s[from:cut])
			from = cut
			p = 'B'
		}

		p1, p2, p3 = p2, p3, p
	}

	return append(dst, s[from:])
}

// charType is the original's character class: M for kanji numerals, H for other kanji, I for hiragana, K for katakana, A for Latin letters, N for digits and O for anything else.
func charType(r rune) byte {
	switch {
	case strings.ContainsRune(numerals, r):
		return 'M'
	case r >= '一' && r <= '龠', r == '々', r == '〆', r == 'ヵ', r == 'ヶ':
		return 'H'
	case r >= 'ぁ' && r <= 'ん':
		return 'I'
	case r >= 'ァ' && r <= 'ヴ', r == 'ー', r >= 'ｱ' && r <= 'ﾝ', r == 'ﾞ', r == 'ｰ':
		return 'K'
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= 'ａ' && r <= 'ｚ', r >= 'Ａ' && r <= 'Ｚ':
		return 'A'
	case r >= '0' && r <= '9', r >= '０' && r <= '９':
		return 'N'
	default:
		return 'O'
	}
}

// tags packs up to four class or tag bytes into one key.
func tags(b ...byte) uint32 {
	var k uint32
	for _, c := range b {
		k = k<<8 | uint32(c)
	}

	return k
}

// chars2 and chars3 pack two or three characters (or padding tags) into one key, 21 bits each.
func chars2(a, b rune) uint64 { return uint64(a)<<runeBits | uint64(b) } //nolint:gosec // Runes ranged from a string, and the tags, are never negative.

func chars3(a, b, c rune) uint64 { return uint64(a)<<(2*runeBits) | uint64(b)<<runeBits | uint64(c) } //nolint:gosec // As chars2.

// m is built from the tables in weights.go once, at init.
var m = build()

func build() *weights {
	w := &weights{}

	for i, t := range upTable {
		w.up[i] = map[byte]int32{}
		for k, v := range t {
			w.up[i][k[0]] = v
		}
	}

	for i, t := range ucTable {
		w.uc[i] = map[byte]int32{}
		for k, v := range t {
			w.uc[i][k[0]] = v
		}
	}

	packChars(w.uw[:], uwTable[:], 1, func(c []rune) rune { return c[0] })
	packChars(w.bw[:], bwTable[:], 2, func(c []rune) uint64 { return chars2(c[0], c[1]) })
	packChars(w.tw[:], twTable[:], 3, func(c []rune) uint64 { return chars3(c[0], c[1], c[2]) })
	packTags(w.bp[:], bpTable[:])
	packTags(w.bc[:], bcTable[:])
	packTags(w.tc[:], tcTable[:])
	packTags(w.uq[:], uqTable[:])
	packTags(w.bq[:], bqTable[:])
	packTags(w.tq[:], tqTable[:])

	return w
}

// packChars packs the keys of n characters; the original's tables hold a few keys of other lengths, which its concatenated lookups never meet.
func packChars[K comparable](dst []map[K]int32, src []map[string]int32, n int, pack func([]rune) K) {
	for i, t := range src {
		dst[i] = map[K]int32{}

		for k, v := range t {
			if c := keyChars(k); len(c) == n {
				dst[i][pack(c)] = v
			}
		}
	}
}

func packTags(dst []map[uint32]int32, src []map[string]int32) {
	for i, t := range src {
		dst[i] = map[uint32]int32{}
		for k, v := range t {
			dst[i][tags([]byte(k)...)] = v
		}
	}
}

// keyChars reads a table key as the characters it is made of, taking B1 to B3 and E1 to E3 as the padding tags.
func keyChars(k string) []rune {
	var out []rune

	for k != "" {
		if len(k) >= 2 && (k[0] == 'B' || k[0] == 'E') && k[1] >= '1' && k[1] <= '3' {
			out = append(out, padTag(k[0], k[1]))
			k = k[2:]

			continue
		}

		r, size := utf8.DecodeRuneInString(k)
		out = append(out, r)
		k = k[size:]
	}

	return out
}

func padTag(side, n byte) rune {
	if side == 'B' {
		return tagB1 + rune(n-'1')
	}

	return tagE1 + rune(n-'1')
}
