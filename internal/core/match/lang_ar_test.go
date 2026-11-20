package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestNormalizeArabic(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		// Arabic is compared in Latin letters: the emphatics as their plain pairs, ذ and ظ as th, a final ta marbuta as a.
		{name: "ShouldReadWjdaWithTaMarbuta", have: "وجدة", expected: "wjda"},
		{name: "ShouldReadWjdaWithFinalHa", have: "وجده", expected: "wjda"},
		{name: "ShouldReadEmphaticTaAsT", have: "طاش ما طاش", expected: "tash ma tash"},
		{name: "ShouldReadTashMaTash", have: "تاش ما تاش", expected: "tash ma tash"},
		{name: "ShouldReadSaydi", have: "صعيدي", expected: "saydi"},
		{name: "ShouldReadKathmSahr", have: "كاظم الساهر", expected: "kathm sahr"},
		{name: "ShouldReadShirin", have: "شيرين", expected: "shirin"},
		{name: "ShouldReadAmruDiab", have: "عمرو دياب", expected: "amru diab"},
		{name: "ShouldReadAwlad", have: "اولاد", expected: "awlad"},
		{name: "ShouldReadTaytank", have: "تايتانك", expected: "taytank"},
		{name: "ShouldReadYum", have: "يوم", expected: "yum"},
		// Hamza seats, a hamza on the line, alif maqsura, alef wasla, harakat and tatweel.
		{name: "ShouldReadAmKlthum", have: "أم كلثوم", expected: "am klthum"},
		{name: "ShouldReadAsmaaylia", have: "إسماعيلية", expected: "asmaaylia"},
		{name: "ShouldReadShi", have: "شيء", expected: "shi"},
		{name: "ShouldReadThib", have: "ذئب", expected: "thib"},
		{name: "ShouldReadBri", have: "البريء", expected: "bri"},
		{name: "ShouldReadHighHamzaAlefAsA", have: "ٵ", expected: "a"},
		{name: "ShouldDropHighHamzaAfterLatin", have: "aٴ", expected: "a"},
		{name: "ShouldReadLmbi", have: "اللمبى", expected: "lmbi"},
		{name: "ShouldDropArticleWithAlefWasla", have: "ٱلرسالة", expected: "rsala"},
		{name: "ShouldDropHarakat", have: "مُحَمَّد", expected: "mhmd"},
		{name: "ShouldDropTatweel", have: "محـــمد", expected: "mhmd"},
		// Persian and Gulf letters for sounds Arabic lacks.
		{name: "ShouldReadFayknqz", have: "ڤايكنقز", expected: "fayknqz"},
		{name: "ShouldReadBiki", have: "پيكي", expected: "biki"},
		{name: "ShouldReadNaqa", have: "ناگة", expected: "naqa"},
		// The article, fused or said as its assimilation, and its clitic forms.
		{name: "ShouldReadArhabKbab", have: "الإرهاب والكباب", expected: "arhab kbab"},
		{name: "ShouldReadBitBitBitBnatBnat", have: "بالبيت فالبيت كالبيت للبنات وللبنات", expected: "bit bit bit bnat bnat"},
		{name: "ShouldReadNathr", have: "انناظر", expected: "nathr"},
		{name: "ShouldDropAssimilatedArticleSaidApart", have: "ار رسالة", expected: "rsala"},
		{name: "ShouldDropArticleWrittenApart", have: "ال رسالة", expected: "rsala"},
		{name: "ShouldReadTghribaFlstinia", have: "اتتغريبة الفلسطينية", expected: "tghriba flstinia"},
		{name: "ShouldReadHb", have: "الحب", expected: "hb"},
		{name: "ShouldReadAlfAsThousand", have: "الف", expected: "1000"},
		{name: "ShouldReadBali", have: "بالي", expected: "bali"},
		// An alef and a doubled letter only start the article said aloud in a word long enough to hold it, and "أن" stays a word.
		{name: "ShouldReadAss", have: "اسس", expected: "ass"},
		{name: "ShouldReadAnna", have: "إننا", expected: "anna"},
		{name: "ShouldReadAddm", have: "اددم", expected: "addm"},
		{name: "ShouldReadQblAnNshahd", have: "قبل أن نشاهد", expected: "qbl an nshahd"},
		// Names built on عبد, written as one word or two.
		{name: "ShouldSplitAbdNamesWrittenAsOneWord", have: "عبدالمجيد عبدالله", expected: "abd mjid abd la"},
		{name: "ShouldReadAbdNamesWrittenAsTwoWords", have: "عبد المجيد عبد الله", expected: "abd mjid abd la"},
		{name: "ShouldReadAbdal", have: "عبدال", expected: "abdal"},
		// English "the" said ذا or زا, and English titles as they are.
		{name: "ShouldReadQudfathr2", have: "ذا قودفاذر بارت تو", expected: "qudfathr 2"},
		{name: "ShouldReadJudfazr", have: "زا جودفازر", expected: "judfazr"},
		{name: "ShouldReadGodfather2", have: "The Godfather Part II", expected: "godfather 2"},
		{name: "ShouldReadFastWFurious", have: "Fast & Furious", expected: "fast w furious"},
		{name: "ShouldRead21Century", have: "21st Century", expected: "21 century"},
		// Numbers: digits in either script, number words standard and dialect, units before the tens, teens, hundreds and thousands.
		{name: "ShouldReadArabicIndicDigit", have: "الفيل الأزرق ٢", expected: "fil azrq 2"},
		{name: "ShouldReadDialectNumberWord", have: "الفيل الازرق اتنين", expected: "fil azrq 2"},
		{name: "ShouldReadPartOrdinal", have: "الفيل الازرق الجزء الثاني", expected: "fil azrq 2"},
		{name: "ShouldReadDialectOrdinal", have: "الفيل الازرق التاني", expected: "fil azrq 2"},
		{name: "ShouldReadDialectThree", have: "ولاد رزق تلاتة", expected: "wlad rzq 3"},
		{name: "ShouldReadSeasonOrdinal", have: "الموسم الثالث", expected: "3"},
		{name: "ShouldReadUnitsBeforeTens", have: "واحد وعشرين", expected: "21"},
		{name: "ShouldReadUnitsBeforeTensWithSpacedWa", have: "خمسة و عشرين", expected: "25"},
		{name: "ShouldReadTeen", have: "ثلاثة عشر", expected: "13"},
		{name: "ShouldReadFeminineTeen", have: "احدى عشرة", expected: "11"},
		{name: "ShouldReadDialectTeen", have: "اتناشر", expected: "12"},
		{name: "ShouldReadHundredAndUnits", have: "مية وخمسة", expected: "105"},
		{name: "ShouldReadDualHundred", have: "ميتين", expected: "200"},
		{name: "ShouldReadHundredsAsOneWord", have: "ثلاثمية", expected: "300"},
		{name: "ShouldReadHundredsAsTwoWords", have: "ثلاث مية", expected: "300"},
		{name: "ShouldReadThousandAndOneNights", have: "ألف ليلة وليلة", expected: "1000 lila wlila"},
		{name: "ShouldReadDualThousand", have: "ألفين", expected: "2000"},
		{name: "ShouldReadDualThousandAndTens", have: "الفين وعشرين", expected: "2020"},
		{name: "ShouldReadPluralThousands", have: "ثلاثة آلاف", expected: "3000"},
		{name: "ShouldReadMillion", have: "مليون", expected: "1000000"},
		{name: "ShouldReadDualMillion", have: "مليونين", expected: "2000000"},
		{name: "ShouldReadPluralMillions", have: "خمسة ملايين", expected: "5000000"},
		// Ordinals from the eleventh to the nineteenth, masculine and feminine, with and without the article.
		{name: "ShouldReadEleventhPart", have: "الجزء الحادي عشر", expected: "11"},
		{name: "ShouldReadFeminineEleventh", have: "الحلقة الحادية عشرة", expected: "hlqa 11"},
		{name: "ShouldReadTwelfthSeason", have: "الموسم الثاني عشر", expected: "12"},
		{name: "ShouldReadNineteenthWithArticle", have: "التاسع عشر", expected: "19"},
		{name: "ShouldReadFeminineNineteenthWithoutArticle", have: "تاسعة عشرة", expected: "19"},
		{name: "ShouldReadThirteenthAfterWa", have: "والثالث عشر", expected: "13"},
		{name: "ShouldKeepHadiAloneAsWord", have: "حادي", expected: "hadi"},
		{name: "ShouldReadEnglishTwoInArabicLetters", have: "فروزن تو", expected: "fruzn 2"},
		{name: "ShouldReadEnglishSevenInArabicLetters", have: "فيوريوس سفن", expected: "fiwriws 7"},
		{name: "ShouldReadFrozen2", have: "Frozen Two", expected: "frozen 2"},
		// A و that only looks like one joined to a number stays.
		{name: "ShouldKeepWaJoinedToNumberAsWord", have: "وخمسة", expected: "wkhmsa"},
		{name: "ShouldReadTen", have: "عشر", expected: "10"},
		{name: "ShouldReadThree", have: "ثلاثة", expected: "3"},
		// ½ is "ونص".
		{name: "ShouldReadHalfAsWns", have: "8½", expected: "8 wns"},
		{name: "ShouldReadSpokenHalf", have: "ثمانية ونص", expected: "8 wns"},
		// A letter of the Arabic block without a Latin form, such as ػ, is a word of its own, as a second pass would make it; a leading a is then the article.
		{name: "ShouldKeepLetterWithoutLatinFormAsWord", have: "آػ", expected: "ػ"},
		{name: "ShouldReadLeadingAAfterLetterWithoutLatinFormAsArticle", have: "ػآ", expected: "ػ a"},
		// Only an Arabic letter is set apart: a letter of another script keeps its marks.
		{name: "ShouldOnlySetArabicLetterApart", have: "ؠᛮ\u0300", expected: "ؠ ᛮ\u0300"},
		// The locale is enough.
		{name: "ShouldNormalizeEmpty"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize("ar", tc.have)
			assert.Equal(t, tc.expected, got)
			assert.Equal(t, got, Normalize("ar", got), "normalising it again")
		})
	}

	assert.Equal(t, "wjda", Normalize("ar-SA", "وجدة"))
}

