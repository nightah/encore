package match

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

func movie(id, title string, year int) Doc {
	return Doc{ID: id, Kind: media.KindMovie, Title: title, Year: year}
}

func newIndex(t *testing.T, retr string, docs ...Doc) *Index {
	t.Helper()

	ix, err := New("en", Config{Retriever: retr})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ix.Close() })
	require.NoError(t, ix.Upsert(docs...))

	return ix
}

func retrievers(t *testing.T, fn func(t *testing.T, retr string)) {
	t.Helper()

	for _, r := range []string{RetrieverInverted, RetrieverTrigram} {
		t.Run(r, func(t *testing.T) { fn(t, r) })
	}
}

func TestIndexLifecycle(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr, movie("a", "Inception", 2010), movie("b", "Interstellar", 2014))
		ctx := t.Context()

		assert.Equal(t, 2, ix.Len())

		res, err := ix.Search(ctx, Query{Text: "inception"})
		require.NoError(t, err)
		assert.Equal(t, DecisionPlay, res.Decision)

		// Replacing a doc drops its old names.
		require.NoError(t, ix.Upsert(movie("a", "Memento", 2000)))
		assert.Equal(t, 2, ix.Len())

		res, err = ix.Search(ctx, Query{Text: "inception"})
		require.NoError(t, err)
		assert.NotEqual(t, DecisionPlay, res.Decision)

		res, err = ix.Search(ctx, Query{Text: "memento"})
		require.NoError(t, err)

		best, ok := res.Best()
		require.True(t, ok)
		assert.Equal(t, "a", best.Doc.ID)

		require.NoError(t, ix.Delete("a", "missing"))
		assert.Equal(t, 1, ix.Len())

		res, err = ix.Search(ctx, Query{Text: "memento"})
		require.NoError(t, err)
		assert.Equal(t, DecisionNotFound, res.Decision)

		require.NoError(t, ix.Close())
		require.NoError(t, ix.Close())

		_, err = ix.Search(ctx, Query{Text: "interstellar"})
		require.ErrorIs(t, err, ErrClosed)
		require.ErrorIs(t, ix.Upsert(movie("c", "Up", 2009)), ErrClosed)
		require.ErrorIs(t, ix.Delete("b"), ErrClosed)
	})
}

func TestIndexRejectsBadInput(t *testing.T) {
	_, err := New("en", Config{Retriever: "nope"})
	require.Error(t, err)

	ix := newIndex(t, RetrieverInverted)
	require.Error(t, ix.Upsert(Doc{Title: "No ID"}))

	res, err := ix.Search(t.Context(), Query{Text: "?!"})
	require.NoError(t, err)
	assert.Equal(t, DecisionNotFound, res.Decision)
	assert.Empty(t, res.Candidates)
}

func TestIndexNormalizesInItsLanguage(t *testing.T) {
	de, err := New("de-AT", Config{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = de.Close() })

	assert.Equal(t, "arzte", de.Normalize("Die Ärzte"), "German drops its article")
	assert.Equal(t, "die arzte", newIndex(t, RetrieverTrigram).Normalize("Die Ärzte"), "English doesn't")
}

func TestUnspeakableTitleFoundByAlias(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr, Doc{ID: "x", Kind: media.KindMovie, Title: "?!", Aliases: []string{"Interrobang"}})

		res, err := ix.Search(t.Context(), Query{Text: "interrobang"})
		require.NoError(t, err)

		best, ok := res.Best()
		require.True(t, ok)
		assert.Equal(t, "x", best.Doc.ID)
		assert.Equal(t, "Interrobang", best.Breakdown.Alias)
	})
}

func TestLanguages(t *testing.T) {
	assert.Equal(t, []string{"ar", "de", "en", "es", "fr", "hi", "it", "ja", "nl", "pt"}, Languages())
}

