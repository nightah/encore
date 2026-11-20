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

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldConfirmHibernateWithSluimerstand", have: "power.confirm_hibernate", expected: "sluimerstand"},
		{name: "ShouldHibernateWithSluimerstand", have: "power.hibernate", expected: "sluimerstand"},
		{name: "ShouldConfirmSuspendWithSlaapstand", have: "power.confirm_suspend", expected: "slaapstand"},
		{name: "ShouldSuspendWithSlaapstand", have: "power.suspend", expected: "slaapstand"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, r[tc.have], tc.expected)
		})
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

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected string
	}{
		{name: "ShouldSayCollectionWatchedInEsMX", locale: "es-MX", have: "movie.collection_watched", expected: "Ya viste todas las películas de {collection}."},
		{name: "ShouldSayAllWatchedInEsMX", locale: "es-MX", have: "playback.all_watched", expected: "Ya viste todos los episodios de {show}."},
		{name: "ShouldSayCollectionWatchedInEsUS", locale: "es-US", have: "movie.collection_watched", expected: "Ya viste todas las películas de {collection}."},
		{name: "ShouldSayAllWatchedInEsUS", locale: "es-US", have: "playback.all_watched", expected: "Ya viste todos los episodios de {show}."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := src.Responses(tc.locale)
			require.NoError(t, err)

			assert.Equal(t, tc.expected, r[tc.have])
		})
	}
}
