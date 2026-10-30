package loanword

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func TestNativeJapanese(t *testing.T) {
	for in, want := range map[string][]string{
		"Superfly":                   {"スーパーフライ"},
		"GLAY":                       {"グレイ"},
		"Queen":                      {"クイーン"},
		"The Beatles":                {"ザ ビートルズ", "ビートルズ"},
		"Star Wars":                  {"スター ウォーズ"},
		"GHOST IN THE SHELL / 攻殻機動隊": {"ゴースト イン ザ シェル / 攻殻機動隊"},
		"機動戦士ガンダムSEED":               {"機動戦士ガンダムシード"},
		"ゴッドファーザー PART II":           {"ゴッドファーザー パート II"},
		"Frozen":                     {"フローズン"},
		"Home Alone":                 {"ホーム アローン"},
		"HOWEVER":                    {"ハウエヴァー"},
		"E.T.":                       {"イーティー"},
		"AKB48":                      {"エーケービー48"},
		"ONE OK ROCK":                {"ワン オーケー ロック"},
		"back number":                {"バック ナンバー"},
		"DREAMS COME TRUE":           {"ドリームズ カム トルー"},
		"Mrs. GREEN APPLE":           {"ミシズ. グリーン アップル"},
		"Creepy Nuts":                {"クリーピー ナッツ"},
		"Get Wild":                   {"ゲット ワイルド"},
		"Harry Potter":               {"ハリー ポッター"},
		"Mission: Impossible":        {"ミッション: インポッシブル"},
		"The Godfather":              {"ザ ゴッドファーザー", "ゴッドファーザー"},
		"Squid Game":                 {"スクイッド ゲーム"},
		"Dune":                       {"デューン"},
		"Newton":                     {"ニュートン"},
		"Spider-Man: No Way Home":    {"スパイダー-マン: ノー ウェイ ホーム"},
		"The Walking Dead":           {"ザ ウォーキング デッド", "ウォーキング デッド"},
		"Batman Begins":              {"バットマン ビギンズ"},
		"Rocky V":                    {"ロッキー V"},
		"Stranger Things":            {"ストレーンジャー シングズ"},
		"Calm Down":                  {"カーム ダウン"},
		"Vikings":                    {"ヴァイキングズ"},
		"A Wednesday!":               {"ア ウェンズデイ!"},
		"Amélie":                     {"アメリー"},
		"Rock'n'Roll":                {"ロックンロル"},
		"The":                        {"ザ"},
	} {
		assert.Equal(t, want, Native("ja-JP", in), in)
	}
}

func TestNativeHindi(t *testing.T) {
	for in, want := range map[string][]string{
		"Frozen":                  {"फ्रोजन", "फ्रोजेन"},
		"Mr. India":               {"मिस्टर. इंडिया", "म्र. इंदिआ"},
		"Deewaar":                 {"डीवैआर", "दीवार"},
		"Zanjeer":                 {"जैंजियर", "जंजीर"},
		"Yeh Jawaani Hai Deewani": {"ये जॉऐऐनी हे डीवॉनी", "ये जवानी है दीवनी"},
		"The Beatles":             {"द बीटल्स", "थे बेअत्लेस"},
		"K.G.F: Chapter 1":        {"केजीएफ: चैप्टर 1", "केजीएफ: चप्तेर 1"},
		"RRR":                     {"आरआरआर"},
		"R. D. Burman":            {"आरडी बरमैन", "आरडी बुर्मन"},
		"Mission: Impossible":     {"मिशन: इंपॉसिबल", "मिस्सिओन: इम्पोस्सिब्ले"},
		"Stranger Things":         {"स्ट्रेंजर थिंग्स", "स्त्रंगेर थिंग्स"},
		"Chhota Bheem":            {"च्होटा ब्हीम", "छोता भीम"},
		"Krrish":                  {"क्रिश", "क्र्रिश"},
		"Pushpa":                  {"पुश्पा", "पुश्पा"[:0] + "पुश्पा"},
	} {
		got := Native("hi-IN", in)
		assert.Equal(t, dedupe(want), got, in)
	}
}

func TestNativeArabic(t *testing.T) {
	for in, want := range map[string][]string{
		"Home Alone":     {"هوم الون"},
		"Avatar":         {"افاتار"},
		"Dune":           {"ديون"},
		"Believer":       {"بيليفر"},
		"The Godfather":  {"ذا قودفاذر"},
		"Braveheart":     {"بريفهارت"},
		"Hello":          {"هيلو"},
		"Vikings":        {"فايكينقز"},
		"BTS":            {"بي تي اس"},
		"E.T.":           {"اي تي"},
		"Batman Begins":  {"باتمان بيقينز"},
		"Superfly":       {"سوبرفلاي"},
		"Oppenheimer":    {"اوبنهايمر"},
		"Things":         {"ثينقز"},
		"Pink":           {"بينك"},
		"Inception":      {"انسيبشن"},
		"Iron Man":       {"ايرن مان"},
		"Up":             {"اب"},
		"Eat Pray Love":  {"ايت بري لف"},
		"Ocean's Eleven": {"اوشنز اليفن"},
	} {
		assert.Equal(t, want, Native("ar-SA", in), in)
	}
}