func TestSearchKindsAndLimit(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr,
			movie("m", "Queen", 2006),
			Doc{ID: "a", Kind: media.KindArtist, Title: "Queen"},
			movie("x", "Queens", 2010),
		)

		res, err := ix.Search(t.Context(), Query{Text: "queen", Kinds: []media.Kind{media.KindArtist}})
		require.NoError(t, err)
		require.Len(t, res.Candidates, 1)
		assert.Equal(t, "a", res.Candidates[0].Doc.ID)
		assert.Equal(t, DecisionPlay, res.Decision)

		res, err = ix.Search(t.Context(), Query{Text: "queen"})
		require.NoError(t, err)
		assert.Equal(t, DecisionAsk, res.Decision, "two kinds share the exact name")

		res, err = ix.Search(t.Context(), Query{Text: "queen", Limit: 1})
		require.NoError(t, err)
		assert.Len(t, res.Candidates, 1)
	})
}

// TestDefaultRetrieverFindsUnsegmentedScripts searches titles in scripts written without spaces with a letter missing. Segmenting them into words by Unicode rules splits them into single characters or drops them, so only an exact title would find them.
func TestDefaultRetrieverFindsUnsegmentedScripts(t *testing.T) {
	ix, err := New("en", Config{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ix.Close() })

	require.NoError(t, ix.Upsert(
		movie("spirited", "千と千尋の神隠し", 2001),
		Doc{ID: "thai", Kind: media.KindSong, Title: "สวัสดีชาวโลก"},
		Doc{ID: "kana", Kind: media.KindSong, Title: "ひらがなとカタカナ"},
		movie("filler", "Inception", 2010),
	))

	testCases := []struct {
		name     string
		heard    string
		expected string
	}{
		{name: "ShouldFindKanjiAndKanaTitleMissingALetter", heard: "千と千尋の神隠", expected: "spirited"},
		{name: "ShouldFindThaiTitleMissingALetter", heard: "สวัสดีชาวโล", expected: "thai"},
		{name: "ShouldFindKanaTitleMissingALetter", heard: "ひらがなとカタカ", expected: "kana"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ix.Search(t.Context(), Query{Text: tc.heard})
			require.NoError(t, err)

			best, _ := res.Best()
			assert.Equal(t, tc.expected, best.Doc.ID)
		})
	}
}

// TestSearchOtherScriptInEnglishLibrary checks that a request in Japanese, Hindi or Arabic meets a Latin title in an English library, the way a ja-JP, hi-IN or ar-SA recogniser writes it.
func TestSearchOtherScriptInEnglishLibrary(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr,
			movie("titanic", "Titanic", 1997),
			movie("dangal", "Dangal", 2016),
			movie("dhoom2", "Dhoom 2", 2006),
			movie("matrix", "The Matrix", 1999),
			movie("spirited", "千と千尋の神隠し", 2001),
			movie("filler", "Inception", 2010),
		)

		testCases := []struct {
			name     string
			heard    string
			expected string
		}{
			{name: "ShouldFindLatinTitleFromKatakana", heard: "タイタニック", expected: "titanic"},
			{name: "ShouldFindLatinTitleFromDevanagari", heard: "दंगल", expected: "dangal"},
			{name: "ShouldFindLatinSequelFromHindiNumberWord", heard: "धूम दो", expected: "dhoom2"},
			{name: "ShouldFindLatinTitleFromArabicWithArticle", heard: "ذا ماتريكس", expected: "matrix"},
			{name: "ShouldFindJapaneseTitleAsWritten", heard: "千と千尋の神隠し", expected: "spirited"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				res, err := ix.Search(t.Context(), Query{Text: tc.heard})
				require.NoError(t, err)

				best, _ := res.Best()
				assert.Equal(t, DecisionPlay, res.Decision)
				assert.Equal(t, tc.expected, best.Doc.ID)
			})
		}
	})
}

