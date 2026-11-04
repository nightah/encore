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
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"猫", []string{"猫"}},
		{"私の名前は中野です", []string{"私", "の", "名前", "は", "中野", "です"}},
		{"第二部", []string{"第", "二", "部"}},
		{"abc", []string{"abc"}},
	} {
		assert.Equal(t, tc.want, Segment(tc.in), tc.in)
	}

	long := strings.Repeat("東京都に住んでいます", 10)
	assert.Equal(t, long, strings.Join(Segment(long), ""), "past the stack arrays")
	assert.Equal(t, []string{"x", "猫"}, Append([]string{"x"}, "猫"), "Append appends to dst")
}

func TestCharType(t *testing.T) {
	for r, want := range map[rune]byte{
		'三': 'M', '万': 'M', '猫': 'H', '々': 'H', 'ヶ': 'H', 'の': 'I', 'ア': 'K', 'ー': 'K', 'ｱ': 'K',
		'a': 'A', 'Z': 'A', 'ａ': 'A', '7': 'N', '７': 'N', '、': 'O', ' ': 'O',
	} {
		assert.Equal(t, string(want), string(charType(r)), string(r))
	}
}

func TestKeyChars(t *testing.T) {
	assert.Equal(t, []rune{tagB1, 'あ'}, keyChars("B1あ"))
	assert.Equal(t, []rune{tagB3, tagB2, tagE1}, keyChars("B3B2E1"))
	assert.Equal(t, []rune{'B', 'x', tagE3}, keyChars("BxE3"))
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
