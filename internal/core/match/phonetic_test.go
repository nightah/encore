package match

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

// TestFrenchSoundAlikeNeedsLead pins that French titles which only sound like the query play on a clear lead and ask on a near tie: the French encoder merges silent endings, so a near tie between sound-alikes (son, sonde for "sont") is a guess.
func TestFrenchSoundAlikeNeedsLead(t *testing.T) {
	for _, tc := range []struct {
		heard  string
		titles []string
		want   Decision
	}{
		{"sont", []string{"Son", "Sonde"}, DecisionAsk},
		{"sauvegardes", []string{"Sauvegarde", "Sauvegardez"}, DecisionAsk},
		{"paquets", []string{"Paquet", "Paquebot"}, DecisionPlay},
		{"les visiteur", []string{"Les Visiteurs", "Les Visiteurs du soir"}, DecisionPlay},
	} {
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

		assert.Equal(t, tc.want, res.Decision, "%q: %s", tc.heard, describe(res))

		if tc.want == DecisionPlay {
			best, _ := res.Best()
			assert.Equal(t, "0", best.Doc.ID, tc.heard)
		}
	}
}
