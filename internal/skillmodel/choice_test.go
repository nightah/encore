package skillmodel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
)

// TestShippedChoiceAnswers checks that the ways people answer a "did you mean" question by position or age are in each locale's model word for word: a ChooseMatch sample with {Which} filled by a MATCH_CHOICE value or synonym that resolves to the position meant. An answer no sample and synonym spell out may reach another intent, or fill Which with a value entity resolution can't name.
func TestShippedChoiceAnswers(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected string
	}{
		{name: "ShouldResolveTheOlderOneInEnUS", locale: "en-US", have: "the older one", expected: "older"},
		{name: "ShouldResolveIMeantTheSecondOneInEnUS", locale: "en-US", have: "i meant the second one", expected: "second"},
		{name: "ShouldResolveTheOtherInFrFR", locale: "fr-FR", have: "l'autre", expected: "second"},
		{name: "ShouldResolveIMeanTheOtherInFrFR", locale: "fr-FR", have: "je veux dire l'autre", expected: "second"},
		{name: "ShouldResolveTheOldOneInFrFR", locale: "fr-FR", have: "l'ancien", expected: "older"},
		{name: "ShouldResolveTheOriginalInFrFR", locale: "fr-FR", have: "l'original", expected: "older"},
		{name: "ShouldResolveTheOldestInFrFR", locale: "fr-FR", have: "le plus ancien", expected: "older"},
		{name: "ShouldResolveTheOtherInFrCA", locale: "fr-CA", have: "l'autre", expected: "second"},
		{name: "ShouldResolveIMeantTheOldOneInFrCA", locale: "fr-CA", have: "je voulais dire l'ancienne", expected: "older"},
		{name: "ShouldResolveTheOtherInItIT", locale: "it-IT", have: "l'altro", expected: "second"},
		{name: "ShouldResolveTheLastInItIT", locale: "it-IT", have: "l'ultimo", expected: "last"},
		{name: "ShouldResolveIMeantTheOtherInItIT", locale: "it-IT", have: "intendevo l'altro", expected: "second"},
		{name: "ShouldResolveTheOriginalInItIT", locale: "it-IT", have: "l'originale", expected: "older"},
		{name: "ShouldResolveTheOldestInItIT", locale: "it-IT", have: "il più vecchio", expected: "older"},
		{name: "ShouldResolveTheNewestOneInHiIN", locale: "hi-IN", have: "सबसे नया वाला", expected: "newer"},
		{name: "ShouldResolveTheOldestOneInHiIN", locale: "hi-IN", have: "सबसे पुराना वाला", expected: "older"},
		{name: "ShouldResolveIMeantTheNewestOneInHiIN", locale: "hi-IN", have: "मेरा मतलब सबसे नई वाली था", expected: "newer"},
		{name: "ShouldResolveTheOldOneInHiIN", locale: "hi-IN", have: "पुराना वाला", expected: "older"},
		{name: "ShouldResolveTheOldestOneInJaJP", locale: "ja-JP", have: "一番古い方", expected: "older"},
		{name: "ShouldResolveItIsTheNewestOneInJaJP", locale: "ja-JP", have: "一番新しい方です", expected: "newer"},
		{name: "ShouldResolveTheNewestOneSoInJaJP", locale: "ja-JP", have: "一番新しいので", expected: "newer"},
		{name: "ShouldResolveTheOlderOneInJaJP", locale: "ja-JP", have: "古い方", expected: "older"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Build(src, tc.locale, BuildOptions{})
			require.NoError(t, err)

			join := " "
			if tc.locale == "ja-JP" {
				join = ""
			}

			var samples []string

			for _, in := range m.InteractionModel.LanguageModel.Intents {
				if in.Name == "ChooseMatch" {
					samples = in.Samples

					// An answer said on its own is a sample of the slot a question elicits.
					for _, slot := range in.Slots {
						samples = append(samples, slot.Samples...)
					}
				}
			}

			ids := map[string]string{}

			for _, typ := range m.InteractionModel.LanguageModel.Types {
				if typ.Name == "MATCH_CHOICE" {
					for _, v := range typ.Values {
						for _, word := range append([]string{v.Name.Value}, v.Name.Synonyms...) {
							ids[strings.ToLower(word)] = v.ID
						}
					}
				}
			}

			said := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), join) }

			actual := ""

			for _, s := range samples {
				for word, id := range ids {
					if strings.Contains(s, "{Which}") && said(strings.ReplaceAll(s, "{Which}", word)) == said(tc.have) {
						actual = id
					}
				}
			}

			assert.Equal(t, tc.expected, actual)
		})
	}
}
