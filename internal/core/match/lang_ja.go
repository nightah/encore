package match

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nightah/encore/internal/fuzzy/phonetic/en"
	"github.com/nightah/encore/internal/fuzzy/tinyseg"
)

// japanese is written without spaces, in three scripts at once, so it leans on hooks the other languages leave unset: split segments each run of text into words (splitJapanese) and romanize writes kana in Hepburn romaji (romanizeKana), so a title and a request that spell a word in hiragana, katakana or Latin letters meet. Kanji are kept as they are: reading them takes a dictionary. Stop words and parts are therefore listed as romanize writes them, and the romaji is keyed with English Double Metaphone, which already merges the voicing slips (k/g, t/d, p/b, s/z) a recogniser makes with dakuten.
func japanese() *language {
	return &language{
		code: "ja",
		and:  "と",
		half: "半",
		// Particles: of, and, topic, subject, object, at, by, too, to, or, question.
		stop: set("no", "to", "wa", "ga", "o", "ni", "de", "mo", "e", "ya", "ka"),
		// パート, シーズン, チャプター and ボリューム as romanized, their English spellings, and 第, which comes before a number ("第2部").
		parts:    set("pato", "shizun", "chaputa", "boryumu", "part", "season", "chapter", "vol", "volume", "第"),
		split:    splitJapanese,
		unspaced: true,
		romanize: romanizeKana,
		phonetic: phoneticKeysOf(en.Encoder{}),
	}
}

// splitJapanese segments each token into words: runs of digits, of Latin letters and of any other script but Japanese stand alone, and the Japanese runs are segmented by TinySegmenter. Two of the segmenter's splits are undone (rejoin), a reading of a number or letter at the end of a katakana word becomes a word of its own ("トイストーリーフォー"), kanji numerals become digits (kanjiNumbers), and a kanji word is split again as it would be on its own (stableKanji).
func splitJapanese(toks []string) []string {
	out := make([]string, 0, len(toks)*2)

	for _, tok := range toks {
		start := len(out)

		for _, run := range scriptRuns(tok) {
			if r, _ := utf8.DecodeRuneInString(run); isJapanese(r) {
				out = tinyseg.Append(out, dropMarks(run))
			} else {
				out = append(out, run)
			}
		}

		out = append(out[:start], rejoin(out[start:])...)
	}

	words := make([]string, 0, len(out))
	for _, w := range out {
		words = append(words, splitReading(w)...)
	}

	return stableKanji(ordinals(kanjiNumbers(words)))
}

// scriptRuns splits tok where digits, Latin letters, the Japanese scripts and other scripts meet ("ストーリー3", "official髭男dism"). A combining mark belongs to the run of the letter before it.
func scriptRuns(tok string) []string {
	var out []string

	from, prev := 0, -1

	for i, r := range tok {
		class := 0

		switch {
		case unicode.Is(unicode.M, r) && prev >= 0:
			class = prev
		case r >= '0' && r <= '9':
			class = 1
		case r < utf8.RuneSelf || unicode.Is(unicode.Latin, r):
			class = 2
		case isJapanese(r):
			class = 3
		}

		if prev >= 0 && class != prev {
			out = append(out, tok[from:i])
			from = i
		}

		prev = class
	}

	return append(out, tok[from:])
}

// isJapanese reports whether r is written in a Japanese script: kanji, kana, the long-vowel mark, the kanji repetition mark or a kana voicing mark.
func isJapanese(r rune) bool {
	return r == 'ー' || r == '々' || r == '\u3099' || r == '\u309a' || unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana)
}

// dropMarks drops the combining marks tokenize could not compose with the kana or kanji before them ("か゚"): the segmenter and romanization would leave each as a word of its own.
func dropMarks(run string) string {
	if !strings.ContainsFunc(run, isMark) {
		return run
	}

	return strings.Map(func(r rune) rune {
		if isMark(r) {
			return -1
		}

		return r
	}, run)
}

func isMark(r rune) bool { return unicode.Is(unicode.M, r) }