// TestSearchResolvedCandidates checks that a Doc Alexa's entity resolution matched is scored even when retrieval misses it, against what was heard and against the value name Alexa matched, and that Encore's scorer still decides.
func TestSearchResolvedCandidates(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr,
			movie("spirited", "千と千尋の神隠し", 2001),
			movie("totoro", "となりのトトロ", 1988),
			movie("titanic", "Titanic", 1997),
			movie("toystory", "Toy Story", 1995),
			Doc{ID: "pinkfloyd", Kind: media.KindArtist, Title: "Pink Floyd"},
			Doc{ID: "pink", Kind: media.KindArtist, Title: "P!nk"},
		)

		search := func(t *testing.T, q Query) (Result, Candidate) {
			t.Helper()

			res, err := ix.Search(t.Context(), q)
			require.NoError(t, err)

			best, _ := res.Best()

			return res, best
		}

		// Hiragana for a kanji title: retrieval finds nothing, and the value Alexa matched names the title.
		heard := "せんとちひろのかみかくし"
		res, _ := search(t, Query{Text: heard})
		require.Equal(t, DecisionNotFound, res.Decision, "without resolution")

		res, best := search(t, Query{Text: heard, Resolved: []Resolved{{ID: "spirited", Name: "千と千尋の神隠し"}}})
		assert.Equal(t, DecisionPlay, res.Decision)
		assert.Equal(t, "spirited", best.Doc.ID)
		assert.Less(t, best.Score, 1.0, "a resolved name is never an exact match")

		// An ID whose value names another title (a stale model) does not play it.
		res, _ = search(t, Query{Text: heard, Resolved: []Resolved{{ID: "totoro", Name: "千と千尋の神隠し"}}})
		assert.NotEqual(t, DecisionPlay, res.Decision)

		// Without a name, the resolved Doc is scored against what was heard alone.
		res, best = search(t, Query{Text: "titanik", Resolved: []Resolved{{ID: "titanic"}}})
		assert.Equal(t, "titanic", best.Doc.ID)
		assert.Equal(t, DecisionPlay, res.Decision)

		res, _ = search(t, Query{Text: heard, Resolved: []Resolved{{ID: "spirited"}}})
		assert.Equal(t, DecisionNotFound, res.Decision)

		// Two values for one request ask which was meant.
		res, _ = search(t, Query{Text: "pink", Kinds: []media.Kind{media.KindArtist}, Resolved: []Resolved{{ID: "pinkfloyd", Name: "Pink Floyd"}, {ID: "pink", Name: "P!nk"}}})
		assert.Equal(t, DecisionAsk, res.Decision)

		// What was heard still wins when it names another title exactly.
		res, best = search(t, Query{Text: "titanic", Resolved: []Resolved{{ID: "toystory", Name: "Toy Story"}}})
		assert.Equal(t, DecisionPlay, res.Decision)
		assert.Equal(t, "titanic", best.Doc.ID)

		// A number the title lacks is still a missing sequel.
		res, _ = search(t, Query{Text: "toy story 4", Resolved: []Resolved{{ID: "toystory", Name: "Toy Story"}}})
		assert.Equal(t, DecisionAsk, res.Decision)

		// Kinds still restrict, and unknown IDs are ignored.
		res, _ = search(t, Query{Text: heard, Kinds: []media.Kind{media.KindArtist}, Resolved: []Resolved{{ID: "spirited", Name: "千と千尋の神隠し"}}})
		assert.Equal(t, DecisionNotFound, res.Decision)

		res, _ = search(t, Query{Text: heard, Resolved: []Resolved{{ID: "missing", Name: "千と千尋の神隠し"}}})
		assert.Equal(t, DecisionNotFound, res.Decision)

		// A name with nothing to match is no name.
		res, _ = search(t, Query{Text: heard, Resolved: []Resolved{{ID: "spirited", Name: "!!!"}}})
		assert.Equal(t, DecisionNotFound, res.Decision)
	})
}

func TestBoostsReorderOnlyNearTies(t *testing.T) {
	ix := newIndex(t, RetrieverInverted,
		movie("old", "The Thing", 1982),
		movie("new", "The Things", 2011),
		movie("far", "Thingamajig", 2011),
	)

	// A hint breaks a near-tie between fuzzy matches...
	res, err := ix.Search(t.Context(), Query{Text: "thinks", Hints: map[string]float64{"new": 1}})
	require.NoError(t, err)

	best, _ := res.Best()
	assert.Equal(t, "new", best.Doc.ID)
	assert.InDelta(t, DefaultConfig().MaxBoost, best.Breakdown.Boost, 1e-9, "hints are capped")

	// ...but can't beat an exact match.
	res, err = ix.Search(t.Context(), Query{Text: "the thing", Hints: map[string]float64{"far": 1}})
	require.NoError(t, err)

	best, _ = res.Best()
	assert.Equal(t, "old", best.Doc.ID)
	assert.Equal(t, DecisionPlay, res.Decision)
}

