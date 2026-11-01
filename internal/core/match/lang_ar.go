package match

import (
	"strings"
	"unicode/utf8"

	"github.com/nightah/encore/internal/fuzzy/phonetic/ar"
)

// arabicFolds are the Arabic letters written more than one way for one sound, folded to one spelling. NFKD has already dropped the harakat and the hamza on a seat (أ إ آ ؤ ئ); a hamza on the line is dropped too, so "شيء" meets "شي" and "ذئب" meets "ذيب". Persian and Gulf letters for sounds Arabic lacks fold to the Arabic letter people also write: پ for p as ب, ڤ for v as ف, and گ for the Gulf g of ق as ق.
var arabicFolds = map[rune]string{
	'ٱ': "ا", 'ة': "ه", 'ى': "ي", 'ء': "", 'ی': "ي", 'ک': "ك", 'ھ': "ه", 'ہ': "ه",
	'پ': "ب", 'ڤ': "ف", 'ڥ': "ف", 'گ': "ق", 'ڨ': "ق",
}

// arabicTeenStems are the units that "عشر" follows in 11 to 19 ("ثلاثة عشر"), folded.
var arabicTeenStems = map[string]int{
	"احد": 1, "احدي": 1, "اثنا": 2, "اثني": 2, "اثنتا": 2, "اثنتي": 2, "اتنين": 2,
	"ثلاث": 3, "ثلاثه": 3, "تلات": 3, "تلاته": 3, "اربع": 4, "اربعه": 4, "خمس": 5, "خمسه": 5,
	"ست": 6, "سته": 6, "سبع": 7, "سبعه": 7, "ثمان": 8, "ثماني": 8, "ثمانيه": 8, "تسع": 9, "تسعه": 9,
}

// arabicOrdinalStems are the ordinals that "عشر" follows in 11th to 19th ("الثالث عشر"), folded and without the article.
var arabicOrdinalStems = map[string]int{
	"حادي": 1, "حاديه": 1, "ثاني": 2, "ثانيه": 2, "تاني": 2, "تانيه": 2, "ثالث": 3, "ثالثه": 3, "تالت": 3, "تالته": 3,
	"رابع": 4, "رابعه": 4, "خامس": 5, "خامسه": 5, "سادس": 6, "سادسه": 6, "سابع": 7, "سابعه": 7,
	"ثامن": 8, "ثامنه": 8, "تامن": 8, "تاسع": 9, "تاسعه": 9,
}

// arabicCounted are number words that carry their count, rewritten as the count and the word: the dual ("الفين", two thousand) and the plural after three to ten ("آلاف" in "ثلاثة آلاف"), folded. "الفين" and "الاف" start like the article, so they are rewritten before the article is stripped.
var arabicCounted = map[string][]string{
	"الفين": {"اثنين", "الف"}, "الفان": {"اثنين", "الف"}, "الاف": {"الف"},
	"مليونين": {"اثنين", "مليون"}, "مليونان": {"اثنين", "مليون"}, "ملايين": {"مليون"},
}

// arabicArticles are the article and its clitic forms ("وال" and, "بال" with, "فال" so, "كال" like, "لل" for the).
var arabicArticles = []string{"ال", "وال", "بال", "فال", "كال", "لل", "ولل"}

// arabicLatin writes each Arabic consonant in Latin letters as English spellings of Arabic names do: the emphatic consonants as their plain pairs (ص s, ض d, ط t), ذ and ظ as th ("Theeb" for ذيب, "Kathem" for كاظم), ح as h like ه, and ع as the vowel it colours.
var arabicLatin = map[rune]string{
	'ب': "b", 'ت': "t", 'ث': "th", 'ج': "j", 'ح': "h", 'خ': "kh", 'د': "d", 'ذ': "th", 'ر': "r", 'ز': "z",
	'س': "s", 'ش': "sh", 'ص': "s", 'ض': "d", 'ط': "t", 'ظ': "th", 'ع': "a", 'غ': "gh", 'ف': "f", 'ق': "q",
	'ك': "k", 'ل': "l", 'م': "m", 'ن': "n", 'ه': "h", 'چ': "ch", 'ژ': "zh",
}

func arabic() *language {
	nums := arabicNumbers()

	return &language{
		code: "ar",
		half: "ونص",
		and:  "و",
		// "ذا" and "زا" are English "the" as Arabic speakers say it ("ذا قودفاذر").
		articles:         romanizeSet("ذا", "زا", "the", "a", "an"),
		articlePrefixes:  arabicArticles,
		articlePrefixMin: 2,
		folds:            arabicFolds,
		split:            arabicSplit(nums),
		romanize:         romanizeArabic,
		native:           nativeOf("ar"),
		stop: romanizeSet(
			"في", "من", "عن", "مع", "و", "يا", "ف", "ب", "ل", "ذا", "زا",
			"the", "a", "an", "of", "and", "in", "on", "to", "for", "at", "with",
		),
		ordSuffixes: []string{"st", "nd", "rd", "th"},
		nums:        romanizeNums(nums),
		unitsFirst:  true,
		conj:        romanizeSet("و", "and"),
		parts:       romanizeSet("جز", "موسم", "فصل", "مجلد", "بارت", "part", "chapter", "volume", "vol"),
		phonetic:    phoneticKeysOf(ar.Encoder{}),
	}
}

