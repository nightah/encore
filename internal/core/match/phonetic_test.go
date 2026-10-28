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

// TestGermanFinalER pins that German keys a final "-er" as the "-a" it is spoken as, so "lieda" meets "Lieder", while the plain Kölner Phonetik encoder, which keeps library parity, tells them apart.
func TestGermanFinalER(t *testing.T) {
	keys := languages["de"].phonetic
	plain := func(tok string) []string { return de.Encoder{}.Keys(nil, tok) }

	for _, tc := range []struct{ heard, title string }{
		{"lieda", "lieder"},
		{"rolla", "roller"},
		{"somma", "sommer"},
		{"pfefferkorna", "pfefferkorner"},
		{"gabalia", "gabalier"},
		{"werna", "werner"},
	} {
		assert.Equal(t, keys(tc.title), keys(tc.heard), "%q and %q", tc.heard, tc.title)
		assert.NotEqual(t, plain(tc.title), plain(tc.heard), "plain encoder: %q and %q", tc.heard, tc.title)
	}

	// Short words, other endings and digits keep their Kölner Phonetik key.
	for _, tok := range []string{"der", "wer", "er", "lied", "liedern", "uhr", "tor", "1984"} {
		assert.Equal(t, plain(tok), keys(tok), tok)
	}

	titles := []string{"Lieder", "Es war Sommer", "Bilder im Kopf", "Roller", "Liebe", "Sommerhaus"}
	docs := make([]Doc, len(titles))

	for i, title := range titles {
		docs[i] = Doc{ID: title, Kind: media.KindSong, Ref: media.Ref{Kind: media.KindSong, ID: i + 1}, Title: title}
	}

	ix, err := New("de", Config{})
	require.NoError(t, err)
	require.NoError(t, ix.Upsert(docs...))

	t.Cleanup(func() { require.NoError(t, ix.Close()) })

	for _, tc := range []struct{ heard, want string }{
		{"lieda", "Lieder"},
		{"rolla", "Roller"},
		{"es war somma", "Es war Sommer"},
		{"bilda im kopf", "Bilder im Kopf"},
	} {
		res, err := ix.Search(t.Context(), Query{Text: tc.heard})
		require.NoError(t, err)

		best, _ := res.Best()
		assert.Equal(t, DecisionPlay, res.Decision, "%q: %s", tc.heard, describe(res))
		assert.Equal(t, tc.want, best.Doc.ID, tc.heard)
	}
}
