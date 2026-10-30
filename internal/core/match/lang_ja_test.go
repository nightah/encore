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
	for _, tc := range []struct {
		in, want string
	}{
		// Segmented, kana in romaji, kanji kept, particles as stop words.
		{"千と千尋の神隠し", "1000 to 1000 尋 no 神隠 shi"},
		{"君の名は。", "君 no 名 wa"},
		{"abcの歌", "abc no 歌"},
		{"Official髭男dism", "official 髭男 dism"},
		{"トム&ジェリー", "tomu to jeri"},
		{"8½", "8 半"},
		// Hiragana, katakana, half-width and full-width forms meet.
		{"とうきょう", "tokyo"},
		{"トーキョー", "tokyo"},
		{"ｶﾀｶﾅ", "katakana"},
		{"ＡＢＣ", "abc"},
		// Long vowels, small kana and sokuon.
		{"うっせぇわ", "ussewa"},
		{"うっせーわ", "ussewa"},
		{"ポニョ", "ponyo"},
		{"ラピュタ", "rapyuta"},
		{"がっこう", "gakko"},
		{"マッチ", "matchi"},
		{"ティファニーで朝食を", "tifani de 朝食 o"},
		{"ヴァイオレット", "vaioretto"},
		{"ウィキ", "wiki"},
		{"シェフ", "shefu"},
		{"デュエット", "dyuetto"},
		{"ファイナルファンタジー", "fainarufantaji"},
		{"イェーイ", "yei"},
		// Separators split words, and digits and Latin letters stand apart.
		{"トイ・ストーリー3", "toi sutori 3"},
		{"東京2020", "東京 2020"},
		{"ゴジラ-1.0", "gojira 1 0"},
		// Latin words keep their vowels.
		{"ゴッドファーザー PART II", "goddofaza 2"},
		{"Cool", "cool"},
		// Readings of numbers and letters at the end of a katakana word.
		{"トイストーリーフォー", "toisutori 4"},
		{"ターミネーターツー", "tamineta 2"},
		{"ドラゴンボールゼット", "doragonboru z"},
		{"ワン", "1"},
		{"スワン", "suwan"},
		// Kanji numerals that start a word, positional or with multipliers.
		{"七人の侍", "7 人 no 侍"},
		{"7人の侍", "7 人 no 侍"},
		{"千九百八十四", "1984"},
		{"二〇二三年", "2023 年"},
		{"二万", "20000"},
		{"一億二千万", "120000000"},
		{"統一", "統一"},
		{"一二三四五六七八九〇一二三", "一二三四五六七八九〇一二三"},
		// Parts and 第 with a counter.
		{"第二部", "2"},
		{"第2章", "2"},
		{"シーズン2", "2"},
		{"パート2", "2"},
		{"2部", "2 部"},
		{"第", "第"},
		{"", ""},
		{"ー", ""},
	} {
		assert.Equal(t, tc.want, Normalize("ja-JP", tc.in), tc.in)
	}
}

