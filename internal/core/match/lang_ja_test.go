package match

import (
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestNormalizeJapanese(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		// Segmented, kana in romaji, kanji kept, particles as stop words.
		{name: "ShouldSegmentKanjiAndKana", have: "千と千尋の神隠し", expected: "1000 to 1000 尋 no 神隠 shi"},
		{name: "ShouldReadParticleHaAsWa", have: "君の名は。", expected: "君 no 名 wa"},
		{name: "ShouldSplitLatinFromKana", have: "abcの歌", expected: "abc no 歌"},
		{name: "ShouldSplitLatinFromKanji", have: "Official髭男dism", expected: "official 髭男 dism"},
		{name: "ShouldReadAmpersandAsTo", have: "トム&ジェリー", expected: "tomu to jeri"},
		{name: "ShouldReadHalfAsHan", have: "8½", expected: "8 半"},
		// Hiragana, katakana, half-width and full-width forms meet.
		{name: "ShouldReadHiraganaLongVowel", have: "とうきょう", expected: "tokyo"},
		{name: "ShouldReadKatakanaLongVowelMark", have: "トーキョー", expected: "tokyo"},
		{name: "ShouldReadHalfWidthKatakana", have: "ｶﾀｶﾅ", expected: "katakana"},
		{name: "ShouldReadFullWidthLatin", have: "ＡＢＣ", expected: "abc"},
		// Long vowels, small kana and sokuon.
		{name: "ShouldReadSmallVowelAsLong", have: "うっせぇわ", expected: "ussewa"},
		{name: "ShouldReadLongVowelMarkAfterSokuon", have: "うっせーわ", expected: "ussewa"},
		{name: "ShouldReadPonyo", have: "ポニョ", expected: "ponyo"},
		{name: "ShouldReadRapyuta", have: "ラピュタ", expected: "rapyuta"},
		{name: "ShouldReadGakko", have: "がっこう", expected: "gakko"},
		{name: "ShouldReadMatchi", have: "マッチ", expected: "matchi"},
		{name: "ShouldReadSmallKanaWithParticles", have: "ティファニーで朝食を", expected: "tifani de 朝食 o"},
		{name: "ShouldReadVuAsB", have: "ヴァイオレット", expected: "baioretto"},
		{name: "ShouldReadWiki", have: "ウィキ", expected: "wiki"},
		{name: "ShouldReadShefu", have: "シェフ", expected: "shefu"},
		{name: "ShouldReadDyuetto", have: "デュエット", expected: "dyuetto"},
		{name: "ShouldReadFainarufantaji", have: "ファイナルファンタジー", expected: "fainarufantaji"},
		{name: "ShouldReadYei", have: "イェーイ", expected: "yei"},
		// Separators split words, and digits and Latin letters stand apart.
		{name: "ShouldSplitAtMiddleDot", have: "トイ・ストーリー3", expected: "toi sutori 3"},
		{name: "ShouldSplitDigitsFromKanji", have: "東京2020", expected: "東京 2020"},
		{name: "ShouldSplitAtPunctuation", have: "ゴジラ-1.0", expected: "gojira 1 0"},
		// Latin words keep their vowels.
		{name: "ShouldReadLatinPartAndRomanNumeral", have: "ゴッドファーザー PART II", expected: "goddofaza 2"},
		{name: "ShouldKeepVowelsOfLatinWord", have: "Cool", expected: "cool"},
		// Readings of numbers and letters at the end of a katakana word.
		{name: "ShouldReadFinalNumberReading", have: "トイストーリーフォー", expected: "toisutori 4"},
		{name: "ShouldReadFinalTsuuAsTwo", have: "ターミネーターツー", expected: "tamineta 2"},
		{name: "ShouldReadFinalLetterReading", have: "ドラゴンボールゼット", expected: "doragonboru z"},
		{name: "ShouldReadWanAsOne", have: "ワン", expected: "1"},
		{name: "ShouldNotReadWanInsideShortWord", have: "スワン", expected: "suwan"},
		// Kanji numerals that start a word, positional or with multipliers.
		{name: "ShouldReadLeadingKanjiNumeral", have: "七人の侍", expected: "7 人 no 侍"},
		{name: "ShouldReadLeadingDigitBeforeCounter", have: "7人の侍", expected: "7 人 no 侍"},
		{name: "ShouldReadKanjiNumeralWithMultipliers", have: "千九百八十四", expected: "1984"},
		{name: "ShouldReadPositionalKanjiNumeral", have: "二〇二三年", expected: "2023 年"},
		{name: "ShouldReadMan", have: "二万", expected: "20000"},
		{name: "ShouldReadOkuAndMan", have: "一億二千万", expected: "120000000"},
		{name: "ShouldNotReadNumeralThatEndsWord", have: "統一", expected: "統一"},
		{name: "ShouldKeepNumeralTooLongToRead", have: "一二三四五六七八九〇一二三", expected: "一二三四五六七八九〇一二三"},
		// Parts and 第 with a counter.
		{name: "ShouldReadDaiWithCounter", have: "第二部", expected: "2"},
		{name: "ShouldReadDaiWithDigitAndCounter", have: "第2章", expected: "2"},
		{name: "ShouldDropSeasonWord", have: "シーズン2", expected: "2"},
		{name: "ShouldDropPartWord", have: "パート2", expected: "2"},
		{name: "ShouldKeepCounterWithoutDai", have: "2部", expected: "2 部"},
		{name: "ShouldKeepDaiAlone", have: "第", expected: "第"},
		{name: "ShouldNormalizeEmpty"},
		{name: "ShouldDropLoneLongVowelMark", have: "ー"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize("ja-JP", tc.have))
		})
	}
}

