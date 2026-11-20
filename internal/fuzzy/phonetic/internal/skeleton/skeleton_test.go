package skeleton

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func key(codes string) *Key {
	var k Key

	for i := range len(codes) {
		if codes[i] == '_' {
			k.Vowel()
		} else {
			k.Emit(codes[i])
		}
	}

	return &k
}

func TestKey(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldWriteLeadingVowelAsA", have: "_", expected: "A"},
		{name: "ShouldOnlySeparateWithLaterVowels", have: "_P_T", expected: "APT"},
		{name: "ShouldCollapseRepeats", have: "PPT", expected: "PT"},
		{name: "ShouldKeepRepeatsApartAcrossVowel", have: "P_P", expected: "PP"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, string(key(tc.have).Bytes()))
		})
	}

	assert.Len(t, key(strings.Repeat("PT", MaxLen)).Bytes(), MaxLen, "keys stop at MaxLen")

	var k Key

	k.Emit('K', 'S')
	assert.Equal(t, "KS", string(k.Bytes()))
}

func TestAppend(t *testing.T) {
	testCases := []struct {
		name     string
		dst      []string
		primary  string
		full     string
		expected []string
	}{
		{name: "ShouldFallBackToWordWithoutKey", expected: []string{"w"}},
		{name: "ShouldAppendFullKeyAlone", full: "PT", expected: []string{"PT"}},
		{name: "ShouldAppendShortKeyAlone", primary: "P", expected: []string{"P"}},
		{name: "ShouldNotRepeatEqualFullKey", primary: "P", full: "P", expected: []string{"P"}},
		{name: "ShouldAppendBothKeysToDst", dst: []string{"x"}, primary: "P", full: "PT", expected: []string{"x", "P", "PT"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Append(tc.dst, "w", key(tc.primary), key(tc.full)))
		})
	}
}

func TestAt(t *testing.T) {
	testCases := []struct {
		name     string
		have     int
		expected byte
	}{
		{name: "ShouldReturnByteInRange", have: 1, expected: 'b'},
		{name: "ShouldReturnZeroPastEnd", have: 2},
		{name: "ShouldReturnZeroBeforeStart", have: -1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, At("ab", tc.have))
		})
	}
}
