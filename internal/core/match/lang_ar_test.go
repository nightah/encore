package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestNormalizeArabic(t *testing.T) {
	tests := []struct{ in, want string }{
		// Arabic is compared in Latin letters: the emphatics as their plain pairs, ذ and ظ as th, a final ta marbuta as a.
		{"وجدة", "wjda"},
		{"وجده", "wjda"},
		{"طاش ما طاش", "tash ma tash"},
		{"تاش ما تاش", "tash ma tash"},
		{"صعيدي", "saydi"},
		{"كاظم الساهر", "kathm sahr"},
		{"شيرين", "shirin"},
		{"عمرو دياب", "amru diab"},
		{"اولاد", "awlad"},
		{"تايتانك", "taytank"},
		{"يوم", "yum"},
		// Hamza seats, a hamza on the line, alif maqsura, alef wasla, harakat and tatweel.
		{"أم كلثوم", "am klthum"},
		{"إسماعيلية", "asmaaylia"},
		{"شيء", "shi"},
		{"ذئب", "thib"},
		{"البريء", "bri"},
		{"اللمبى", "lmbi"},
		{"ٱلرسالة", "rsala"},
		{"مُحَمَّد", "mhmd"},
		{"محـــمد", "mhmd"},
		// Persian and Gulf letters for sounds Arabic lacks.
		{"ڤايكنقز", "fayknqz"},
		{"پيكي", "biki"},
		{"ناگة", "naqa"},
		// The article, fused or said as its assimilation, and its clitic forms.
		{"الإرهاب والكباب", "arhab kbab"},
		{"بالبيت فالبيت كالبيت للبنات وللبنات", "bit bit bit bnat bnat"},
		{"انناظر", "nathr"},
		{"ار رسالة", "rsala"},
		{"ال رسالة", "rsala"},
		{"اتتغريبة الفلسطينية", "tghriba flstinia"},
		{"الحب", "hb"},
		{"الف", "1000"},
		{"بالي", "bali"},
		// An alef and a doubled letter only start the article said aloud in a word long enough to hold it, and "أن" stays a word.
		{"اسس", "ass"},
		{"إننا", "anna"},
		{"اددم", "addm"},
		{"قبل أن نشاهد", "qbl an nshahd"},
		// Names built on عبد, written as one word or two.
		{"عبدالمجيد عبدالله", "abd mjid abd la"},
		{"عبد المجيد عبد الله", "abd mjid abd la"},
		{"عبدال", "abdal"},
		// English "the" said ذا or زا, and English titles as they are.
		{"ذا قودفاذر بارت تو", "qudfathr 2"},
		{"زا جودفازر", "judfazr"},
		{"The Godfather Part II", "godfather 2"},
		{"Fast & Furious", "fast w furious"},
		{"21st Century", "21 century"},
		// Numbers: digits in either script, number words standard and dialect, units before the tens, teens, hundreds and thousands.
		{"الفيل الأزرق ٢", "fil azrq 2"},
		{"الفيل الازرق اتنين", "fil azrq 2"},
		{"الفيل الازرق الجزء الثاني", "fil azrq 2"},
		{"الفيل الازرق التاني", "fil azrq 2"},
		{"ولاد رزق تلاتة", "wlad rzq 3"},
		{"الموسم الثالث", "3"},
		{"واحد وعشرين", "21"},
		{"خمسة و عشرين", "25"},
		{"ثلاثة عشر", "13"},
		{"احدى عشرة", "11"},
		{"اتناشر", "12"},
		{"مية وخمسة", "105"},
		{"ميتين", "200"},
		{"ثلاثمية", "300"},
		{"ثلاث مية", "300"},
		{"ألف ليلة وليلة", "1000 lila wlila"},
		{"ألفين", "2000"},
		{"الفين وعشرين", "2020"},
		{"ثلاثة آلاف", "3000"},
		{"مليون", "1000000"},
		{"مليونين", "2000000"},
		{"خمسة ملايين", "5000000"},
		// Ordinals from the eleventh to the nineteenth, masculine and feminine, with and without the article.
		{"الجزء الحادي عشر", "11"},
		{"الحلقة الحادية عشرة", "hlqa 11"},
		{"الموسم الثاني عشر", "12"},
		{"التاسع عشر", "19"},
		{"تاسعة عشرة", "19"},
		{"والثالث عشر", "13"},
		{"حادي", "hadi"},
		{"فروزن تو", "fruzn 2"},
		{"فيوريوس سفن", "fiwriws 7"},
		{"Frozen Two", "frozen 2"},
		// A و that only looks like one joined to a number stays.
		{"وخمسة", "wkhmsa"},
		{"عشر", "10"},
		{"ثلاثة", "3"},
		// ½ is "ونص".
		{"8½", "8 wns"},
		{"ثمانية ونص", "8 wns"},
		// The locale is enough.
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, Normalize("ar", tt.in))
		})
	}

	assert.Equal(t, "wjda", Normalize("ar-SA", "وجدة"))
}