func TestSearchYear(t *testing.T) {
	ix := newIndex(t, RetrieverInverted, movie("d84", "Dune", 1984), movie("d21", "Dune", 2021))

	testCases := []struct {
		name     string
		heard    string
		expected string
	}{
		{name: "ShouldPlayYearInDigits", heard: "dune 1984", expected: "d84"},
		{name: "ShouldPlayYearInWords", heard: "dune twenty twenty one", expected: "d21"},
		{name: "ShouldPlayYearInParentheses", heard: "dune (2021)", expected: "d21"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ix.Search(t.Context(), Query{Text: tc.heard})
			require.NoError(t, err)

			best, _ := res.Best()
			assert.Equal(t, tc.expected, best.Doc.ID)
			assert.Equal(t, DecisionPlay, res.Decision)
		})
	}

	res, err := ix.Search(t.Context(), Query{Text: "dune 1999"})
	require.NoError(t, err)
	assert.Equal(t, DecisionAsk, res.Decision)
}

// TestSearchSpokenArticle checks that an article the request says picks the one title it starts among titles that are otherwise the same name: "the odyssey" is the film The Odyssey, not the album or song Odyssey that a search of music as well (deep search) also finds, and only a request without the article asks between them.
func TestSearchSpokenArticle(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr,
			Doc{ID: "o05", Kind: media.KindAlbum, Title: "Odyssey", Year: 2005},
			Doc{ID: "o24", Kind: media.KindSong, Title: "Odyssey", Year: 2024},
			movie("the", "The Odyssey", 2026),
			Doc{ID: "bonus", Kind: media.KindSong, Title: "Odyssey (Bonus Track)", Year: 2002},
			Doc{ID: "xix", Kind: media.KindSong, Title: "Odyssey XIX", Year: 2012},
			movie("2001", "2001: A Space Odyssey", 1968),
		)

		t.Run("ShouldPlayTitleNamedAsSaid", func(t *testing.T) {
			testCases := []struct {
				name     string
				heard    string
				expected string
			}{
				{name: "ShouldPlayTitleStartingWithSpokenArticle", heard: "the odyssey", expected: "the"},
				{name: "ShouldPlayTitleStartingWithCapitalisedArticle", heard: "The Odyssey", expected: "the"},
				{name: "ShouldPlayTitleWithArticleAndYear", heard: "the odyssey 2026", expected: "the"},
				{name: "ShouldPlayTitleWithArticleAndFromYear", heard: "the odyssey from 2026", expected: "the"},
				{name: "ShouldPlayTitleByYearWithoutArticle", heard: "odyssey 2026", expected: "the"},
				{name: "ShouldPlaySongByYear", heard: "odyssey 2024", expected: "o24"},
				{name: "ShouldPlayAlbumByFromYear", heard: "odyssey from 2005", expected: "o05"},
				{name: "ShouldPlayAlbumByYearDespiteArticle", heard: "the odyssey 2005", expected: "o05"},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					res, err := ix.Search(t.Context(), Query{Text: tc.heard})
					require.NoError(t, err)

					best, _ := res.Best()
					assert.Equal(t, DecisionPlay, res.Decision)
					assert.Equal(t, tc.expected, best.Doc.ID)
				})
			}
		})

		// Without an article nothing is named as said, since the recogniser may have missed one: all three exact titles are asked about.
		res, err := ix.Search(t.Context(), Query{Text: "odyssey"})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Candidates), 3)
		assert.Equal(t, DecisionAsk, res.Decision)
		assert.ElementsMatch(t, []string{"o05", "o24", "the"}, []string{res.Candidates[0].Doc.ID, res.Candidates[1].Doc.ID, res.Candidates[2].Doc.ID})

		// Articles are dropped so a request finds a title whether or not either says one, and that still holds: only a tie between exact titles is settled by the article.
		t.Run("ShouldDropArticles", func(t *testing.T) {
			testCases := []struct {
				name     string
				heard    string
				docs     []Doc
				expected string
				decision Decision
			}{
				{name: "ShouldPlayTitleWithDroppedArticle", heard: "matrix", docs: []Doc{movie("m", "The Matrix", 1999), movie("r", "The Matrix Reloaded", 2003)}, expected: "m", decision: DecisionPlay},
				{name: "ShouldPlayTitleWhenRequestSaysArticleItLacks", heard: "the colony", docs: []Doc{{ID: "show", Kind: media.KindTVShow, Title: "Colony", Year: 2016}}, expected: "show", decision: DecisionPlay},
				{name: "ShouldPlayTitleWithDroppedIndefiniteArticle", heard: "a quiet place", docs: []Doc{movie("qp", "Quiet Place", 2018)}, expected: "qp", decision: DecisionPlay},
				{name: "ShouldSettleTieBetweenExactTitlesBySpokenArticle", heard: "the colony", docs: []Doc{{ID: "show", Kind: media.KindTVShow, Title: "Colony", Year: 2016}, movie("film", "The Colony", 2013)}, expected: "film", decision: DecisionPlay},
				{name: "ShouldAskBetweenExactTitlesWithoutSpokenArticle", heard: "colony", docs: []Doc{{ID: "show", Kind: media.KindTVShow, Title: "Colony", Year: 2016}, movie("film", "The Colony", 2013)}, expected: "show", decision: DecisionAsk},
				{name: "ShouldAskWhenArticleNamesNeitherTitle", heard: "the odyssey", docs: []Doc{{ID: "o05", Kind: media.KindAlbum, Title: "Odyssey", Year: 2005}, {ID: "o24", Kind: media.KindSong, Title: "Odyssey", Year: 2024}}, expected: "o05", decision: DecisionAsk},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					res, err := newIndex(t, retr, tc.docs...).Search(t.Context(), Query{Text: tc.heard})
					require.NoError(t, err)

					best, _ := res.Best()
					assert.Equal(t, tc.decision, res.Decision)
					assert.Equal(t, tc.expected, best.Doc.ID)
				})
			}
		})

		// An article that is the whole title stays: "The" is not "A".
		ix = newIndex(t, retr, movie("a", "A", 2001), movie("the1", "The", 2002))
		res, err = ix.Search(t.Context(), Query{Text: "the"})
		require.NoError(t, err)

		best, _ := res.Best()
		assert.Equal(t, "the1", best.Doc.ID)
	})
}