func TestKanjiValue(t *testing.T) {
	t.Run("ShouldReadNumerals", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     string
			expected int
		}{
			{name: "ShouldReadOne", have: "一", expected: 1},
			{name: "ShouldReadTen", have: "十", expected: 10},
			{name: "ShouldReadTenAndUnit", have: "十一", expected: 11},
			{name: "ShouldReadMultipliedTen", have: "二十", expected: 20},
			{name: "ShouldReadHundredAndTens", have: "百二十", expected: 120},
			{name: "ShouldReadThousands", have: "三千", expected: 3000},
			{name: "ShouldReadYear", have: "千九百八十四", expected: 1984},
			{name: "ShouldReadPositionalNumeral", have: "二〇二三", expected: 2023},
			{name: "ShouldReadZero", have: "零"},
			{name: "ShouldReadMan", have: "万", expected: 10_000},
			{name: "ShouldReadManAndThousands", have: "二万五千", expected: 25_000},
			{name: "ShouldReadOku", have: "一億", expected: 100_000_000},
			{name: "ShouldReadOkuAndMan", have: "一億二千万", expected: 120_000_000},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				actual, ok := kanjiValue(tc.have)
				assert.True(t, ok)
				assert.Equal(t, tc.expected, actual)
			})
		}
	})

	t.Run("ShouldRejectNumeralsThatDoNotFitAnInt", func(t *testing.T) {
		testCases := []struct {
			name string
			have string
		}{
			{name: "ShouldRejectOkuOfManyNines", have: "九九九九九九九九九九九億"},
			{name: "ShouldRejectOkuOfPositionalNumeral", have: "一〇〇〇〇〇〇〇〇〇〇〇億"},
			{name: "ShouldRejectLongPositionalNumeral", have: "九九九九九九九九九九九九九九九九九九九九"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, ok := kanjiValue(tc.have)
				assert.False(t, ok, "does not fit an int")
			})
		}
	})

	assert.Equal(t, []string{"九九九九九九九九九九九億"}, kanjiNumbers([]string{"九九九九九九九九九九九億"}), "a numeral too large to read stays a word")
}

