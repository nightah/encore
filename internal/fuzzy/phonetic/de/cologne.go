// Portions of this file are derived from github.com/f1monkey/phonetic v1.4.2 (cologne), used under the MIT License:
//
// Copyright (c) 2023 F1monkey Labs
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package de

import (
	"strings"
	"unicode"
)

// cologne appends the Kölner Phonetik code of word to dst. It reproduces github.com/f1monkey/phonetic v1.4.2's cologne encoder exactly, including its edge cases: a context letter that is missing (at either end of the word) satisfies every rule that tests it, so a final "P" codes 3 and a lone "C" codes 4; letters it has no rule for, such as digits or accented letters other than ä, ö and ü, are skipped without becoming the previous letter; and duplicates are collapsed before zeros are dropped.
func cologne(dst []byte, word string) []byte {
	var buf [stackRunes]rune

	up := buf[:0]

	for _, r := range word {
		switch r = unicode.ToUpper(r); r {
		case 'Ä':
			up = append(up, 'A')
		case 'Ö':
			up = append(up, 'O')
		case 'Ü':
			up = append(up, 'U')
		case 'ß':
			up = append(up, 'S', 'S')
		default:
			up = append(up, r)
		}
	}

	var (
		prev    rune
		last    byte
		started bool
	)

	// emit collapses runs of one code, then drops zeros except a leading one.
	emit := func(c byte) {
		if started && c == last {
			return
		}

		if c != '0' || !started {
			dst = append(dst, c)
		}

		last, started = c, true
	}

	for i, r := range up {
		if r < 'A' || r > 'Z' {
			continue
		}

		var next rune
		if i+1 < len(up) {
			next = up[i+1]
		}

		code := cologneCode(r, i == 0, prev, next)
		for k := range len(code) {
			emit(code[k])
		}

		prev = r
	}

	return dst
}

// cologneCode is the code for letter r at the start of the word or not, between prev and next (zero when absent).
func cologneCode(r rune, initial bool, prev, next rune) string {
	// A missing neighbour satisfies both the positive and the negative test, as in the reference.
	after := func(set string) bool { return prev == 0 || strings.ContainsRune(set, prev) }
	notAfter := func(set string) bool { return prev == 0 || !strings.ContainsRune(set, prev) }
	before := func(set string) bool { return next == 0 || strings.ContainsRune(set, next) }
	notBefore := func(set string) bool { return next == 0 || !strings.ContainsRune(set, next) }

	switch r {
	case 'A', 'E', 'I', 'J', 'O', 'U', 'Y':
		return "0"
	case 'B':
		return "1"
	case 'C':
		switch {
		case initial && before("AHKLOQRUX"):
			return "4"
		case before("AHKOQUX") && notAfter("SZ"):
			return "4"
		case after("SZ"):
			return "8"
		case initial && notBefore("AHKLOQRUX"):
			return "8"
		case notBefore("AHKOQUX"):
			return "8"
		}

		return ""
	case 'D', 'T':
		if notBefore("CSZ") {
			return "2"
		}

		return "8"
	case 'F', 'V', 'W':
		return "3"
	case 'G', 'K', 'Q':
		return "4"
	case 'L':
		return "5"
	case 'M', 'N':
		return "6"
	case 'P':
		if before("H") {
			return "3"
		}

		return "1"
	case 'R':
		return "7"
	case 'S', 'Z':
		return "8"
	case 'X':
		if notAfter("CKQ") {
			return "48"
		}

		return "8"
	}

	// H has no code but still counts as the previous letter.
	return ""
}
