package loanword

import "strings"

// katakana writes English phones as Japanese writes an English loanword: each consonant takes the vowel after it, or u (o after t and d, i after ch and j) when none does; long vowels and an r after a vowel are ー; a stop after a short vowel is doubled with ッ; l is the r line, th is s and the th of "the" z, and v the ヴ line, which the matcher keys as the b line; n before a consonant is ン and m ム, or ン before a b or p.
//
//nolint:gocyclo,cyclop // One case per kind of phone.
func katakana(ps []phone) string {
	var b strings.Builder

	long := func() {
		if !strings.HasSuffix(b.String(), "ー") {
			b.WriteString("ー")
		}
	}

	for i := 0; i < len(ps); i++ {
		p := ps[i]
		next := at(ps, i+1)

		switch {
		case p == geminate:
			// Before a consonant or at the end, the stop rule below writes ッ itself.
			if after := at(ps, i+2); isVowelPhone(after) || after == "y" {
				b.WriteString("ッ")
			}
		case p == postR:
			// An r after the a of "here" and "there" is already written ア.
			if i > 0 && isVowelPhone(ps[i-1]) && ps[i-1] != "ia" && ps[i-1] != "ea" {
				long()
			}
		case isVowelPhone(p):
			writeVowel(&b, "", p, next, long)
		case (p == "n" || p == "ng" && (next == "k" || next == "g")) && !isVowelPhone(next) && next != "y":
			b.WriteString("ン")
		case p == "m" && !isVowelPhone(next) && next != "y":
			if next == "b" || next == "p" {
				b.WriteString("ン")
			} else {
				b.WriteString("ム")
			}
		case p == "ng" && !isVowelPhone(next):
			b.WriteString("ング")
		case p == "t" && next == "s" && !isVowelPhone(at(ps, i+2)):
			// ts is ツ ("nuts").
			if i > 0 && short(ps[i-1]) {
				b.WriteString("ッ")
			}

			b.WriteString("ツ")
			i++
		case next == schwa && at(ps, i+2) == "l" && !isVowelPhone(at(ps, i+3)) && !isVowelPhone(p) && p != "r" && p != "l":
			// A consonant before the syllabic l of "-ble" and "-tle" takes its own vowel ("Beatles", "apple").
			if (p == "p" || p == "k") && i > 0 && short(ps[i-1]) {
				b.WriteString("ッ")
			}

			b.WriteString(alone(p))
			i++
		case p == "w" && i > 0 && (ps[i-1] == "k" || ps[i-1] == "g") && isVowelPhone(next):
			// After k and g, w is written as the vowel it glides into ("queen" クイーン, "squid" スクイッド).
			writeVowel(&b, "", next, at(ps, i+2), long)
			i++
		case next == "y" && isVowelPhone(at(ps, i+2)):
			v := at(ps, i+2)
			b.WriteString(palatal(p, v))

			if l := vowelTail(v, at(ps, i+3)); l != "" {
				if l == "ー" {
					long()
				} else {
					b.WriteString(l)
				}
			}

			i += 2
		case isVowelPhone(next):
			if p == "sh" || p == "zh" || p == "ch" || p == "j" {
				if next == schwa && at(ps, i+2) == "n" {
					b.WriteString(kanaRow[p][4])
					i++

					continue
				}
			}

			if (p == "z" || p == "v" || p == "s") && next == schwa && at(ps, i+2) == "n" && i+3 == len(ps) {
				b.WriteString(alone(p))
				i++

				continue
			}

			writeVowel(&b, p, next, at(ps, i+2), long)
			i++
		default:
			if stop(p) && i > 0 && (short(ps[i-1]) || ps[i-1] == geminate && i > 1 && short(ps[i-2]) && !isVowelPhone(next)) && (i+1 == len(ps) || !isVowelPhone(next) && next != "y" && next != "r" && next != "l" && next != "w") {
				b.WriteString("ッ")
			}

			b.WriteString(alone(p))
		}
	}

	return b.String()
}

// writeVowel writes the mora of consonant c (or none) and vowel v, lengthening a long vowel. next is the phone after v.
func writeVowel(b *strings.Builder, c string, v, next phone, long func()) {
	row, ok := kanaRow[c]
	if !ok {
		row = kanaRow[""]
	}

	b.WriteString(row[vowelIndex(v)])

	switch tail := vowelTail(v, next); tail {
	case "":
	case "ー":
		long()
	default:
		b.WriteString(tail)
	}
}

// vowelIndex is the column of a vowel's first sound in kanaRow: a, i, u, e, o.
func vowelIndex(v phone) int {
	switch v {
	case "i", "ii", "ia":
		return 1
	case "u", "uu":
		return 2
	case "e", "ei", "ea":
		return 3
	case "o", "oo", "ou", "oi":
		return 4
	}

	return 0
}

// vowelTail is what follows a vowel's first kana: ー for a long vowel, the second vowel of a diphthong, or nothing. ei is エー before a consonant and エイ at the end of a word.
func vowelTail(v, next phone) string {
	switch v {
	case "aa", "ii", "uu", "oo", "ou":
		return "ー"
	case "ei":
		if next == "" || isVowelPhone(next) {
			return "イ"
		}

		return "ー"
	case "ai", "oi":
		return "イ"
	case "au":
		return "ウ"
	case "ia", "ea":
		return "ア"
	}

	return ""
}