// TestSearchAsksForMissingSequel checks that a number the best title lacks stops a play: with no sibling to compete, a long title must not outscore the number and start the original instead of asking.
func TestSearchAsksForMissingSequel(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr, movie("dundee", "Crocodile Dundee", 1986), movie("babe", "Babe", 1995), movie("bullitt", "Bullitt", 1968))

		t.Run("ShouldAskForNumberTitleLacks", func(t *testing.T) {
			testCases := []struct {
				name     string
				heard    string
				expected Decision
			}{
				{name: "ShouldAskForSequelNumberInWords", heard: "crocodile dundee two", expected: DecisionAsk},
				{name: "ShouldAskForSequelNumberInDigits", heard: "crocodile dundee 3", expected: DecisionAsk},
				{name: "ShouldAskForSequelPart", heard: "crocodile dundee part two", expected: DecisionAsk},
				{name: "ShouldPlayTitleWithoutNumber", heard: "crocodile dundee", expected: DecisionPlay},
				{name: "ShouldPlayTitleWithItsYear", heard: "crocodile dundee 1986", expected: DecisionPlay},
				{name: "ShouldPlayTitleWithOne", heard: "crocodile dundee one", expected: DecisionPlay},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					res, err := ix.Search(t.Context(), Query{Text: tc.heard})
					require.NoError(t, err)

					best, _ := res.Best()
					assert.Equal(t, tc.expected, res.Decision)
					assert.Equal(t, "dundee", best.Doc.ID)
				})
			}
		})

		// A title that spells its number ("uno") still lacks the sequel's: matching as spelled out does not make "uno dos" agree with "uno".
		es, err := New("es", Config{Retriever: retr})
		require.NoError(t, err)
		t.Cleanup(func() { _ = es.Close() })
		require.NoError(t, es.Upsert(movie("padre", "Padre no hay más que uno", 2019), movie("tesis", "Tesis", 1996)))

		t.Run("ShouldAskForSequelOfTitleThatSpellsItsNumber", func(t *testing.T) {
			testCases := []struct {
				name     string
				heard    string
				expected Decision
			}{
				{name: "ShouldAskForSequelNumberInWords", heard: "padre no hay más que uno dos", expected: DecisionAsk},
				{name: "ShouldAskForSequelNumberInDigits", heard: "padre no hay más que uno 2", expected: DecisionAsk},
				{name: "ShouldPlayTitleWithItsSpelledNumber", heard: "padre no hay más que uno", expected: DecisionPlay},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					res, err := es.Search(t.Context(), Query{Text: tc.heard})
					require.NoError(t, err)

					best, _ := res.Best()
					assert.Equal(t, tc.expected, res.Decision)
					assert.Equal(t, "padre", best.Doc.ID)
				})
			}
		})

		// Not missing sequels: a spoken number that sounds like a title word ("four" for "for"), and a query number the title carries among others.
		ix = newIndex(t, retr, Doc{ID: "tff", Kind: media.KindArtist, Title: "Tears for Fears"}, movie("babe", "Babe", 1995))
		require.NoError(t, es.Upsert(movie("padre2", "Padre no hay más que uno 2: La llegada de la suegra", 2020)))

		t.Run("ShouldNotTreatAsMissingSequel", func(t *testing.T) {
			testCases := []struct {
				name     string
				index    *Index
				heard    string
				expected string
			}{
				{name: "ShouldPlayTitleWordThatSoundsLikeNumber", index: ix, heard: "tears four fears", expected: "tff"},
				{name: "ShouldPlayTitleCarryingNumberAmongOthers", index: es, heard: "padre no hay más que uno la llegada de la suegra", expected: "padre2"},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					res, err := tc.index.Search(t.Context(), Query{Text: tc.heard})
					require.NoError(t, err)

					best, _ := res.Best()
					assert.Equal(t, DecisionPlay, res.Decision)
					assert.Equal(t, tc.expected, best.Doc.ID)
				})
			}
		})
	})
}