func TestArabicSplit(t *testing.T) {
	split := languages["ar"].split

	testCases := []struct {
		name     string
		have     []string
		expected []string
	}{
		{name: "ShouldRestoreAssimilatedArticle", have: []string{"انناظر"}, expected: []string{"الناظر"}},
		{name: "ShouldKeepDoubledLetterThatIsNoArticle", have: []string{"اببيت"}, expected: []string{"اببيت"}},
		{name: "ShouldJoinArticleSaidApart", have: []string{"ار", "رساله"}, expected: []string{"الرساله"}},
		{name: "ShouldNotJoinArticleBeforeOtherLetter", have: []string{"ار", "ساله"}, expected: []string{"ار", "ساله"}},
		{name: "ShouldKeepArticleAlone", have: []string{"ار"}, expected: []string{"ار"}},
		{name: "ShouldKeepAnBeforeVerb", have: []string{"ان", "نشاهد"}, expected: []string{"ان", "نشاهد"}},
		{name: "ShouldKeepShortWordWithDoubledLetter", have: []string{"اسس"}, expected: []string{"اسس"}},
		{name: "ShouldKeepAnnaAsWord", have: []string{"اننا"}, expected: []string{"اننا"}},
		{name: "ShouldJoinCompoundOrdinal", have: []string{"الحادي", "عشر"}, expected: []string{"الحاديعشر"}},
		{name: "ShouldNotJoinWaAlBeforeTen", have: []string{"وال", "عشر"}, expected: []string{"وال", "عشر"}},
		{name: "ShouldSplitDualThousand", have: []string{"الفين"}, expected: []string{"اثنين", "الف"}},
		{name: "ShouldSingulariseThousandsAfterNumber", have: []string{"ثلاثه", "الاف"}, expected: []string{"ثلاثه", "الف"}},
		{name: "ShouldSingulariseMillions", have: []string{"ملايين"}, expected: []string{"مليون"}},
		{name: "ShouldJoinArticleWrittenApart", have: []string{"ال", "بيت"}, expected: []string{"البيت"}},
		{name: "ShouldSplitAbdName", have: []string{"عبدالله"}, expected: []string{"عبد", "الله"}},
		{name: "ShouldKeepAbdalAsWord", have: []string{"عبدال"}, expected: []string{"عبدال"}},
		{name: "ShouldSplitWaBeforeTensAfterNumber", have: []string{"واحد", "وعشرين"}, expected: []string{"واحد", "و", "عشرين"}},
		{name: "ShouldKeepWaBeforeTensAfterWord", have: []string{"ولد", "وعشرين"}, expected: []string{"ولد", "وعشرين"}},
		{name: "ShouldKeepWaBeforeWordAfterNumber", have: []string{"واحد", "وولد"}, expected: []string{"واحد", "وولد"}},
		{name: "ShouldJoinTeen", have: []string{"ثلاثه", "عشر"}, expected: []string{"ثلاثهعشر"}},
		{name: "ShouldKeepUnitAlone", have: []string{"ثلاثه"}, expected: []string{"ثلاثه"}},
		{name: "ShouldNotJoinTenAfterWord", have: []string{"ولد", "عشر"}, expected: []string{"ولد", "عشر"}},
		{name: "ShouldSplitNothing", expected: []string{}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, split(tc.have))
		})
	}
}