func TestRomanizeKana(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldReadParticleHaAsWa", have: "は", expected: "wa"},
		{name: "ShouldReadParticleHeAsE", have: "へ", expected: "e"},
		{name: "ShouldReadParticleWoAsO", have: "を", expected: "o"},
		{name: "ShouldReadTsuuAsTwo", have: "ツー", expected: "2"},
		{name: "ShouldReadZettoAsZ", have: "ゼット", expected: "z"},
		{name: "ShouldReadKya", have: "きゃ", expected: "kya"},
		{name: "ShouldReadSho", have: "しょ", expected: "sho"},
		{name: "ShouldReadChu", have: "ちゅ", expected: "chu"},
		{name: "ShouldReadJa", have: "じゃ", expected: "ja"},
		{name: "ShouldReadNyo", have: "にょ", expected: "nyo"},
		{name: "ShouldReadObsoleteWiWeAsVowels", have: "ゐゑ", expected: "ie"},
		{name: "ShouldReadFa", have: "ファ", expected: "fa"},
		{name: "ShouldReadTi", have: "ティ", expected: "ti"},
		{name: "ShouldReadTu", have: "トゥ", expected: "tu"},
		{name: "ShouldReadWo", have: "ウォ", expected: "wo"},
		{name: "ShouldReadYe", have: "イェ", expected: "ye"},
		{name: "ShouldReadKwaAsKa", have: "クァ", expected: "ka"},
		{name: "ShouldReadVaAsBa", have: "ヴァ", expected: "ba"},
		{name: "ShouldReadVuAsBu", have: "ヴ", expected: "bu"},
		{name: "ShouldReadViAsBi", have: "ヴィ", expected: "bi"},
		{name: "ShouldReadVeAsBe", have: "ヴェ", expected: "be"},
		{name: "ShouldReadVoAsBo", have: "ヴォ", expected: "bo"},
		{name: "ShouldReadHiraganaVuAsBu", have: "ゔ", expected: "bu"},
		{name: "ShouldReadGwa", have: "グヮ", expected: "gwa"},
		{name: "ShouldFoldSmallVowelAfterSameVowel", have: "せぇ", expected: "se"},
		{name: "ShouldFoldSmallAAfterA", have: "あぁ", expected: "a"},
		{name: "ShouldFoldSmallOAfterO", have: "おぉ", expected: "o"},
		{name: "ShouldReadSmallYaAfterN", have: "んゃ", expected: "nya"},
		{name: "ShouldReadLoneSmallA", have: "ぁ", expected: "a"},
		{name: "ShouldReadLoneSmallYa", have: "ゃ", expected: "ya"},
		{name: "ShouldReadSmallKa", have: "ヵ", expected: "ka"},
		{name: "ShouldReadSmallKe", have: "ヶ", expected: "ke"},
		{name: "ShouldDoubleConsonantAfterSokuon", have: "っか", expected: "kka"},
		{name: "ShouldReadSokuonBeforeChiAsT", have: "っち", expected: "tchi"},
		{name: "ShouldDropSokuonBeforeVowel", have: "っあ", expected: "a"},
		{name: "ShouldDropSokuonBeforeN", have: "っん", expected: "n"},
		{name: "ShouldDropLoneSokuon", have: "っ"},
		{name: "ShouldDropLoneLongVowelMark", have: "ー"},
		{name: "ShouldSplitKanjiFromKana", have: "駆ける", expected: "駆 keru"},
		{name: "ShouldSplitKanjiFromSuffix", have: "子ちゃん", expected: "子 chan"},
		{name: "ShouldKeepLatin", have: "abc", expected: "abc"},
		{name: "ShouldKeepLatinVowels", have: "cool", expected: "cool"},
		{name: "ShouldKeepKanji", have: "髭男", expected: "髭男"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, romanizeKana(tc.have))
		})
	}
}

