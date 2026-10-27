package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Expected values are RapidFuzz 3.14 results divided by 100.
func TestRatio(t *testing.T) {
	tests := []struct {
		a, b string
		want float64
	}{
		{"this is a test", "this is a test!", 0.9655172413793104},
		{"NEW YORK METS", "NEW YORK YANKEES", 0.7586206896551724},
		{"lewenstein", "levenshtein", 0.8571428571428571},
		{"", "", 1},
		{"abc", "", 0},
		{"same", "same", 1},
		{"brücke", "brucke", 0.8333333333333334},
		// Longer than 64 bytes takes the rune DP path.
		{strings.Repeat("ab", 40), strings.Repeat("ab", 40) + "c", 0.9937888198757764},
	}

	for _, tt := range tests {
		assert.InDelta(t, tt.want, ratio(tt.a, tt.b), 1e-9, "%q vs %q", tt.a, tt.b)
	}
}

func TestPartialRatio(t *testing.T) {
	tests := []struct {
		a, b string
		want float64
	}{
		{"this is a test", "this is a test!", 1},
		{"YANKEES", "NEW YORK YANKEES", 1},
		{"NEW YORK YANKEES", "YANKEES", 1},
		{"fuzzy was a bear", "wuzzy fuzzy was a bear", 1},
		{"abcd", "xbcdy", 0.75},
		{"", "", 1},
		{"", "abc", 0},
		{"xyz", "abc", 0},
		{"fellowship of the rung", "lord of the rings the fellowship of the ring", 0.9545454545454546},
		{"ümlaut", "das ümlaut ist", 1},
	}

	for _, tt := range tests {
		assert.InDelta(t, tt.want, partialRatio(tt.a, tt.b), 1e-9, "%q vs %q", tt.a, tt.b)
	}
}

func TestTokenSetRatio(t *testing.T) {
	tests := []struct {
		a, b string
		want float64
	}{
		{"fuzzy was a bear", "fuzzy fuzzy was a bear", 1},
		{"new york mets vs atlanta braves", "atlanta braves vs new york mets", 1},
		{"mariners vs angels", "los angeles angels of anaheim at seattle mariners", 0.9090909090909091},
		{"abc xyz", "abd uvw", 0.42857142857142855},
		{"rocky 3", "rocky 2", 0.8571428571428571},
		{"dune 2 alpha", "dune 3 beta", 0.6086956521739131},
		{"", "abc", 0},
	}

	for _, tt := range tests {
		assert.InDelta(t, tt.want, tokenSetRatio(strings.Fields(tt.a), strings.Fields(tt.b)), 1e-9, "%q vs %q", tt.a, tt.b)
	}
}

func TestTokenSetRatioDoesNotModifyInput(t *testing.T) {
	toks := []string{"a", "a", "b"}
	tokenSetRatio(toks, []string{"b"})
	assert.Equal(t, []string{"a", "a", "b"}, toks)
}

func TestLCSPathsAgree(t *testing.T) {
	pairs := [][2]string{
		{"seinfeld", "seinfield"},
		{"the lord of the rings", "lord of the rings fellowship"},
		{"", "abc"},
		{strings.Repeat("x", 64), strings.Repeat("x", 70)},
	}

	for _, p := range pairs {
		assert.Equal(t, lcsRunes([]rune(p[0]), []rune(p[1])), lcsLen(p[0], p[1]), "%q vs %q", p[0], p[1])
	}
}