// palatal writes consonant c, a y glide and vowel v ("k" "uu" -> キュ), the length left to the caller.
func palatal(c string, v phone) string {
	col := vowelIndex(v)

	switch c {
	case "t", "d", "f", "v":
		return palatalBase[c] + [5]string{"ャ", "ィ", "ュ", "ェ", "ョ"}[col]
	case "s", "sh", "z", "j", "zh", "ch":
		// The sh, ch and j rows already carry the glide.
		return kanaRow[glided[c]][col]
	}

	row, ok := kanaRow[c]
	if !ok {
		return alone(c) + kanaRow["y"][col]
	}

	if col == 1 {
		return row[1]
	}

	return row[1] + [5]string{"ャ", "", "ュ", "ェ", "ョ"}[col]
}

// palatalBase is the kana a y glide follows for consonants whose i-kana has a small vowel of its own (ティ, フィ).
var palatalBase = map[string]string{"t": "テ", "d": "デ", "f": "フ", "v": "ヴ"}

// glided is the row that writes a sibilant or affricate before a y glide.
var glided = map[string]string{"s": "sh", "sh": "sh", "z": "j", "j": "j", "zh": "j", "ch": "ch"}

// alone writes a consonant with no vowel after it, with the vowel Japanese gives it.
func alone(c string) string {
	switch c {
	case "t", "d":
		return kanaRow[c][4]
	case "ch", "j":
		return kanaRow[c][1]
	case "h":
		return ""
	}

	if row, ok := kanaRow[c]; ok {
		return row[2]
	}

	return ""
}

func stop(p phone) bool {
	switch p {
	case "p", "t", "k", "ch", "b", "d", "g", "j":
		return true
	}

	return false
}

func short(p phone) bool {
	switch p {
	case "a", "ae", "e", "i", "o", "u":
		return true
	}

	return false
}

func at(ps []phone, i int) phone {
	if i < len(ps) {
		return ps[i]
	}

	return ""
}

// kanaRow is each consonant's katakana with a, i, u, e and o.
var kanaRow = map[string][5]string{
	"":   {"ア", "イ", "ウ", "エ", "オ"},
	"k":  {"カ", "キ", "ク", "ケ", "コ"},
	"g":  {"ガ", "ギ", "グ", "ゲ", "ゴ"},
	"s":  {"サ", "シ", "ス", "セ", "ソ"},
	"z":  {"ザ", "ジ", "ズ", "ゼ", "ゾ"},
	"t":  {"タ", "ティ", "トゥ", "テ", "ト"},
	"d":  {"ダ", "ディ", "ドゥ", "デ", "ド"},
	"n":  {"ナ", "ニ", "ヌ", "ネ", "ノ"},
	"h":  {"ハ", "ヒ", "フ", "ヘ", "ホ"},
	"f":  {"ファ", "フィ", "フ", "フェ", "フォ"},
	"v":  {"ヴァ", "ヴィ", "ヴ", "ヴェ", "ヴォ"},
	"b":  {"バ", "ビ", "ブ", "ベ", "ボ"},
	"p":  {"パ", "ピ", "プ", "ペ", "ポ"},
	"m":  {"マ", "ミ", "ム", "メ", "モ"},
	"y":  {"ヤ", "イ", "ユ", "イェ", "ヨ"},
	"r":  {"ラ", "リ", "ル", "レ", "ロ"},
	"l":  {"ラ", "リ", "ル", "レ", "ロ"},
	"w":  {"ワ", "ウィ", "ウ", "ウェ", "ウォ"},
	"sh": {"シャ", "シ", "シュ", "シェ", "ショ"},
	"ch": {"チャ", "チ", "チュ", "チェ", "チョ"},
	"j":  {"ジャ", "ジ", "ジュ", "ジェ", "ジョ"},
	"zh": {"ジャ", "ジ", "ジュ", "ジェ", "ジョ"},
	"th": {"サ", "シ", "ス", "セ", "ソ"},
	"dh": {"ザ", "ジ", "ズ", "ゼ", "ゾ"},
	"ng": {"ンガ", "ンギ", "ング", "ンゲ", "ンゴ"},
}

// katakanaLetters are the Latin letters' names in katakana, as a Japanese speaker says an initialism.
var katakanaLetters = map[byte]string{
	'a': "エー", 'b': "ビー", 'c': "シー", 'd': "ディー", 'e': "イー", 'f': "エフ", 'g': "ジー", 'h': "エイチ", 'i': "アイ",
	'j': "ジェー", 'k': "ケー", 'l': "エル", 'm': "エム", 'n': "エヌ", 'o': "オー", 'p': "ピー", 'q': "キュー", 'r': "アール",
	's': "エス", 't': "ティー", 'u': "ユー", 'v': "ブイ", 'w': "ダブリュー", 'x': "エックス", 'y': "ワイ", 'z': "ゼット",
}