func TestSplitJapanese(t *testing.T) {
	t.Run("ShouldSplitJapanese", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     []string
			expected []string
		}{
			{name: "ShouldSplitSentenceAndReadNumerals", have: []string{"リビングで千と千尋の神隠しを再生"}, expected: []string{"リビング", "で", "1000", "と", "1000", "尋", "の", "神隠", "し", "を", "再生"}},
			{name: "ShouldSplitLatinFromKanji", have: []string{"official髭男dism"}, expected: []string{"official", "髭男", "dism"}},
			{name: "ShouldSplitDigitsFromWords", have: []string{"ストーリー3", "abc12"}, expected: []string{"ストーリー", "3", "abc", "12"}},
			{name: "ShouldKeepSmallVowelWithWord", have: []string{"うっせぇわ"}, expected: []string{"うっせぇわ"}},
			{name: "ShouldReadOrdinalWithCounter", have: []string{"第三章"}, expected: []string{"第", "3"}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, splitJapanese(tc.have))
			})
		}
	})

	t.Run("ShouldRejoin", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     []string
			expected []string
		}{
			{name: "ShouldRejoinSmallVowelAndNumeral", have: []string{"うっせ", "ぇわ", "千", "九百", "八十四", "人"}, expected: []string{"うっせぇわ", "千九百八十四", "人"}},
			{name: "ShouldKeepWordWithNothingBeforeToJoin", have: []string{"ー"}, expected: []string{"ー"}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, rejoin(tc.have))
			})
		}
	})

	t.Run("ShouldSplitReading", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     string
			expected []string
		}{
			{name: "ShouldSplitFinalNumberReading", have: "ブラックスワン", expected: []string{"ブラックス", "ワン"}},
			{name: "ShouldNotSplitWithTooShortWordBefore", have: "スワン", expected: []string{"スワン"}},
			{name: "ShouldNotSplitWordThatIsNotKatakana", have: "あいうえおワン", expected: []string{"あいうえおワン"}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, splitReading(tc.have))
			})
		}
	})

	t.Run("ShouldReadOrdinals", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     []string
			expected []string
		}{
			{name: "ShouldDropCounterAfterDaiAndNumber", have: []string{"第", "2", "部", "部", "第", "2"}, expected: []string{"第", "2", "部", "第", "2"}},
			{name: "ShouldDropEachCounterAfterDaiAndNumber", have: []string{"第", "2", "部", "第", "3", "章"}, expected: []string{"第", "2", "第", "3"}},
			{name: "ShouldKeepCounterWithoutDai", have: []string{"2", "部"}, expected: []string{"2", "部"}},
			{name: "ShouldKeepCounterOnlyAfterNumber", have: []string{"第", "x", "部"}, expected: []string{"第", "x", "部"}},
			{name: "ShouldOnlyDropCounterOfItsOwn", have: []string{"第", "2", "部品"}, expected: []string{"第", "2", "部品"}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, ordinals(tc.have))
			})
		}
	})
}

// TestJapaneseVFoldsToB checks that ヴ is keyed as the b line, as Japanese says it, so a title spelt with ブ or バ and a request spelt with ヴ meet, and the other way round.
func TestJapaneseVFoldsToB(t *testing.T) {
	ix, err := New("ja", Config{})
	require.NoError(t, err)
	require.NoError(t, ix.Upsert(
		Doc{ID: "lotr", Kind: "movie", Title: "ロード・オブ・ザ・リング"},
		Doc{ID: "drive", Kind: "movie", Title: "ドライブ・マイ・カー"},
		Doc{ID: "longvacation", Kind: "tvshow", Title: "ロングバケーション"},
		Doc{ID: "plastic", Kind: "song", Title: "プラスティック・ラブ"},
		Doc{ID: "violet", Kind: "tvshow", Title: "ヴァイオレット・エヴァーガーデン"},
	))

	testCases := []struct {
		name     string
		heard    string
		expected string
	}{
		{name: "ShouldFindOAsVoInRequest", heard: "ロードオヴザリング", expected: "lotr"},
		{name: "ShouldFindBuAsVuInRequest", heard: "ドライヴマイカー", expected: "drive"},
		{name: "ShouldFindBaAsVaInRequest", heard: "ロングヴァケーション", expected: "longvacation"},
		{name: "ShouldFindBuAsVuAtEndOfRequest", heard: "プラスティックラヴ", expected: "plastic"},
		{name: "ShouldFindVaAsBaInTitle", heard: "バイオレットエバーガーデン", expected: "violet"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ix.Search(t.Context(), Query{Text: tc.heard})
			require.NoError(t, err)

			best, _ := res.Best()
			assert.Equal(t, DecisionPlay, res.Decision)
			assert.Equal(t, tc.expected, best.Doc.ID)
		})
	}
}