// arabicNumbers is the Arabic number vocabulary in Arabic script, folded: standard and Gulf and Egyptian forms, and the English number words of English titles.
func arabicNumbers() map[string]numWord {
	eng := english()

	compounds := map[string]int{}

	for stem, v := range arabicTeenStems {
		for _, ten := range []string{"عشر", "عشره"} {
			compounds[stem+ten] = 10 + v
		}
	}

	ordinals := map[string]int{
		"اول": 1, "اولي": 1, "ثاني": 2, "تاني": 2, "ثانيه": 2, "تانيه": 2, "ثالث": 3, "تالت": 3, "ثالثه": 3, "تالته": 3,
		"رابع": 4, "رابعه": 4, "خامس": 5, "خامسه": 5, "سادس": 6, "سادسه": 6, "سابع": 7, "سابعه": 7,
		"ثامن": 8, "ثامنه": 8, "تامن": 8, "تاسع": 9, "تاسعه": 9, "عاشر": 10, "عاشره": 10,
	}

	for stem, v := range arabicOrdinalStems {
		for _, ten := range []string{"عشر", "عشره"} {
			ordinals[stem+ten] = 10 + v
		}
	}

	extraUnits := map[string]int{
		"واحده": 1, "اثنان": 2, "اتنين": 2, "ثنين": 2, "اثنتين": 2, "اثنتان": 2, "ثلاث": 3, "تلاته": 3, "تلات": 3,
		"اربع": 4, "خمس": 5, "ست": 6, "سبع": 7, "ثمان": 8, "ثماني": 8, "تمانيه": 8, "تمنيه": 8, "تسع": 9, "عشر": 10,
		// Eleven to nineteen as Gulf and Egyptian speech run them together.
		"حداشر": 11, "احداشر": 11, "احدعش": 11, "اتناشر": 12, "اطناشر": 12, "اثنعش": 12, "ثنعش": 12,
		"تلتاشر": 13, "ثلطعش": 13, "ثلاثطعش": 13, "اربعتاشر": 14, "اربعطعش": 14, "خمستاشر": 15, "خمسطعش": 15,
		"ستاشر": 16, "سطعش": 16, "سبعتاشر": 17, "سبعطعش": 17, "تمنتاشر": 18, "ثمنطعش": 18, "تسعتاشر": 19, "تسعطعش": 19,
		// Sequel numbers said in English and written in Arabic letters ("فروزن تو").
		"تو": 2, "ثري": 3, "فور": 4, "فايف": 5, "سفن": 7,
	}

	extraTen := map[string]int{
		"عشرون": 20, "ثلاثون": 30, "تلاتين": 30, "اربعون": 40, "خمسون": 50, "ستون": 60, "سبعون": 70,
		"ثمانون": 80, "تمانين": 80, "تسعون": 90,
	}

	hundredsOf := map[string]int{"ميتين": 200, "مايتين": 200, "ميتان": 200, "مايتان": 200}

	for i, u := range []string{"ثلاث", "اربع", "خمس", "ست", "سبع", "ثمان", "تسع"} {
		for _, h := range []string{"ميه", "مايه"} {
			hundredsOf[u+h] = (i + 3) * 100
		}
	}

	// Libraries in Arabic hold English titles too, so English number words, articles and parts are read as well.
	for w, n := range eng.nums {
		switch {
		case n.ordinal:
			ordinals[w] = n.val
		case n.kind == numTens:
			extraTen[w] = n.val
		case n.kind == numUnit:
			extraUnits[w] = n.val
		}
	}

	return numTable{
		units:      []string{"صفر", "واحد", "اثنين", "ثلاثه", "اربعه", "خمسه", "سته", "سبعه", "ثمانيه", "تسعه", "عشره"},
		tens:       []string{"عشرين", "ثلاثين", "اربعين", "خمسين", "ستين", "سبعين", "ثمانين", "تسعين"},
		extraUnits: extraUnits,
		extraTen:   extraTen,
		hundreds:   []string{"ميه", "مايه", "hundred"},
		hundredsOf: hundredsOf,
		thousands:  []string{"الف", "thousand"},
		millions:   []string{"مليون", "million"},
		compounds:  compounds,
		ordinals:   ordinals,
	}.build()
}