func dedupe(ss []string) []string {
	var out []string

	for _, s := range ss {
		if !contains(out, s) {
			out = append(out, s)
		}
	}

	return out
}

func TestNativeLeavesOtherTextAlone(t *testing.T) {
	assert.Nil(t, Native("en", "Superfly"), "a language without a writer")
	assert.Nil(t, Native("de-DE", "Superfly"))
	assert.Nil(t, Native("ja", "千と千尋の神隠し"), "no Latin letters")
	assert.Nil(t, Native("ja", ""))
	assert.Nil(t, Native("ja", "II"), "only a numeral")
	assert.Nil(t, Native("ar", "تايتانك 2"))
	assert.True(t, Supports("ja-JP"))
	assert.True(t, Supports("hi_IN"))
	assert.True(t, Supports("AR"))
	assert.False(t, Supports("fr"))
}

func TestNativeForms(t *testing.T) {
	assert.Equal(t, []string{"ザ ビートルズ", "ビートルズ"}, Native("ja", "The Beatles"), "Japanese also drops a leading The")
	assert.Equal(t, []string{"ザ"}, Native("ja", "The"), "but not when nothing follows")
	assert.Equal(t, []string{"ヒア カムズ ザ サン"}, Native("ja", "Here Comes the Sun"), "only a leading The")
	assert.Equal(t, []string{"「ザ ビートルズ」"}, Native("ja", "「The Beatles」"), "a The after other text is not leading")
	assert.Equal(t, []string{"ザ ビートルズ", "ビートルズ"}, Native("ja", "  The Beatles"), "spaces before it don't count")
	assert.LessOrEqual(t, len(Native("hi", "The Fast and the Furious")), maxForms)
}

func TestInitialisms(t *testing.T) {
	for in, want := range map[string]bool{
		"BTS": true, "AKB": true, "OK": true, "TV": true, "NHK": true, "UK": true,
		"GLAY": false, "ZARD": false, "SEED": false, "ONE": false, "FOR": false, "GO": false, "DREAMS": false, "ALCHEMIST": false,
		"SMAP": false, "ABBA": false,
	} {
		assert.Equal(t, want, unsayable(fold(in)), in)
	}

	for in, want := range map[string]string{
		"X JAPAN":   "エックス ジャパン",
		"Malcolm X": "マルコルム X",
		"A Day":     "ア デイ",
		"I Robot":   "アイ ローボット",
		"U2":        "ユー2",
		"Jay-Z":     "ジェイ-ゼット",
		"S.H.E":     "エスエイチイー",
		"Ne-Yo":     "ニー-ヨー",
	} {
		assert.Equal(t, []string{want}, Native("ja", in), in)
	}
}

