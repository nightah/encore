package skillmodel

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
)

// TestDutchPowerTermsFollowKodi checks that "slaapstand" suspends and "sluimerstand" hibernates, as in Kodi's own Dutch strings, in both the slot values and the responses.
func TestDutchPowerTermsFollowKodi(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	types, err := src.Types("nl-NL")
	require.NoError(t, err)

	words := map[string][]string{}
	for _, v := range types["POWER_ACTION"] {
		words[v.ID] = append([]string{v.Value}, v.Synonyms...)
	}

	assert.Equal(t, "sluimerstand", words["hibernate"][0])
	assert.NotContains(t, words["hibernate"], "slaapstand")
	assert.Equal(t, "slaapstand", words["suspend"][0])
	assert.NotContains(t, words["suspend"], "sluimerstand")

	r, err := src.Responses("nl-NL")
	require.NoError(t, err)

	for _, id := range []string{"power.confirm_hibernate", "power.hibernate"} {
		assert.Contains(t, r[id], "sluimerstand", id)
	}

	for _, id := range []string{"power.confirm_suspend", "power.suspend"} {
		assert.Contains(t, r[id], "slaapstand", id)
	}
}

// TestArabicResponsesAvoidGenderedWatched checks that no Arabic response uses "شاهدت", which inflects for the listener's gender and also reads as "I watched".
func TestArabicResponsesAvoidGenderedWatched(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	for _, locale := range src.LocaleNames() {
		if src.Locales[locale].Language != "ar" {
			continue
		}

		r, err := src.Responses(locale)
		require.NoError(t, err)

		for id, text := range r {
			assert.False(t, slices.Contains(strings.Fields(text), "شاهدت"), "%s %s: %q", locale, id, text)
		}
	}
}

// TestLatinAmericanSpanishUsesPreterite checks that es-MX and es-US say "ya viste" where Spain says "ya has visto".
func TestLatinAmericanSpanishUsesPreterite(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	want := map[string]string{
		"movie.collection_watched": "Ya viste todas las películas de {collection}.",
		"playback.all_watched":     "Ya viste todos los episodios de {show}.",
	}

	for _, locale := range []string{"es-MX", "es-US"} {
		r, err := src.Responses(locale)
		require.NoError(t, err)

		for id, text := range want {
			assert.Equal(t, text, r[id], "%s %s", locale, id)
		}
	}
}