// TestJapaneseUnspacedCoverage checks that a word a title spells across a segmenter's word boundary still counts towards coverage, so a missing sequel is asked about and a short form is found.
func TestJapaneseUnspacedCoverage(t *testing.T) {
	ix, err := New("ja", Config{})
	require.NoError(t, err)
	require.NoError(t, ix.Upsert(
		Doc{ID: "toy3", Kind: "movie", Title: "トイ・ストーリー3"},
		Doc{ID: "toy4", Kind: "movie", Title: "トイ・ストーリー4"},
		Doc{ID: "potter", Kind: "movie", Title: "ハリー・ポッターと賢者の石"},
	))

	res, err := ix.Search(t.Context(), Query{Text: "トイストーリー5"})
	require.NoError(t, err)
	assert.Equal(t, DecisionAsk, res.Decision, "a missing sequel")

	q := ix.lang.newForm(ix.lang.normalize("トイストーリー", false), "")
	a := ix.lang.newForm(ix.lang.normalize("トイ・ストーリー3", false), "")
	short := ix.lang.newForm([]string{"to", "toi"}, "")
	digits := ix.lang.newForm([]string{"3"}, "")
	other := ix.lang.newForm([]string{"x", "13"}, "")

	testCases := []struct {
		name     string
		query    *form
		title    *form
		unspaced bool
		expected float64
	}{
		{name: "ShouldCoverWordSpeltAcrossSegmentBoundary", query: &q, title: &a, unspaced: true, expected: 1.0},
		{name: "ShouldMatchWordsOnlyInSpacedLanguage", query: &q, title: &a},
		{name: "ShouldMatchWordShorterThanMinUnspacedCoverAsWord", query: &short, title: &a, unspaced: true, expected: 3.0 / 5},
		{name: "ShouldNeverMatchNumberInsideAnother", query: &digits, title: &other, unspaced: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.expected, coverage(tc.query, tc.title, tc.unspaced), 1e-9)
		})
	}
}