func TestKanjiValue(t *testing.T) {
	for in, want := range map[string]int{
		"一": 1, "十": 10, "十一": 11, "二十": 20, "百二十": 120, "三千": 3000, "千九百八十四": 1984, "二〇二三": 2023,
		"零": 0, "万": 10_000, "二万五千": 25_000, "一億": 100_000_000, "一億二千万": 120_000_000,
	} {
		got, ok := kanjiValue(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}

	for _, in := range []string{"九九九九九九九九九九九億", "一〇〇〇〇〇〇〇〇〇〇〇億", "九九九九九九九九九九九九九九九九九九九九"} {
		_, ok := kanjiValue(in)
		assert.False(t, ok, "%s does not fit an int", in)
	}

	assert.Equal(t, []string{"九九九九九九九九九九九億"}, kanjiNumbers([]string{"九九九九九九九九九九九億"}), "a numeral too large to read stays a word")
}

func TestRomanizeKana(t *testing.T) {
	for in, want := range map[string]string{
		"は": "wa", "へ": "e", "を": "o", "ツー": "2", "ゼット": "z",
		"きゃ": "kya", "しょ": "sho", "ちゅ": "chu", "じゃ": "ja", "にょ": "nyo", "ゐゑ": "ie",
		"ファ": "fa", "ティ": "ti", "トゥ": "tu", "ウォ": "wo", "イェ": "ye", "クァ": "ka", "ヴァ": "va", "グヮ": "gwa",
		"せぇ": "se", "あぁ": "a", "おぉ": "o",
		"んゃ": "nya", "ぁ": "a", "ゃ": "ya", "ヵ": "ka", "ヶ": "ke",
		"っか": "kka", "っち": "tchi", "っあ": "a", "っん": "n", "っ": "", "ー": "",
		"駆ける": "駆 keru", "子ちゃん": "子 chan", "abc": "abc", "cool": "cool", "髭男": "髭男",
	} {
		assert.Equal(t, want, romanizeKana(in), in)
	}
}

func TestSplitJapanese(t *testing.T) {
	for _, tc := range []struct {
		in, want []string
	}{
		{[]string{"リビングで千と千尋の神隠しを再生"}, []string{"リビング", "で", "1000", "と", "1000", "尋", "の", "神隠", "し", "を", "再生"}},
		{[]string{"official髭男dism"}, []string{"official", "髭男", "dism"}},
		{[]string{"ストーリー3", "abc12"}, []string{"ストーリー", "3", "abc", "12"}},
		{[]string{"うっせぇわ"}, []string{"うっせぇわ"}},
		{[]string{"第三章"}, []string{"第", "3"}},
	} {
		assert.Equal(t, tc.want, splitJapanese(tc.in), tc.in)
	}

	assert.Equal(t, []string{"うっせぇわ", "千九百八十四", "人"}, rejoin([]string{"うっせ", "ぇわ", "千", "九百", "八十四", "人"}))
	assert.Equal(t, []string{"ー"}, rejoin([]string{"ー"}), "nothing before to join")
	assert.Equal(t, []string{"ブラックス", "ワン"}, splitReading("ブラックスワン"))
	assert.Equal(t, []string{"スワン"}, splitReading("スワン"), "too short a word before")
	assert.Equal(t, []string{"あいうえおワン"}, splitReading("あいうえおワン"), "not a katakana word")
	assert.Equal(t, []string{"第", "2", "部", "第", "2"}, ordinals([]string{"第", "2", "部", "部", "第", "2"}))
	assert.Equal(t, []string{"第", "2", "第", "3"}, ordinals([]string{"第", "2", "部", "第", "3", "章"}))
	assert.Equal(t, []string{"2", "部"}, ordinals([]string{"2", "部"}), "a counter without 第 stays")
	assert.Equal(t, []string{"第", "x", "部"}, ordinals([]string{"第", "x", "部"}), "only after a number")
	assert.Equal(t, []string{"第", "2", "部品"}, ordinals([]string{"第", "2", "部品"}), "only a counter of its own")
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
	assert.InDelta(t, 1.0, coverage(&q, &a, true), 1e-9)
	assert.InDelta(t, 0.0, coverage(&q, &a, false), 1e-9, "spaced languages match words only")

	short := ix.lang.newForm([]string{"to", "toi"}, "")
	assert.InDelta(t, 3.0/5, coverage(&short, &a, true), 1e-9, "a word shorter than minUnspacedCover must match a word")

	digits := ix.lang.newForm([]string{"3"}, "")
	other := ix.lang.newForm([]string{"x", "13"}, "")
	assert.InDelta(t, 0.0, coverage(&digits, &other, true), 1e-9, "a number never matches inside another")
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
func TestNormalizeJapaneseIdempotent(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"Москва", "москва"},
		{"Ελλάδα", "ελλαδα"},
		{"दिल चाहता है", "dil chahta hai"},
		{"々अ", "々 a"},
		{"ᛮ̀", "ᛮ̀"},
		{"モスクワМосква", "mosukuwa москва"},
		{"ア́", "a"},
		{"日́本", "日本"},
		{"テ̀スト", "tesuto"},
		{"か゚", "ka"},
		// A kanji word is split as the segmenter splits it alone, and a kanji numeral after kana is read.
		{"いい日旅立ち", "i 日 旅立 chi"},
		{"二十世紀少年", "20 世 紀 少年"},
		{"ぁ零", "a 0"},
	} {
		got := Normalize("ja", tc.in)
		assert.Equal(t, tc.want, got, tc.in)
		assert.Equal(t, got, Normalize("ja", got), "normalising %q again", tc.in)
	}

	for _, in := range []string{"한국 영화", "千と千尋の神隠し", "鬼滅の刃無限列車編"} {
		got := Normalize("ja", in)
		assert.Equal(t, got, Normalize("ja", got), "normalising %q again", in)
	}
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