// romanizeSet is set over the words as romanizeArabic writes them, since Arabic compares words in Latin letters.
func romanizeSet(words ...string) map[string]bool {
	for i, w := range words {
		words[i] = romanizeArabic(w)
	}

	return set(words...)
}

func romanizeNums(nums map[string]numWord) map[string]numWord {
	out := make(map[string]numWord, len(nums))
	for w, n := range nums {
		out[romanizeArabic(w)] = n
	}

	return out
}

// arabicSplit returns the split hook for Arabic. It writes the definite article as it is spelt where a request spells it as it is said ("انناظر", "ار رسالة" and "اتتغريبة" for "الناظر", "الرسالة" and "التغريبة"): an alef and a doubled sun letter starting a word of five letters or more are taken as the article said aloud, though a few words spelt with a doubled letter after an alef ("اسس") are taken too, and a separate "ان" stays the word "أن" or "إن". It parts a name built on "عبد" from the word after it ("عبدالله" -> "عبد الله"), separates the "و" ("and") fused to a number word after another ("واحد وعشرين"), joins a unit or an ordinal to the "عشر" after it ("ثلاثة عشر", "الحادي عشر") and writes a number word that carries its count as the count and the word (arabicCounted), so the numbers read. nums is the number vocabulary in Arabic script, since split runs before romanize.
func arabicSplit(nums map[string]numWord) func([]string) []string {
	isNum := func(tok string) bool {
		w, ok := nums[tok]

		return ok && !w.ordinal
	}

	return func(toks []string) []string {
		out := make([]string, 0, len(toks))

		for i := 0; i < len(toks); i++ {
			tok := toks[i]
			r := []rune(tok)

			next := ""
			if i+1 < len(toks) {
				next = toks[i+1]
			}

			switch {
			case arabicCounted[tok] != nil:
				out = append(out, arabicCounted[tok]...)

				continue
			case len(r) >= 5 && r[0] == 'ا' && r[1] == r[2] && strings.ContainsRune(arabicSun, r[1]):
				tok = "ال" + string(r[2:])
			case len(r) == 2 && r[0] == 'ا' && strings.ContainsRune(arabicSun, r[1]) && tok != "ان" && next != "" &&
				(r[1] == 'ل' || strings.HasPrefix(next, string(r[1]))):
				tok = "ال" + next
				i++
			case strings.HasPrefix(tok, "عبدال") && utf8.RuneCountInString(tok) >= 7:
				out = append(out, "عبد")
				tok = tok[len("عبد"):]
			case strings.HasPrefix(tok, "و") && len(out) > 0 && isNum(out[len(out)-1]) && isNum(tok[len("و"):]):
				out = append(out, "و")
				tok = tok[len("و"):]
			case (next == "عشر" || next == "عشره") && (arabicTeenStems[tok] > 0 || arabicOrdinalStems[withoutArticle(tok)] > 0):
				tok += next
				i++
			}

			out = append(out, tok)
		}

		return out
	}
}

// withoutArticle returns tok without the longest of arabicArticles it starts with.
func withoutArticle(tok string) string {
	cut := 0

	for _, p := range arabicArticles {
		if len(p) > cut && strings.HasPrefix(tok, p) {
			cut = len(p)
		}
	}

	return tok[cut:]
}

// romanizeArabic writes an Arabic token as a Latin consonant skeleton (arabicLatin), so an Arabic request and an English title asked for in Arabic letters meet ("تايتانك", "titanic"). Short vowels are not written in Arabic and stay out; the long vowels are their letters: ا as a, و and ي as u and i after a consonant and as w and y at the start of a word or after a vowel. A final ه is a ta marbuta, folded to ه, and is written a, as it is said. A token without Arabic letters is returned as it is.
func romanizeArabic(tok string) string {
	if !strings.ContainsFunc(tok, isArabicLetter) {
		return tok
	}

	var b strings.Builder

	b.Grow(len(tok))

	_, size := utf8.DecodeLastRuneInString(tok)
	last := len(tok) - size
	// vowel is whether the letter before was a vowel, or this is the first: و and ي are consonants there.
	vowel := true

	for i, r := range tok {
		switch {
		case r == 'و', r == 'ي':
			switch {
			case r == 'و' && vowel:
				b.WriteByte('w')
			case r == 'و':
				b.WriteByte('u')
			case vowel:
				b.WriteByte('y')
			default:
				b.WriteByte('i')
			}

			vowel = !vowel
		case r == 'ا', r == 'ع', r == 'ه' && i == last && i > 0:
			b.WriteByte('a')

			vowel = true
		default:
			if s, ok := arabicLatin[r]; ok {
				b.WriteString(s)
			} else {
				b.WriteRune(r)
			}

			vowel = false
		}
	}

	return b.String()
}

// isArabicLetter reports whether r is in the Arabic block, where the letters romanizeArabic writes are.
func isArabicLetter(r rune) bool { return r >= 0x0600 && r <= 0x06FF }