// TestPropNormalizeJapaneseRomanizes checks that no kana survives normalisation and every word is non-empty, whatever the mix of scripts.
func TestPropNormalizeJapaneseRomanizes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[ぁ-ゖァ-ヶー一-龠〇0-9a-zA-Z・、。 ]{0,40}`).Draw(t, "s")
		toks := languageFor("ja").normalize(s, false)

		for _, tok := range toks {
			require.NotEmpty(t, tok)
			require.False(t, strings.ContainsFunc(tok, func(r rune) bool {
				return unicode.In(r, unicode.Hiragana, unicode.Katakana)
			}), "kana left in %q from %q", tok, s)
		}
	})
}

// TestSplitAloneKeepsWordCoverage checks that a spaced language with a split hook still covers query words by whole words only: substring coverage belongs to unspaced languages.
func TestSplitAloneKeepsWordCoverage(t *testing.T) {
	ix, err := New("en", Config{})
	require.NoError(t, err)

	q := ix.lang.newForm([]string{"toyst"}, "")
	a := alias{form: ix.lang.newForm([]string{"toy", "story"}, ""), text: "Toy Story"}
	want := ix.scoreAlias(&q, &a, -1)

	split := *ix.lang
	split.split = func(toks []string) []string { return toks }
	ix.lang = &split
	assert.Equal(t, want, ix.scoreAlias(&q, &a, -1))

	split.unspaced = true
	assert.Greater(t, ix.scoreAlias(&q, &a, -1).score, want.score, "an unspaced language covers a word inside the title")
}

// TestNormalizeJapaneseIdempotent checks that only the Japanese scripts are segmented, a run in another script staying one word (or going through the rules of the language that reads it) and a combining mark on the letter before it, and that normalising the result again changes nothing.
func TestNormalizeKanjiNumeralBeforeAMark(t *testing.T) {
	testCases := []struct {
		name     string
		lang     string
		have     string
		expected string
	}{
		// A mark stays with the numeral before it, whatever its script, and a digit can't carry it.
		{name: "ShouldKeepArabicMarkWithKanjiZeroInGerman", lang: "de", have: "〇ؐ", expected: "0"},
		{name: "ShouldKeepArabicMarkWithKanjiZeroInJapanese", lang: "ja", have: "〇ؐ", expected: "0"},
		{name: "ShouldKeepCombiningAccentWithKanjiSeven", lang: "ja", have: "七\u0301", expected: "7"},
		{name: "ShouldKeepDevanagariVowelSignWithKanjiTwoInHindi", lang: "hi", have: "二ि", expected: "2"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.lang, tc.have)
			assert.Equal(t, tc.expected, got)
			assert.Equal(t, got, Normalize(tc.lang, got), "normalising it again")
		})
	}
}

func TestNormalizeJapaneseIdempotent(t *testing.T) {
	t.Run("ShouldNormalize", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     string
			expected string
		}{
			{name: "ShouldKeepCyrillicAsOneWord", have: "Москва", expected: "москва"},
			{name: "ShouldKeepGreekAsOneWord", have: "Ελλάδα", expected: "ελλαδα"},
			{name: "ShouldReadDevanagariAsHindi", have: "दिल चाहता है", expected: "dil chahta hai"},
			{name: "ShouldSplitIterationMarkFromDevanagari", have: "々अ", expected: "々 a"},
			// A mark of another script after a Devanagari letter isn't carried onto its romanisation, where a second pass would drop it.
			{name: "ShouldNotCarryOtherMarkOntoDevanagariVowel", have: "अ\u0300", expected: "a"},
			{name: "ShouldNotCarryOtherMarkOntoDevanagariWord", have: "दिल\u0301", expected: "dil"},
			{name: "ShouldKeepMarkOnRune", have: "ᛮ̀", expected: "ᛮ̀"},
			{name: "ShouldSplitKatakanaFromCyrillic", have: "モスクワМосква", expected: "mosukuwa москва"},
			{name: "ShouldDropMarkOnKatakana", have: "ア́", expected: "a"},
			{name: "ShouldDropMarkInsideKanjiWord", have: "日́本", expected: "日本"},
			{name: "ShouldDropMarkInsideKatakanaWord", have: "テ̀スト", expected: "tesuto"},
			{name: "ShouldDropSemiVoicedMarkOnKa", have: "か゚", expected: "ka"},
			// A kanji word is split as the segmenter splits it alone, and a kanji numeral after kana is read.
			{name: "ShouldSplitKanjiWordAsSegmenterDoesAlone", have: "いい日旅立ち", expected: "i 日 旅立 chi"},
			{name: "ShouldReadKanjiNumeralBeforeWord", have: "二十世紀少年", expected: "20 世 紀 少年"},
			{name: "ShouldReadKanjiNumeralAfterKana", have: "ぁ零", expected: "a 0"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				got := Normalize("ja", tc.have)
				assert.Equal(t, tc.expected, got)
				assert.Equal(t, got, Normalize("ja", got), "normalising it again")
			})
		}
	})

	t.Run("ShouldNormalizeAgainToItself", func(t *testing.T) {
		testCases := []struct {
			name string
			have string
		}{
			{name: "ShouldNormalizeHangulAgainToItself", have: "한국 영화"},
			{name: "ShouldNormalizeKanjiAndKanaAgainToItself", have: "千と千尋の神隠し"},
			{name: "ShouldNormalizeLongKanjiTitleAgainToItself", have: "鬼滅の刃無限列車編"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				got := Normalize("ja", tc.have)
				assert.Equal(t, got, Normalize("ja", got), "normalising it again")
			})
		}
	})
}

// TestPropNormalizeJapaneseIdempotent checks that Japanese normalisation is idempotent on text that mixes kana, kanji, numerals, other scripts and combining marks, which the general property test rarely draws.
func TestPropNormalizeJapaneseIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[ぁ-ゖァ-ヶー々一-龠〇零十百千万億第部0-9a-zА-яα-ωअ-ह\x{093F}\x{0300}\x{3099}\x{309A}・ ]{0,40}`).Draw(t, "s")
		once := Normalize("ja", s)

		if twice := Normalize("ja", once); twice != once {
			t.Fatalf("Normalize(%q) = %q but Normalize(%q) = %q", s, once, once, twice)
		}
	})
}
