package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativeAliases checks that a Latin title is found by a request written in the language's script as a recogniser writes it, and that the request in Latin letters still finds it.
func TestNativeAliases(t *testing.T) {
	for _, tc := range []struct {
		lang  string
		docs  []Doc
		heard map[string]string
	}{
		{"ja", []Doc{
			{ID: "superfly", Kind: "artist", Title: "Superfly"},
			{ID: "glay", Kind: "artist", Title: "GLAY"},
			{ID: "beatles", Kind: "artist", Title: "The Beatles"},
			{ID: "starwars", Kind: "movie", Title: "Star Wars"},
			{ID: "ghost", Kind: "movie", Title: "GHOST IN THE SHELL / 攻殻機動隊"},
			{ID: "akb", Kind: "artist", Title: "AKB48"},
			{ID: "vivant", Kind: "tvshow", Title: "VIVANT"},
		}, map[string]string{
			"スーパーフライ":  "superfly",
			"グレー":      "glay",
			"ビートルズ":    "beatles",
			"スターウォーズ":  "starwars",
			"エーケービー48": "akb",
			"superfly": "superfly",
		}},
		{"hi", []Doc{
			{ID: "frozen", Kind: "movie", Title: "Frozen"},
			{ID: "mrindia", Kind: "movie", Title: "Mr. India"},
			{ID: "deewaar", Kind: "movie", Title: "Deewaar"},
			{ID: "zanjeer", Kind: "movie", Title: "Zanjeer"},
			{ID: "kgf", Kind: "movie", Title: "K.G.F: Chapter 1"},
		}, map[string]string{
			"फ्रोजन":          "frozen",
			"मिस्टर इंडिया":   "mrindia",
			"दीवार":           "deewaar",
			"जंजीर":           "zanjeer",
			"केजीएफ चैप्टर 1": "kgf",
			"frozen": "frozen",
		}},
		{"ar", []Doc{
			{ID: "homealone", Kind: "movie", Title: "Home Alone"},
			{ID: "braveheart", Kind: "movie", Title: "Braveheart"},
			{ID: "avatar", Kind: "movie", Title: "Avatar"},
			{ID: "believer", Kind: "song", Title: "Believer"},
		}, map[string]string{
			"هوم الون":  "homealone",
			"بريف هارت": "braveheart",
			"افتار":     "avatar",
			"بيليفر":    "believer",
			"avatar":    "avatar",
		}},
	} {
		ix, err := New(tc.lang, Config{})
		require.NoError(t, err)
		require.NoError(t, ix.Upsert(tc.docs...))

		for heard, want := range tc.heard {
			res, err := ix.Search(t.Context(), Query{Text: heard})
			require.NoError(t, err)

			best, ok := res.Best()
			require.True(t, ok, "%s %q", tc.lang, heard)
			assert.Equal(t, DecisionPlay, res.Decision, "%s %q", tc.lang, heard)
			assert.Equal(t, want, best.Doc.ID, "%s %q", tc.lang, heard)
		}

		require.NoError(t, ix.Close())
	}
}

// TestNativeAliasesOnlyAdd checks that a native alias never makes Encore decide worse than a title's own names would: a title that wins on one plays only when it scores at least PlayScore and leads by PlayMargin, is asked about only from nativeAskScore, and whatever the title's own names would play still plays.
func TestNativeAliasesOnlyAdd(t *testing.T) {
	ix, err := New("ar", Config{})
	require.NoError(t, err)
	require.NoError(t, ix.Upsert(
		Doc{ID: "avengers", Kind: "movie", Title: "The Avengers"},
		Doc{ID: "endgame", Kind: "movie", Title: "Avengers: Endgame"},
		Doc{ID: "darkknight", Kind: "movie", Title: "The Dark Knight"},
		Doc{ID: "avatar", Kind: "movie", Title: "Avatar"},
	))

	decide := func(heard string) (Decision, string) {
		res, err := ix.Search(t.Context(), Query{Text: heard})
		require.NoError(t, err)

		best, _ := res.Best()

		return res.Decision, best.Doc.ID
	}

	d, id := decide("افنجرز")
	assert.Equal(t, DecisionAsk, d, "two Avengers films a native alias barely separates")
	assert.Equal(t, "avengers", id)

	res, err := ix.Search(t.Context(), Query{Text: "افنجرز"})
	require.NoError(t, err)
	assert.InDelta(t, nativeAskScore, res.AskScore, 1e-9, "the question reports the higher bar a native alias winner is asked about from")

	res, err = ix.Search(t.Context(), Query{Text: "avatar"})
	require.NoError(t, err)
	assert.InDelta(t, ix.cfg.AskScore, res.AskScore, 1e-9, "a title's own name keeps the configured bar")

	d, _ = decide("ذا دارك نايت رايزز")
	assert.Equal(t, DecisionNotFound, d, "an absent sequel close to a native alias is not asked about")

	d, id = decide("avatar")
	assert.Equal(t, DecisionPlay, d, "the title's own name plays")
	assert.Equal(t, "avatar", id)

	d, id = decide("افاتار")
	assert.Equal(t, DecisionPlay, d, "a request written exactly as the native alias plays")
	assert.Equal(t, "avatar", id)

	// Whatever a title's own names would play still plays when a native alias puts it on top by less than PlayMargin.
	hi, err := New("hi", Config{})
	require.NoError(t, err)
	require.NoError(t, hi.Upsert(
		Doc{ID: "ironman", Kind: "movie", Title: "Iron Man"},
		Doc{ID: "ironman2", Kind: "movie", Title: "Iron Man 2"},
		Doc{ID: "ironman3", Kind: "movie", Title: "Iron Man 3"},
	))

	res, err = hi.Search(t.Context(), Query{Text: "आइरन मैन"})
	require.NoError(t, err)

	best, _ := res.Best()
	assert.Equal(t, "ironman", best.Doc.ID)
	assert.NotEqual(t, "Iron Man", best.Breakdown.Alias, "the native alias scores best")
	assert.Less(t, res.Margin, hi.cfg.PlayMargin)
	assert.Equal(t, DecisionPlay, res.Decision, "as the title's own name would play it")
}