func TestSearchHonoursContext(t *testing.T) {
	ix := newIndex(t, RetrieverInverted, movie("a", "Inception", 2010))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := ix.Search(ctx, Query{Text: "inception"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestIndexConcurrency(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr, syntheticDocs(2000, 7)...)
		ix2 := newIndex(t, retr)

		var wg sync.WaitGroup

		for w := range 8 {
			wg.Go(func() {
				for i := range 50 {
					if _, err := ix.Search(t.Context(), Query{Text: fmt.Sprintf("starnight %d", i)}); err != nil {
						t.Error(err)
					}

					if _, err := ix2.Search(t.Context(), Query{Text: "title"}); err != nil {
						t.Error(err)
					}
				}
			})

			wg.Go(func() {
				for i := range 20 {
					id := fmt.Sprintf("w%d-%d", w, i)
					assert.NoError(t, ix.Upsert(movie(id, "Concurrent Title "+id, 2000)))
					assert.NoError(t, ix2.Upsert(movie(id, "Title "+id, 2000)))

					if i%2 == 0 {
						assert.NoError(t, ix.Delete(id))
					}
				}
			})
		}

		wg.Wait()
		assert.Equal(t, 2000+8*10, ix.Len())
		assert.Equal(t, 8*20, ix2.Len())
	})
}