func TestRomanizeArabic(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldWriteInterdentalsAsTh", have: "ثذظ", expected: "ththth"},
		{name: "ShouldWriteEmphaticsAsPlainPairs", have: "صضطظ", expected: "sdtth"},
		{name: "ShouldWriteDigraphs", have: "خغشچژ", expected: "khghshchzh"},
		{name: "ShouldWriteAyn", have: "عين", expected: "ayn"},
		{name: "ShouldWriteWawAndYaBetweenLetters", have: "ويوي", expected: "wiwi"},
		{name: "ShouldWriteLoneHa", have: "ه", expected: "h"},
		{name: "ShouldWriteFinalHaAsA", have: "هه", expected: "ha"},
		{name: "ShouldKeepLatin", have: "titanic", expected: "titanic"},
		{name: "ShouldKeepArabicIndicDigit", have: "٣", expected: "٣"},
		{name: "ShouldKeepLetterWithoutLatinForm", have: "ۓ", expected: "ۓ"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, romanizeArabic(tc.have))
		})
	}
}

// No two Arabic number words of different values romanize to the same word, so reading numbers in Latin letters reads them as written.
func TestArabicNumbersRomanizeApart(t *testing.T) {
	seen := map[string]string{}

	for w, n := range arabicNumbers() {
		r := romanizeArabic(w)
		if prev, ok := seen[r]; ok {
			assert.Equal(t, arabicNumbers()[prev].val, n.val, "%s and %s both romanize to %s", prev, w, r)
		}

		seen[r] = w
	}
}

