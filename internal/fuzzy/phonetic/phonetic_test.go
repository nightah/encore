package phonetic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type fixed string

func (f fixed) Keys(dst []string, _ string) []string { return append(dst, string(f)) }

func TestRegistry(t *testing.T) {
	saved := registry
	registry = map[string]Encoder{}

	t.Cleanup(func() { registry = saved })

	assert.Nil(t, For("fr"), "nothing registered and no English fallback")

	Register("en", fixed("EN"))
	Register("fr", fixed("FR"))

	testCases := []struct {
		name     string
		have     string
		expected Encoder
	}{
		{name: "ShouldFindLanguage", have: "fr", expected: fixed("FR")},
		{name: "ShouldFindLanguageForRegion", have: "fr-FR", expected: fixed("FR")},
		{name: "ShouldFindLanguageIgnoringCaseAndUnderscore", have: "FR_ca", expected: fixed("FR")},
		{name: "ShouldFindEnglishForRegion", have: "en-AU", expected: fixed("EN")},
		{name: "ShouldFallBackToEnglishForUnregisteredLanguage", have: "de", expected: fixed("EN")},
		{name: "ShouldFallBackToEnglishForEmpty", expected: fixed("EN")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, For(tc.have))
		})
	}

	assert.PanicsWithValue(t, `phonetic: encoder for "fr" registered twice`, func() { Register("fr", fixed("again")) })
}

func TestIsDigits(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected bool
	}{
		{name: "ShouldRejectEmpty"},
		{name: "ShouldAcceptZero", have: "0", expected: true},
		{name: "ShouldAcceptNumber", have: "2049", expected: true},
		{name: "ShouldRejectTrailingLetter", have: "20a"},
		{name: "ShouldRejectSuperscriptDigit", have: "²"},
		{name: "ShouldRejectSign", have: "-1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsDigits(tc.have))
		})
	}
}
