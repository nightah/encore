package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Expected values are RapidFuzz 3.14 results divided by 100.
func TestRatio(t *testing.T) {
	testCases := []struct {
		name     string
		a        string
		b        string
		expected float64
	}{
		{name: "ShouldScoreTrailingPunctuation", a: "this is a test", b: "this is a test!", expected: 0.9655172413793104},
		{name: "ShouldScoreDifferentLastWord", a: "NEW YORK METS", b: "NEW YORK YANKEES", expected: 0.7586206896551724},
		{name: "ShouldScoreMisspelling", a: "lewenstein", b: "levenshtein", expected: 0.8571428571428571},
		{name: "ShouldScoreTwoEmptyStringsAsEqual", expected: 1},
		{name: "ShouldScoreTextAgainstEmptyAsZero", a: "abc"},
		{name: "ShouldScoreEqualStringsAsOne", a: "same", b: "same", expected: 1},
		{name: "ShouldScoreUmlautByRune", a: "brücke", b: "brucke", expected: 0.8333333333333334},
		// Longer than 64 bytes takes the rune DP path.
		{name: "ShouldTakeRuneDPPathPast64Bytes", a: strings.Repeat("ab", 40), b: strings.Repeat("ab", 40) + "c", expected: 0.9937888198757764},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.expected, ratio(tc.a, tc.b), 1e-9)
		})
	}
}

func TestPartialRatio(t *testing.T) {
	testCases := []struct {
		name     string
		a        string
		b        string
		expected float64
	}{
		{name: "ShouldScoreTrailingPunctuationAsSubstring", a: "this is a test", b: "this is a test!", expected: 1},
		{name: "ShouldScoreShorterInsideLonger", a: "YANKEES", b: "NEW YORK YANKEES", expected: 1},
		{name: "ShouldScoreLongerAroundShorter", a: "NEW YORK YANKEES", b: "YANKEES", expected: 1},
		{name: "ShouldScoreSubstringAfterOtherWord", a: "fuzzy was a bear", b: "wuzzy fuzzy was a bear", expected: 1},
		{name: "ShouldScoreBestWindow", a: "abcd", b: "xbcdy", expected: 0.75},
		{name: "ShouldScoreTwoEmptyStringsAsEqual", expected: 1},
		{name: "ShouldScoreEmptyAgainstTextAsZero", b: "abc"},
		{name: "ShouldScoreDisjointStringsAsZero", a: "xyz", b: "abc"},
		{name: "ShouldScoreTypoInsideLongerTitle", a: "fellowship of the rung", b: "lord of the rings the fellowship of the ring", expected: 0.9545454545454546},
		{name: "ShouldScoreUmlautSubstring", a: "ümlaut", b: "das ümlaut ist", expected: 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.expected, partialRatio(tc.a, tc.b), 1e-9)
		})
	}
}

func TestTokenSetRatio(t *testing.T) {
	testCases := []struct {
		name     string
		a        string
		b        string
		expected float64
	}{
		{name: "ShouldIgnoreRepeatedWord", a: "fuzzy was a bear", b: "fuzzy fuzzy was a bear", expected: 1},
		{name: "ShouldIgnoreWordOrder", a: "new york mets vs atlanta braves", b: "atlanta braves vs new york mets", expected: 1},
		{name: "ShouldScoreSharedWordsInLongerTitle", a: "mariners vs angels", b: "los angeles angels of anaheim at seattle mariners", expected: 0.9090909090909091},
		{name: "ShouldScoreDisjointWords", a: "abc xyz", b: "abd uvw", expected: 0.42857142857142855},
		{name: "ShouldScoreDifferentSequelNumber", a: "rocky 3", b: "rocky 2", expected: 0.8571428571428571},
		{name: "ShouldScoreDifferentNumberAndWord", a: "dune 2 alpha", b: "dune 3 beta", expected: 0.6086956521739131},
		{name: "ShouldScoreEmptyAgainstTextAsZero", b: "abc"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.expected, tokenSetRatio(strings.Fields(tc.a), strings.Fields(tc.b)), 1e-9)
		})
	}
}

func TestTokenSetRatioDoesNotModifyInput(t *testing.T) {
	toks := []string{"a", "a", "b"}
	tokenSetRatio(toks, []string{"b"})
	assert.Equal(t, []string{"a", "a", "b"}, toks)
}

func TestLCSPathsAgree(t *testing.T) {
	testCases := []struct {
		name string
		a    string
		b    string
	}{
		{name: "ShouldAgreeOnTypo", a: "seinfeld", b: "seinfield"},
		{name: "ShouldAgreeOnSharedWords", a: "the lord of the rings", b: "lord of the rings fellowship"},
		{name: "ShouldAgreeOnEmpty", b: "abc"},
		{name: "ShouldAgreePast64Bytes", a: strings.Repeat("x", 64), b: strings.Repeat("x", 70)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, lcsRunes([]rune(tc.a), []rune(tc.b)), lcsLen(tc.a, tc.b))
		})
	}
}
