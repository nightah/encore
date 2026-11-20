package tinyseg

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// referenceFile pins the original JavaScript TinySegmenter 0.2's output: one line per input, the input and its words joined by "|", tab-separated.
const referenceFile = "testdata/reference.tsv"

func TestSegmentMatchesReference(t *testing.T) {
	raw, err := os.ReadFile(referenceFile)
	require.NoError(t, err)

	n := 0

	for line := range strings.Lines(string(raw)) {
		in, want, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		require.True(t, ok, line)
		assert.Equal(t, strings.Split(want, "|"), Segment(in), in)

		n++
	}

	assert.Greater(t, n, 100)
}

func TestSegment(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldReturnNothingForEmpty"},
		{name: "ShouldKeepSingleKanji", have: "猫", expected: []string{"猫"}},
		{name: "ShouldSplitSentence", have: "私の名前は中野です", expected: []string{"私", "の", "名前", "は", "中野", "です"}},
		{name: "ShouldSplitNumeralPrefixAndCounter", have: "第二部", expected: []string{"第", "二", "部"}},
		{name: "ShouldKeepLatinWord", have: "abc", expected: []string{"abc"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Segment(tc.have))
		})
	}

	long := strings.Repeat("東京都に住んでいます", 10)
	assert.Equal(t, long, strings.Join(Segment(long), ""), "past the stack arrays")
	assert.Equal(t, []string{"x", "猫"}, Append([]string{"x"}, "猫"), "Append appends to dst")
}

func TestCharType(t *testing.T) {
	testCases := []struct {
		name     string
		have     rune
		expected byte
	}{
		{name: "ShouldTypeKanjiThreeAsNumeral", have: '三', expected: 'M'},
		{name: "ShouldTypeKanjiTenThousandAsNumeral", have: '万', expected: 'M'},
		{name: "ShouldTypeKanjiAsHan", have: '猫', expected: 'H'},
		{name: "ShouldTypeIterationMarkAsHan", have: '々', expected: 'H'},
		{name: "ShouldTypeSmallKeAsHan", have: 'ヶ', expected: 'H'},
		{name: "ShouldTypeHiragana", have: 'の', expected: 'I'},
		{name: "ShouldTypeKatakana", have: 'ア', expected: 'K'},
		{name: "ShouldTypeLongVowelMarkAsKatakana", have: 'ー', expected: 'K'},
		{name: "ShouldTypeHalfWidthKatakana", have: 'ｱ', expected: 'K'},
		{name: "ShouldTypeLowerLatin", have: 'a', expected: 'A'},
		{name: "ShouldTypeUpperLatin", have: 'Z', expected: 'A'},
		{name: "ShouldTypeFullWidthLatin", have: 'ａ', expected: 'A'},
		{name: "ShouldTypeDigit", have: '7', expected: 'N'},
		{name: "ShouldTypeFullWidthDigit", have: '７', expected: 'N'},
		{name: "ShouldTypeIdeographicCommaAsOther", have: '、', expected: 'O'},
		{name: "ShouldTypeSpaceAsOther", have: ' ', expected: 'O'},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, string(tc.expected), string(charType(tc.have)))
		})
	}
}

func TestKeyChars(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []rune
	}{
		{name: "ShouldTagBoundaryBeforeCharacter", have: "B1あ", expected: []rune{tagB1, 'あ'}},
		{name: "ShouldTagConsecutiveBoundaries", have: "B3B2E1", expected: []rune{tagB3, tagB2, tagE1}},
		{name: "ShouldKeepLoneBAsCharacter", have: "BxE3", expected: []rune{'B', 'x', tagE3}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, keyChars(tc.have))
		})
	}
}

func TestPropSegmentRejoins(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.OneOf(rapid.String(), rapid.StringMatching(`[ぁ-んァ-ヴー一-龠0-9a-z]{0,80}`)).Draw(t, "s")
		words := Segment(s)

		require.Equal(t, s, strings.Join(words, ""))

		for _, w := range words {
			require.NotEmpty(t, w)
		}
	})
}

func TestSegmentAllocations(t *testing.T) {
	dst := make([]string, 0, 16)
	got := testing.AllocsPerRun(100, func() { dst = Append(dst[:0], "千と千尋の神隠し") })
	assert.Zero(t, got, "Append with room in dst allocates nothing")
}

func BenchmarkSegment(b *testing.B) {
	dst := make([]string, 0, 16)
	words := []string{"千と千尋の神隠し", "ハリー・ポッターと賢者の石", "逃げるは恥だが役に立つ", "リビングで千と千尋の神隠しを再生して"}

	b.ReportAllocs()

	for i := range b.N {
		dst = Append(dst[:0], words[i%len(words)])
	}
}
