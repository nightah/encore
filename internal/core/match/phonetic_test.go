package match

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/fuzzy/phonetic/de"
)

// TestFrenchSoundAlikeNeedsLead pins that French titles which only sound like the query play on a clear lead and ask on a near tie: the French encoder merges silent endings, so a near tie between sound-alikes (son, sonde for "sont") is a guess.
func TestFrenchSoundAlikeNeedsLead(t *testing.T) {
	testCases := []struct {
		name     string
		heard    string
		titles   []string
		expected Decision
	}{
		{name: "ShouldAskBetweenSilentEndingSoundAlikes", heard: "sont", titles: []string{"Son", "Sonde"}, expected: DecisionAsk},
		{name: "ShouldAskBetweenSilentPluralAndImperative", heard: "sauvegardes", titles: []string{"Sauvegarde", "Sauvegardez"}, expected: DecisionAsk},
		{name: "ShouldPlaySoundAlikeWithClearLead", heard: "paquets", titles: []string{"Paquet", "Paquebot"}, expected: DecisionPlay},
		{name: "ShouldPlayTitleWithClearLeadOverLongerTitle", heard: "les visiteur", titles: []string{"Les Visiteurs", "Les Visiteurs du soir"}, expected: DecisionPlay},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			docs := make([]Doc, len(tc.titles))
			for i, title := range tc.titles {
				docs[i] = Doc{ID: fmt.Sprint(i), Kind: media.KindMovie, Ref: media.Ref{Kind: media.KindMovie, ID: i + 1}, Title: title}
			}

			ix, err := New("fr", Config{})
			require.NoError(t, err)
			require.NoError(t, ix.Upsert(docs...))

			res, err := ix.Search(t.Context(), Query{Text: tc.heard})
			require.NoError(t, err)
			require.NoError(t, ix.Close())

			assert.Equal(t, tc.expected, res.Decision, describe(res))

			if tc.expected == DecisionPlay {
				best, _ := res.Best()
				assert.Equal(t, "0", best.Doc.ID)
			}
		})
	}
}

// TestGermanFinalER pins that German keys a final "-er" as the "-a" it is spoken as, so "lieda" meets "Lieder", while the plain Kölner Phonetik encoder, which keeps library parity, tells them apart.
func TestGermanFinalER(t *testing.T) {
	keys := languages["de"].phonetic
	plain := func(tok string) []string { return de.Encoder{}.Keys(nil, tok) }

	t.Run("ShouldKeyFinalErAsA", func(t *testing.T) {
		testCases := []struct {
			name  string
			heard string
			title string
		}{
			{name: "ShouldKeyLiederAsLieda", heard: "lieda", title: "lieder"},
			{name: "ShouldKeyRollerAsRolla", heard: "rolla", title: "roller"},
			{name: "ShouldKeySommerAsSomma", heard: "somma", title: "sommer"},
			{name: "ShouldKeyPfefferkornerAsPfefferkorna", heard: "pfefferkorna", title: "pfefferkorner"},
			{name: "ShouldKeyGabalierAsGabalia", heard: "gabalia", title: "gabalier"},
			{name: "ShouldKeyWernerAsWerna", heard: "werna", title: "werner"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, keys(tc.title), keys(tc.heard))
				assert.NotEqual(t, plain(tc.title), plain(tc.heard), "plain encoder")
			})
		}
	})

	// Short words, other endings and digits keep their Kölner Phonetik key.
	t.Run("ShouldKeepKoelnerPhonetikKey", func(t *testing.T) {
		testCases := []struct {
			name string
			have string
		}{
			{name: "ShouldKeepKeyOfDer", have: "der"},
			{name: "ShouldKeepKeyOfWer", have: "wer"},
			{name: "ShouldKeepKeyOfEr", have: "er"},
			{name: "ShouldKeepKeyOfLied", have: "lied"},
			{name: "ShouldKeepKeyOfLiedern", have: "liedern"},
			{name: "ShouldKeepKeyOfUhr", have: "uhr"},
			{name: "ShouldKeepKeyOfTor", have: "tor"},
			{name: "ShouldKeepKeyOfDigits", have: "1984"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, plain(tc.have), keys(tc.have))
			})
		}
	})

	titles := []string{"Lieder", "Es war Sommer", "Bilder im Kopf", "Roller", "Liebe", "Sommerhaus"}
	docs := make([]Doc, len(titles))

	for i, title := range titles {
		docs[i] = Doc{ID: title, Kind: media.KindSong, Ref: media.Ref{Kind: media.KindSong, ID: i + 1}, Title: title}
	}

	ix, err := New("de", Config{})
	require.NoError(t, err)
	require.NoError(t, ix.Upsert(docs...))

	t.Cleanup(func() { require.NoError(t, ix.Close()) })

	t.Run("ShouldFindTitlesHeardWithFinalA", func(t *testing.T) {
		testCases := []struct {
			name     string
			heard    string
			expected string
		}{
			{name: "ShouldFindLieder", heard: "lieda", expected: "Lieder"},
			{name: "ShouldFindRoller", heard: "rolla", expected: "Roller"},
			{name: "ShouldFindEsWarSommer", heard: "es war somma", expected: "Es war Sommer"},
			{name: "ShouldFindBilderImKopf", heard: "bilda im kopf", expected: "Bilder im Kopf"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				res, err := ix.Search(t.Context(), Query{Text: tc.heard})
				require.NoError(t, err)

				best, _ := res.Best()
				assert.Equal(t, DecisionPlay, res.Decision, describe(res))
				assert.Equal(t, tc.expected, best.Doc.ID)
			})
		}
	})
}