func TestArabicSplit(t *testing.T) {
	split := languages["ar"].split

	for _, tc := range []struct{ in, want []string }{
		{[]string{"انناظر"}, []string{"الناظر"}},
		{[]string{"اببيت"}, []string{"اببيت"}},
		{[]string{"ار", "رساله"}, []string{"الرساله"}},
		{[]string{"ار", "ساله"}, []string{"ار", "ساله"}},
		{[]string{"ار"}, []string{"ار"}},
		{[]string{"ان", "نشاهد"}, []string{"ان", "نشاهد"}},
		{[]string{"اسس"}, []string{"اسس"}},
		{[]string{"اننا"}, []string{"اننا"}},
		{[]string{"الحادي", "عشر"}, []string{"الحاديعشر"}},
		{[]string{"وال", "عشر"}, []string{"وال", "عشر"}},
		{[]string{"الفين"}, []string{"اثنين", "الف"}},
		{[]string{"ثلاثه", "الاف"}, []string{"ثلاثه", "الف"}},
		{[]string{"ملايين"}, []string{"مليون"}},
		{[]string{"ال", "بيت"}, []string{"البيت"}},
		{[]string{"عبدالله"}, []string{"عبد", "الله"}},
		{[]string{"عبدال"}, []string{"عبدال"}},
		{[]string{"واحد", "وعشرين"}, []string{"واحد", "و", "عشرين"}},
		{[]string{"ولد", "وعشرين"}, []string{"ولد", "وعشرين"}},
		{[]string{"واحد", "وولد"}, []string{"واحد", "وولد"}},
		{[]string{"ثلاثه", "عشر"}, []string{"ثلاثهعشر"}},
		{[]string{"ثلاثه"}, []string{"ثلاثه"}},
		{[]string{"ولد", "عشر"}, []string{"ولد", "عشر"}},
		{nil, []string{}},
	} {
		assert.Equal(t, tc.want, split(tc.in), "%q", tc.in)
	}
}

func TestRomanizeArabic(t *testing.T) {
	for in, want := range map[string]string{
		"ثذظ":     "ththth",
		"صضطظ":    "sdtth",
		"خغشچژ":   "khghshchzh",
		"عين":     "ayn",
		"ويوي":    "wiwi",
		"ه":       "h",
		"هه":      "ha",
		"titanic": "titanic",
		"٣":       "٣",
		"ۓ":       "ۓ",
	} {
		assert.Equal(t, want, romanizeArabic(in), in)
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

	for in, want := range map[string][]string{
		"محمد علي":       {"mhmd", "ali"},
		"علي بابا":       {"ali", "baba"},
		"رحلة إلى القمر": {"rhla", "ali", "qmr"},
		"بيت في المدينة": {"bit", "mdina"},
	} {
		qf := l.newForm(l.normalizeSpelled(in, false))
		assert.Equal(t, want, l.queryTokens(&qf), in)
	}
}