func TestEnglish(t *testing.T) {
	for in, want := range map[string]string{
		"game":       "g ei m",
		"stone":      "s t ou n",
		"rises":      "r ai s i z",
		"games":      "g ei m z",
		"boxes":      "b o k s i z",
		"cats":       "k ae t s",
		"named":      "n ei m d",
		"wanted":     "w o n t i d",
		"jumped":     "j a m p t",
		"making":     "m ei k i ng",
		"walking":    "w oo k i ng",
		"running":    "r a n i ng",
		"nation":     "n ae sh @ n",
		"vision":     "v i zh @ n",
		"mansion":    "m ae n sh @ n",
		"picture":    "p i k ch @ R",
		"phone":      "f ou n",
		"ghost":      "g ou s t",
		"night":      "n ai t",
		"eight":      "ei t",
		"thought":    "th oo t",
		"tough":      "t a f",
		"dough":      "d ou",
		"daughter":   "d oo t er R",
		"chorus":     "ch ou r a s",
		"christ":     "k r i s t",
		"school":     "s k uu l",
		"knife":      "n ai f",
		"write":      "r ai t",
		"psycho":     "s i ch ou",
		"lamb":       "l ae m",
		"autumn":     "oo t a m",
		"quick":      "k w i Q k",
		"xenon":      "z e n o n",
		"taxi":       "t ae k s ii",
		"bridge":     "b r i j",
		"magic":      "m ae j i k",
		"city":       "s i t ii",
		"accept":     "ae k s e p t",
		"cycle":      "s i k @ l",
		"music":      "m y uu z i k",
		"super":      "s uu p er R",
		"cute":       "k y uu t",
		"rude":       "r uu d",
		"care":       "k ea R",
		"fire":       "f ai @ R",
		"pure":       "p y uu @ R",
		"sure":       "sh oo R",
		"hour":       "h au @ R",
		"court":      "k oo R t",
		"poor":       "p oo R",
		"clear":      "k l ia R",
		"learn":      "l er R n",
		"beer":       "b ia R",
		"word":       "w er R d",
		"warm":       "w oo R m",
		"quarter":    "k w oo R t er R",
		"bird":       "b er R d",
		"rain":       "r ei n",
		"law":        "l oo",
		"head":       "h e d",
		"death":      "d e th",
		"heaven":     "h e v @ n",
		"sea":        "s ii",
		"vein":       "v ei n",
		"honey":      "h ou n ii",
		"they":       "dh ei",
		"new":        "n y uu",
		"blue":       "b l uu",
		"pie":        "p ai",
		"movie":      "m ou v ii",
		"boat":       "b ou t",
		"toe":        "t ou",
		"boy":        "b oi",
		"book":       "b u k",
		"moon":       "m uu n",
		"house":      "h au s",
		"show":       "sh ou",
		"now":        "n au",
		"known":      "n ou n",
		"down":       "d au n",
		"suit":       "s uu t",
		"guitar":     "g i t aa R",
		"buy":        "b ai",
		"talk":       "t oo k",
		"ball":       "b oo l",
		"salt":       "s oo l t",
		"want":       "w o n t",
		"pizza":      "p i z @",
		"seven":      "s e v @ n",
		"kind":       "k ai n d",
		"wild":       "w ai l d",
		"bold":       "b ou l d",
		"host":       "h ou s t",
		"lost":       "l o s t",
		"hotel":      "h ou t e l",
		"push":       "p u sh",
		"cup":        "k a p",
		"unit":       "y uu n i t",
		"my":         "m ai",
		"happy":      "h ae Q p ii",
		"gym":        "g i m",
		"skyfall":    "s k i f oo l",
		"tiger":      "t ai g er R",
		"love":       "l a v",
		"glove":      "g l a v",
		"stove":      "s t ou v",
		"yes":        "y e s",
		"yellow":     "y e l ou",
		"hello":      "h e l ou",
		"carry":      "k ae r ii",
		"famous":     "f ae m @ s",
		"godfather":  "g o d f aa dh @ R",
		"braveheart": "b r ei v h aa R t",
		"he":         "h ii",
		"watch":      "w o ch",
		"white":      "w ai t",
		"page":       "p ei j",
		"orange":     "o r ae n j",
		"rosa":       "r ou z @",
		"earth":      "er R th",
		"hearts":     "h aa R t s",
		"hearty":     "h aa R t ii",
		"worth":      "w er R th",
		"fort":       "f oo R t",
		"grey":       "g r ei",
		"theme":      "th ii m",
		"menu":       "m e n y uu",
		"ah":         "ae",
	} {
		assert.Equal(t, want, strings.Join(english(in), " "), in)
	}
}

func TestWriters(t *testing.T) {
	assert.Equal(t, "पिंक", devanagari(english("pink")), "ng before k is the anusvara alone")
	assert.Equal(t, "अ", romanHindi("a1"), "a character that is no letter is skipped")
	assert.Equal(t, "キュ", palatal("k", "uu"))
	assert.Equal(t, "キ", palatal("k", "i"))
	assert.Equal(t, "シュ", palatal("s", "uu"))
	assert.Equal(t, "ジョ", palatal("j", "o"))
	assert.Equal(t, "テュ", palatal("t", "uu"))
	assert.Equal(t, "ヤ", palatal("q", "a"), "a consonant without a row writes the glide alone")
	assert.Equal(t, "チ", alone("ch"))
	assert.Equal(t, "ジ", alone("j"))
	assert.Empty(t, alone("h"))
	assert.Empty(t, alone("q"))
	assert.Equal(t, "トゥー", katakana([]phone{"t", "uu"}))
	assert.Equal(t, "キュート", katakana(english("cute")))
	assert.Equal(t, "アウ", katakana([]phone{"au"}))
	assert.Equal(t, "キャイ", katakana([]phone{"k", "y", "ai"}), "a diphthong after a glide keeps its second vowel")

	var b strings.Builder
	writeVowel(&b, "q", "a", "", func() {})
	assert.Equal(t, "ア", b.String(), "a consonant without a row writes the vowel alone")

	r := &reader{w: "ae"}
	r.emit("a")
	r.u(false)
	assert.Equal(t, []phone{"a", "uu"}, r.out, "a long u after a vowel has no glide")

	for in, want := range map[string]string{
		"Straße": "strasse", "Æon": "aeon", "Œuvre": "oeuvre", "Øresund": "oresund", "Łódź": "lodz", "Đorđe": "dorde",
		"Þór": "thor", "ıstanbul": "istanbul", "Ðe": "de",
	} {
		assert.Equal(t, want, fold(in), in)
	}
}

// TestPropNative checks that Native never panics, writes no Latin letters for a language it supports and keeps whatever isn't a Latin word.
func TestPropNative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[A-Za-zéüß'.\- 0-9千ア]{0,30}`).Draw(t, "s")
		lang := rapid.SampledFrom([]string{"ja", "hi", "ar"}).Draw(t, "lang")

		for _, f := range Native(lang, s) {
			for _, r := range f {
				if isLatin(r) && !strings.ContainsRune("IVX", r) {
					t.Fatalf("Native(%q, %q) left a Latin letter in %q", lang, s, f)
				}
			}

			if strings.Contains(s, "千") && !strings.Contains(f, "千") {
				t.Fatalf("Native(%q, %q) = %q lost the kanji", lang, s, f)
			}
		}
	})
}