// TestRetrieversAgree cross-checks the inverted index against the trigram retriever on the corpus libraries: decisions must match, and so must the top Doc when something was found.
func TestRetrieversAgree(t *testing.T) {
	c := loadCorpus(t)

	for _, suite := range c.Suites {
		b, err := New(suite.Lang, Config{Retriever: RetrieverInverted})
		require.NoError(t, err)
		t.Cleanup(func() { _ = b.Close() })

		g, err := New(suite.Lang, Config{Retriever: RetrieverTrigram})
		require.NoError(t, err)

		require.NoError(t, b.Upsert(suite.docs()...))
		require.NoError(t, g.Upsert(suite.docs()...))

		for _, tc := range suite.Cases {
			rb, err := b.Search(t.Context(), tc.query())
			require.NoError(t, err)

			rg, err := g.Search(t.Context(), tc.query())
			require.NoError(t, err)

			assert.Equal(t, rg.Decision, rb.Decision, tc.Heard)

			if rb.Decision == DecisionNotFound {
				// The leftovers of a miss depend on what each retriever happened to fetch.
				continue
			}

			bb, _ := rb.Best()
			bg, _ := rg.Best()
			assert.Equal(t, bg.Doc.ID, bb.Doc.ID, tc.Heard)
		}
	}
}

// TestDefaultPoolFindsSplitTitlesAtScale searches a 100k-Doc scale library for "mo ana". "mo" and "ana" are common enough there that a pool of 200 left Moana out and asked about a song instead; exhaustive scoring plays it.
func TestDefaultPoolFindsSplitTitlesAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 100k-Doc library")
	}

	suite := englishSuite(t)

	ix, err := New("en", Config{})
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, ix.Close()) })
	require.NoError(t, ix.Upsert(scaleDocs(100_000, suite.docs(), 1)...))

	res, err := ix.Search(t.Context(), Query{Text: "mo ana"})
	require.NoError(t, err)

	best, _ := res.Best()
	assert.Equal(t, DecisionPlay, res.Decision, describe(res))
	assert.Equal(t, "moana", best.Doc.ID)
}

func TestScriptLanguage(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldReadKanjiAndKanaAsJapanese", have: "千と千尋の神隠し", expected: "ja"},
		{name: "ShouldReadKatakanaAsJapanese", have: "クイーン", expected: "ja"},
		{name: "ShouldReadDevanagariAsHindi", have: "दंगल", expected: "hi"},
		{name: "ShouldReadArabicScriptAsArabic", have: "تملي معاك", expected: "ar"},
		{name: "ShouldReadNoLanguageFromLatin", have: "Titanic"},
		{name: "ShouldReadNoLanguageFromDigits", have: "2001"},
		{name: "ShouldReadNoLanguageFromEmpty"},
		{name: "ShouldReadJapaneseAfterLatin", have: "Live 東京", expected: "ja"},
		{name: "ShouldReadNoLanguageFromCyrillic", have: "Москва"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ScriptLanguage(tc.have))
		})
	}
}

func TestIndexLanguage(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldReduceLocaleToLanguage", have: "ja-JP", expected: "ja"},
		{name: "ShouldKeepLanguage", have: "de", expected: "de"},
		{name: "ShouldFallBackToEnglishForUnknownLanguage", have: "xx", expected: "en"},
		{name: "ShouldFallBackToEnglishForEmpty", expected: "en"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ix, err := New(tc.have, Config{})
			require.NoError(t, err)
			assert.Equal(t, tc.expected, ix.Language())
			require.NoError(t, ix.Close())
		})
	}
}

// A search reports the score it counts as worth asking about, as configured, so callers that offer more than the top titles use the same bar.
func TestSearchReportsItsAskScore(t *testing.T) {
	ix, err := New("en", Config{AskScore: 0.8})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ix.Close() })
	require.NoError(t, ix.Upsert(movie("a", "Inception", 2010), movie("b", "Interstellar", 2014)))

	res, err := ix.Search(t.Context(), Query{Text: "inception"})
	require.NoError(t, err)
	assert.InDelta(t, 0.8, res.AskScore, 1e-9)
}
