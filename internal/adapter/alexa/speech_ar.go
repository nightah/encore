package alexa

import (
	"strconv"
	"unicode"
	"unicode/utf8"
)

// arabicDuals are the dual forms of the Arabic duration units, which say "two" on their own ("ساعتان").
var arabicDuals = map[string]string{"ساعة": "ساعتان", "دقيقة": "دقيقتان"}

// arabicCount words n of a unit as Arabic counts: the singular alone for one ("ساعة"), the dual for two ("ساعتان", or the plural for a unit without one in arabicDuals), the plural after 3 to 10 ("3 ساعات") and the singular after 11 and more ("11 ساعة"). Past a hundred the last two digits decide ("103 ساعات", "111 ساعة"), and a whole hundred takes the singular ("100 ساعة").
func arabicCount(n int, one, many string) string {
	switch t := n % 100; {
	case n == 1:
		return one
	case n == 2 && arabicDuals[one] != "":
		return arabicDuals[one]
	case n == 2, t >= 3 && t <= 10:
		return strconv.Itoa(n) + " " + many
	default:
		return strconv.Itoa(n) + " " + one
	}
}

// startsArabic reports whether s starts with an Arabic letter, which the conjunction و is written joined to ("ودون"); before a Latin title it stands apart ("و Dune").
func startsArabic(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)

	return unicode.Is(unicode.Arabic, r) && unicode.IsLetter(r)
}
