package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativeAliases checks that a Latin title is found by a request written in the language's script as a recogniser writes it, and that the request in Latin letters still finds it.
func TestNativeAliases(t *testing.T) {
	ja := []Doc{
		{ID: "superfly", Kind: "artist", Title: "Superfly"},
		{ID: "glay", Kind: "artist", Title: "GLAY"},
		{ID: "beatles", Kind: "artist", Title: "The Beatles"},
		{ID: "starwars", Kind: "movie", Title: "Star Wars"},
		{ID: "ghost", Kind: "movie", Title: "GHOST IN THE SHELL / 攻殻機動隊"},
		{ID: "akb", Kind: "artist", Title: "AKB48"},
		{ID: "vivant", Kind: "tvshow", Title: "VIVANT"},
	}
	hi := []Doc{
		{ID: "frozen", Kind: "movie", Title: "Frozen"},
		{ID: "mrindia", Kind: "movie", Title: "Mr. India"},
		{ID: "deewaar", Kind: "movie", Title: "Deewaar"},
		{ID: "zanjeer", Kind: "movie", Title: "Zanjeer"},
		{ID: "kgf", Kind: "movie", Title: "K.G.F: Chapter 1"},
	}
	ar := []Doc{
		{ID: "homealone", Kind: "movie", Title: "Home Alone"},
		{ID: "braveheart", Kind: "movie", Title: "Braveheart"},
		{ID: "avatar", Kind: "movie", Title: "Avatar"},
		{ID: "believer", Kind: "song", Title: "Believer"},
	}

	testCases := []struct {
		name     string
		lang     string
		docs     []Doc
		heard    string
		expected string
	}{
		{name: "ShouldFindSuperflyInKatakana", lang: "ja", docs: ja, heard: "スーパーフライ", expected: "superfly"},
		{name: "ShouldFindGLAYInKatakana", lang: "ja", docs: ja, heard: "グレー", expected: "glay"},
		{name: "ShouldFindTheBeatlesWithoutArticleInKatakana", lang: "ja", docs: ja, heard: "ビートルズ", expected: "beatles"},
		{name: "ShouldFindStarWarsInKatakana", lang: "ja", docs: ja, heard: "スターウォーズ", expected: "starwars"},
		{name: "ShouldFindAKB48InKatakana", lang: "ja", docs: ja, heard: "エーケービー48", expected: "akb"},
		{name: "ShouldFindSuperflyInLatin", lang: "ja", docs: ja, heard: "superfly", expected: "superfly"},
		{name: "ShouldFindFrozenInDevanagari", lang: "hi", docs: hi, heard: "फ्रोजन", expected: "frozen"},
		{name: "ShouldFindMrIndiaInDevanagari", lang: "hi", docs: hi, heard: "मिस्टर इंडिया", expected: "mrindia"},
		{name: "ShouldFindDeewaarInDevanagari", lang: "hi", docs: hi, heard: "दीवार", expected: "deewaar"},
		{name: "ShouldFindZanjeerInDevanagari", lang: "hi", docs: hi, heard: "जंजीर", expected: "zanjeer"},
		{name: "ShouldFindKGFChapter1InDevanagari", lang: "hi", docs: hi, heard: "केजीएफ चैप्टर 1", expected: "kgf"},
		{name: "ShouldFindFrozenInLatin", lang: "hi", docs: hi, heard: "frozen", expected: "frozen"},
		{name: "ShouldFindHomeAloneInArabic", lang: "ar", docs: ar, heard: "هوم الون", expected: "homealone"},
		{name: "ShouldFindBraveheartSplitInArabic", lang: "ar", docs: ar, heard: "بريف هارت", expected: "braveheart"},
		{name: "ShouldFindAvatarInArabic", lang: "ar", docs: ar, heard: "افتار", expected: "avatar"},
		{name: "ShouldFindBelieverInArabic", lang: "ar", docs: ar, heard: "بيليفر", expected: "believer"},
		{name: "ShouldFindAvatarInLatin", lang: "ar", docs: ar, heard: "avatar", expected: "avatar"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ix, err := New(tc.lang, Config{})
			require.NoError(t, err)
			require.NoError(t, ix.Upsert(tc.docs...))

			res, err := ix.Search(t.Context(), Query{Text: tc.heard})
			require.NoError(t, err)
			require.NoError(t, ix.Close())

			best, ok := res.Best()
			require.True(t, ok)
			assert.Equal(t, DecisionPlay, res.Decision)
			assert.Equal(t, tc.expected, best.Doc.ID)
		})
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

	res, err := ix.Search(t.Context(), Query{Text: "افنجرز"})
	require.NoError(t, err)
	assert.InDelta(t, nativeAskScore, res.AskScore, 1e-9, "the question reports the higher bar a native alias winner is asked about from")

	res, err = ix.Search(t.Context(), Query{Text: "avatar"})
	require.NoError(t, err)
	assert.InDelta(t, ix.cfg.AskScore, res.AskScore, 1e-9, "a title's own name keeps the configured bar")

	testCases := []struct {
		name     string
		heard    string
		expected Decision
		id       string
	}{
		{name: "ShouldAskAboutTwoFilmsNativeAliasBarelySeparates", heard: "افنجرز", expected: DecisionAsk, id: "avengers"},
		{name: "ShouldNotAskAboutAbsentSequelCloseToNativeAlias", heard: "ذا دارك نايت رايزز", expected: DecisionNotFound},
		{name: "ShouldPlayTitlesOwnName", heard: "avatar", expected: DecisionPlay, id: "avatar"},
		{name: "ShouldPlayRequestWrittenExactlyAsNativeAlias", heard: "افاتار", expected: DecisionPlay, id: "avatar"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ix.Search(t.Context(), Query{Text: tc.heard})
			require.NoError(t, err)

			best, _ := res.Best()
			assert.Equal(t, tc.expected, res.Decision)

			if tc.id != "" {
				assert.Equal(t, tc.id, best.Doc.ID)
			}
		})
	}

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

// TestAliasAsWrittenBeatsNativeForm checks that a title's alias written in the language's script counts as a name as written, not a native alias, when one of the title's native forms comes out the same.
func TestAliasAsWrittenBeatsNativeForm(t *testing.T) {
	ix, err := New("hi", Config{})
	require.NoError(t, err)

	e := ix.lang.newEntry(Doc{ID: "frozen", Kind: "movie", Title: "Frozen", Aliases: []string{"फ्रोजन"}})

	var found bool

	for _, a := range e.aliases {
		if a.text == "फ्रोजन" {
			found = true

			assert.False(t, a.native, "the alias as written")
			assert.False(t, a.derived)
		}
	}

	assert.True(t, found, "the alias is kept")
	require.NoError(t, ix.Close())
}