// stableKanji splits each run of kanji in a word as the segmenter splits it on its own, and reads a kanji numeral after kana as a number, until nothing changes, so normalising the words again leaves them as they are. In context the segmenter can keep together kanji it splits alone ("いい日旅立ち" gives "日旅立", and "日旅立" alone gives "日" "旅立"), and romanization leaves each run of kanji a word of its own. Kana before or after the run stay on the first and last of its words.
func stableKanji(words []string) []string {
	for changed := true; changed; {
		changed = false
		out := make([]string, 0, len(words))

		for _, w := range words {
			from, to, pieces := unstableKanji(w)
			if pieces == nil {
				out = append(out, w)

				continue
			}

			pieces[0] = w[:from] + pieces[0]
			pieces[len(pieces)-1] += w[to:]
			out = append(out, pieces...)
			changed = true
		}

		words = ordinals(kanjiNumbers(out))
	}

	return words
}

// unstableKanji finds the first run of kanji in w that splitJapanese would split or read as a number on its own ("ぁ零"), and returns where it is in w and the words it splits into.
func unstableKanji(w string) (from, to int, pieces []string) {
	from = -1

	for i, r := range w + " " {
		if i < len(w) && !isKana(r) && isJapanese(r) {
			if from < 0 {
				from = i
			}

			continue
		}

		if from >= 0 {
			run := w[from:i]
			if pieces = kanjiNumbers(rejoin(tinyseg.Segment(run))); len(pieces) != 1 || pieces[0] != run {
				return from, i, pieces
			}
		}

		from = -1
	}

	return 0, 0, nil
}

// rejoin undoes two of the segmenter's splits: a word that starts with a small kana or ー belongs to the word before it ("うっせ" "ぇわ"), and so does a kanji numeral after one ("千" "九" "百" for 千九百).
func rejoin(words []string) []string {
	out := words[:0]

	for _, w := range words {
		if n := len(out); n > 0 {
			first, _ := utf8.DecodeRuneInString(w)
			last, _ := utf8.DecodeLastRuneInString(out[n-1])

			if strings.ContainsRune(jaSmallKana, first) || strings.ContainsRune(jaNumerals, first) && strings.ContainsRune(jaNumerals, last) {
				out[n-1] += w

				continue
			}
		}

		out = append(out, w)
	}

	return out
}

// splitReading splits a katakana reading of an English number or letter off the end of a katakana word of at least three more characters, where a recogniser writes a sequel's number or a title's letter ("ターミネーターツー", "ドラゴンボールゼット").
func splitReading(w string) []string {
	for _, rd := range jaReadings {
		if rest, ok := strings.CutSuffix(w, rd[0]); ok && utf8.RuneCountInString(rest) >= 3 && isKatakana(rest) {
			return []string{rest, rd[0]}
		}
	}

	return []string{w}
}

func isKatakana(s string) bool {
	for _, r := range s {
		if r != 'ー' && !unicode.Is(unicode.Katakana, r) {
			return false
		}
	}

	return true
}

// kanjiNumbers writes the kanji numeral a word starts with as digits, splitting it from the rest of the word ("七人" -> "7" "人", "二〇二三" -> "2023"), so it meets the digits a recogniser writes. A numeral inside a word ("統一") is part of the word and stays.
func kanjiNumbers(words []string) []string {
	out := words[:0:0]

	for _, w := range words {
		n, runes := 0, 0

		for _, r := range w {
			if !strings.ContainsRune(jaNumerals, r) {
				break
			}

			n += utf8.RuneLen(r)
			runes++
		}

		v, ok := kanjiValue(w[:n])
		if n == 0 || runes > maxKanjiNumeral || !ok {
			out = append(out, w)

			continue
		}

		out = append(out, strconv.Itoa(v))
		if n < len(w) {
			out = append(out, w[n:])
		}
	}

	return out
}

// kanjiValue reads a kanji numeral of at most maxKanjiNumeral characters, digit by digit ("二〇二三" 2023) or with multipliers ("千九百八十四" 1984, "一億二千万" 120000000). It reports false for a numeral whose value does not fit an int ("九九九九九九九九九九九億").
func kanjiValue(s string) (int, bool) {
	total, section, cur := 0, 0, 0
	ok := true

	for _, r := range s {
		switch r {
		case '十':
			section, ok = addMul(section, max(cur, 1), 10, ok)
			cur = 0
		case '百':
			section, ok = addMul(section, max(cur, 1), 100, ok)
			cur = 0
		case '千':
			section, ok = addMul(section, max(cur, 1), 1000, ok)
			cur = 0
		case '万':
			section, ok = addMul(section, cur, 1, ok)
			total, ok = addMul(total, max(section, 1), 10_000, ok)
			section, cur = 0, 0
		case '億':
			section, ok = addMul(section, cur, 1, ok)
			total, ok = addMul(total, max(section, 1), 100_000_000, ok)
			section, cur = 0, 0
		default:
			cur, ok = addMul(jaDigits[r], cur, 10, ok)
		}
	}

	section, ok = addMul(section, cur, 1, ok)
	total, ok = addMul(total, section, 1, ok)

	return total, ok
}

