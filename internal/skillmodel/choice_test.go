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

	for locale, answers := range map[string]map[string]string{
		"en-US": {"the older one": "older", "i meant the second one": "second"},
		"fr-FR": {"l'autre": "second", "je veux dire l'autre": "second", "l'ancien": "older", "l'original": "older", "le plus ancien": "older"},
		"fr-CA": {"l'autre": "second", "je voulais dire l'ancienne": "older"},
		"it-IT": {"l'altro": "second", "l'ultimo": "last", "intendevo l'altro": "second", "l'originale": "older", "il più vecchio": "older"},
		"hi-IN": {"सबसे नया वाला": "newer", "सबसे पुराना वाला": "older", "मेरा मतलब सबसे नई वाली था": "newer", "पुराना वाला": "older"},
		"ja-JP": {"一番古い方": "older", "一番新しい方です": "newer", "一番新しいので": "newer", "古い方": "older"},
	} {
		m, err := Build(src, locale, BuildOptions{})
		require.NoError(t, err, locale)

		join := " "
		if locale == "ja-JP" {
			join = ""
		}

		var samples []string

		for _, in := range m.InteractionModel.LanguageModel.Intents {
			if in.Name == "ChooseMatch" {
				samples = in.Samples
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

		for answer, want := range answers {
			got := ""

			for _, s := range samples {
				for word, id := range ids {
					if strings.Contains(s, "{Which}") && said(strings.ReplaceAll(s, "{Which}", word)) == said(answer) {
						got = id
					}
				}
			}

			assert.Equal(t, want, got, "%s %q", locale, answer)
		}
	}
}