func TestPropArabicNormalizeIsLatinOrScript(t *testing.T) {
	letters := []rune("ءآأؤإئابةتثجحخدذرزسشصضطظعغفقكلمنهوىيٱپچژڤکگیھ ًٌٍَُِّْـ٠١٢٣٤٥٦٧٨٩")

	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringOfN(rapid.SampledFrom(letters), 0, 30, -1).Draw(t, "s")
		got := Normalize("ar", s)

		require.Equal(t, got, Normalize("ar", got), "normalising twice changes nothing")

		for _, r := range got {
			require.True(t, r == ' ' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9', "%q has %q", got, r)
		}

		require.Equal(t, strings.Join(strings.Fields(got), " "), got)
	})
}

// The name Ali (علي) is written like the preposition على once alif maqsura folds, and إلى is written ali too, so neither is a stop word: "محمد علي" searches for both words.
func TestArabicQueryTokensKeepAli(t *testing.T) {
	l := languages["ar"]

	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldKeepAliAfterName", have: "محمد علي", expected: []string{"mhmd", "ali"}},
		{name: "ShouldKeepLeadingAli", have: "علي بابا", expected: []string{"ali", "baba"}},
		{name: "ShouldKeepIla", have: "رحلة إلى القمر", expected: []string{"rhla", "ali", "qmr"}},
		{name: "ShouldDropFi", have: "بيت في المدينة", expected: []string{"bit", "mdina"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			qf := l.newForm(l.normalizeSpelled(tc.have, false))
			assert.Equal(t, tc.expected, l.queryTokens(&qf))
		})
	}
}