// addMul returns acc + x*m for non-negative operands, and false once the result would pass math.MaxInt or ok is already false.
func addMul(acc, x, m int, ok bool) (int, bool) {
	if !ok || x > (math.MaxInt-acc)/m {
		return 0, false
	}

	return acc + x*m, true
}

// ordinals drops the counter after "第" and a number ("第2部", "第3章"), leaving "第" for dropParts.
func ordinals(words []string) []string {
	out := make([]string, 0, len(words))
	before, prev := "", ""

	for _, w := range words {
		if before != "第" || !isDigits(prev) || utf8.RuneCountInString(w) != 1 || !strings.Contains(jaCounters, w) {
			out = append(out, w)
		}

		before, prev = prev, w
	}

	return out
}

// romanizeKana writes a word in kana in modified Hepburn romaji, without marking long vowels ("ターミネーター" -> "tamineta", "とうきょう" -> "tokyo"), and a number or letter reading as what it reads ("ツー" -> "2"). The particles は and へ on their own are read "wa" and "e". Characters that are not kana stay as they are, apart from the kana around them ("駆ける" -> "駆 keru").
func romanizeKana(w string) string {
	for _, rd := range jaReadings {
		if w == rd[0] {
			return rd[1]
		}
	}

	switch w {
	case "は":
		return "wa"
	case "へ":
		return "e"
	}

	runs := make([]string, 0, 2)
	from, inKana := 0, false

	for i, r := range w {
		if k := isKana(r); i == 0 || k != inKana {
			if i > 0 {
				runs = append(runs, kanaRun(w[from:i], inKana))
			}

			from, inKana = i, k
		}
	}

	return strings.Join(append(runs, kanaRun(w[from:], inKana)), " ")
}

func isKana(r rune) bool {
	_, ok := kanaRomaji[hiragana(r)]

	return ok || r == 'ー' || r == 'っ' || r == 'ッ'
}

// kanaRun is a run of kana in romaji, or a run of other characters as it is.
func kanaRun(run string, kana bool) string {
	if !kana {
		return run
	}

	var b strings.Builder

	sokuon := false

	for i := 0; i < len(run); {
		r, size := utf8.DecodeRuneInString(run[i:])
		i += size

		switch h := hiragana(r); h {
		case 'っ':
			sokuon = true
		case 'ー':
		default:
			syl := kanaRomaji[h]

			if next, size := utf8.DecodeRuneInString(run[i:]); size > 0 {
				if joined, ok := withSmall(syl, hiragana(next)); ok {
					syl = joined
					i += size
				}
			}

			if sokuon && !isVowelByte(syl[0]) && syl != "n" {
				// っ doubles the consonant after it, and makes ch tch.
				if syl[0] == 'c' {
					b.WriteByte('t')
				} else {
					b.WriteByte(syl[0])
				}
			}

			b.WriteString(syl)

			sokuon = false
		}
	}

	return shortVowels(b.String())
}

// hiragana is the hiragana for a katakana letter, or r itself.
func hiragana(r rune) rune {
	if r >= 'ァ' && r <= 'ヶ' {
		return r - ('ァ' - 'ぁ')
	}

	return r
}

func isVowelByte(c byte) bool { return strings.IndexByte("aiueo", c) >= 0 }

// withSmall joins a syllable to a small kana after it: a small ya, yu or yo makes a palatal syllable ("き" "ゃ" -> "kya", "し" "ょ" -> "sho"), and a small vowel the vowels of loanwords ("テ" "ィ" -> "ti", "フ" "ァ" -> "fa", "ウ" "ィ" -> "wi", "ヴ" "ァ" -> "va"). After the vowel it repeats it lengthens it ("せ" "ぇ").
func withSmall(syl string, small rune) (string, bool) {
	v, ok := smallKana[small]
	if !ok || !isVowelByte(syl[len(syl)-1]) {
		return "", false
	}

	stem := syl[:len(syl)-1]

	switch {
	case v[0] == 'y' && (strings.HasSuffix(stem, "sh") || strings.HasSuffix(stem, "ch") || stem == "j"):
		return stem + v[1:], true
	case v[0] == 'y' || v == "wa":
		return stem + v, true
	case syl == "u":
		return "w" + v, true
	case syl == "i":
		return "y" + v, true
	case stem == "" || syl[len(syl)-1] == v[0]:
		return syl + v, true
	default:
		return stem + v, true
	}
}

// shortVowels writes long vowels short, as romaji without macrons does ("tookyoo", "toukyou" -> "tokyo"): a vowel after the same vowel, or u after o, is dropped.
func shortVowels(s string) string {
	b := []byte(s)
	out := b[:0]

	for _, c := range b {
		if n := len(out); n > 0 && isVowelByte(c) && (out[n-1] == c || out[n-1] == 'o' && c == 'u') {
			continue
		}

		out = append(out, c)
	}

	return string(out)
}

// jaReadings are katakana readings of English numbers and letters that recognisers write for a title's digit or letter, and what they read as. エイト, テン and the letters with short readings are left out: they end ordinary words (ウエイト, カーテン).
var jaReadings = [][2]string{
	{"ワン", "1"},
	{"ツー", "2"},
	{"トゥー", "2"},
	{"スリー", "3"},
	{"フォー", "4"},
	{"ファイブ", "5"},
	{"ファイヴ", "5"},
	{"シックス", "6"},
	{"セブン", "7"},
	{"セヴン", "7"},
	{"ナイン", "9"},
	{"ゼット", "z"},
	{"エックス", "x"},
}

// jaDigits are the values of the kanji digits among jaNumerals.
var jaDigits = map[rune]int{'〇': 0, '零': 0, '一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}

// smallKana are the small kana withSmall joins to the syllable before them, as they sound.
var smallKana = map[rune]string{
	'ぁ': "a", 'ぃ': "i", 'ぅ': "u", 'ぇ': "e", 'ぉ': "o", 'ゃ': "ya", 'ゅ': "yu", 'ょ': "yo", 'ゎ': "wa",
}

// kanaRomaji is modified Hepburn for each hiragana; katakana is read as its hiragana. ぢ and づ are written ji and zu as じ and ず are, and small kana standing alone as their full-size forms.
var kanaRomaji = map[rune]string{
	'あ': "a", 'い': "i", 'う': "u", 'え': "e", 'お': "o",
	'か': "ka", 'き': "ki", 'く': "ku", 'け': "ke", 'こ': "ko",
	'が': "ga", 'ぎ': "gi", 'ぐ': "gu", 'げ': "ge", 'ご': "go",
	'さ': "sa", 'し': "shi", 'す': "su", 'せ': "se", 'そ': "so",
	'ざ': "za", 'じ': "ji", 'ず': "zu", 'ぜ': "ze", 'ぞ': "zo",
	'た': "ta", 'ち': "chi", 'つ': "tsu", 'て': "te", 'と': "to",
	'だ': "da", 'ぢ': "ji", 'づ': "zu", 'で': "de", 'ど': "do",
	'な': "na", 'に': "ni", 'ぬ': "nu", 'ね': "ne", 'の': "no",
	'は': "ha", 'ひ': "hi", 'ふ': "fu", 'へ': "he", 'ほ': "ho",
	'ば': "ba", 'び': "bi", 'ぶ': "bu", 'べ': "be", 'ぼ': "bo",
	'ぱ': "pa", 'ぴ': "pi", 'ぷ': "pu", 'ぺ': "pe", 'ぽ': "po",
	'ま': "ma", 'み': "mi", 'む': "mu", 'め': "me", 'も': "mo",
	'や': "ya", 'ゆ': "yu", 'よ': "yo",
	'ら': "ra", 'り': "ri", 'る': "ru", 'れ': "re", 'ろ': "ro",
	'わ': "wa", 'ゐ': "i", 'ゑ': "e", 'を': "o", 'ん': "n", 'ゔ': "vu",
	'ぁ': "a", 'ぃ': "i", 'ぅ': "u", 'ぇ': "e", 'ぉ': "o",
	'ゃ': "ya", 'ゅ': "yu", 'ょ': "yo", 'ゎ': "wa", 'ゕ': "ka", 'ゖ': "ke",
}
