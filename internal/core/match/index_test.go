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

// TestDefaultRetrieverFindsUnsegmentedScripts searches titles in scripts written without spaces with a letter missing. Segmenting them into words by Unicode rules, as bleve did, splits them into single characters or drops them, so only an exact title would find them.
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

	for _, c := range [][2]string{
		{"千と千尋の神隠", "spirited"},
		{"สวัสดีชาวโล", "thai"},
		{"ひらがなとカタカ", "kana"},
	} {
		res, err := ix.Search(t.Context(), Query{Text: c[0]})
		require.NoError(t, err)

		best, _ := res.Best()
		assert.Equal(t, c[1], best.Doc.ID, "%q", c[0])
	}
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

	for q, want := range map[string]string{"dune 1984": "d84", "dune twenty twenty one": "d21", "dune (2021)": "d21"} {
		res, err := ix.Search(t.Context(), Query{Text: q})
		require.NoError(t, err)

		best, _ := res.Best()
		assert.Equal(t, want, best.Doc.ID, q)
		assert.Equal(t, DecisionPlay, res.Decision, q)
	}

	res, err := ix.Search(t.Context(), Query{Text: "dune 1999"})
	require.NoError(t, err)
	assert.Equal(t, DecisionAsk, res.Decision)
}

// TestSearchAsksForMissingSequel checks that a number the best title lacks stops a play: with no sibling to compete, a long title used to outscore the number and start the original instead of asking.
func TestSearchAsksForMissingSequel(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix := newIndex(t, retr, movie("dundee", "Crocodile Dundee", 1986), movie("babe", "Babe", 1995), movie("bullitt", "Bullitt", 1968))

		for _, q := range []string{"crocodile dundee two", "crocodile dundee 3", "crocodile dundee part two"} {
			res, err := ix.Search(t.Context(), Query{Text: q})
			require.NoError(t, err)

			best, _ := res.Best()
			assert.Equal(t, DecisionAsk, res.Decision, q)
			assert.Equal(t, "dundee", best.Doc.ID, q)
		}

		for _, q := range []string{"crocodile dundee", "crocodile dundee 1986", "crocodile dundee one"} {
			res, err := ix.Search(t.Context(), Query{Text: q})
			require.NoError(t, err)

			best, _ := res.Best()
			assert.Equal(t, DecisionPlay, res.Decision, q)
			assert.Equal(t, "dundee", best.Doc.ID, q)
		}

		// A title that spells its number ("uno") still lacks the sequel's: matching as spelled out does not make "uno dos" agree with "uno".
		es, err := New("es", Config{Retriever: retr})
		require.NoError(t, err)
		t.Cleanup(func() { _ = es.Close() })
		require.NoError(t, es.Upsert(movie("padre", "Padre no hay más que uno", 2019), movie("tesis", "Tesis", 1996)))

		for q, want := range map[string]Decision{"padre no hay más que uno dos": DecisionAsk, "padre no hay más que uno 2": DecisionAsk, "padre no hay más que uno": DecisionPlay} {
			res, err := es.Search(t.Context(), Query{Text: q})
			require.NoError(t, err)

			best, _ := res.Best()
			assert.Equal(t, want, res.Decision, q)
			assert.Equal(t, "padre", best.Doc.ID, q)
		}

		// Not missing sequels: a spoken number that sounds like a title word ("four" for "for"), and a query number the title carries among others.
		ix = newIndex(t, retr, Doc{ID: "tff", Kind: media.KindArtist, Title: "Tears for Fears"}, movie("babe", "Babe", 1995))
		require.NoError(t, es.Upsert(movie("padre2", "Padre no hay más que uno 2: La llegada de la suegra", 2020)))

		for idx, cases := range map[*Index]map[string]string{ix: {"tears four fears": "tff"}, es: {"padre no hay más que uno la llegada de la suegra": "padre2"}} {
			for q, want := range cases {
				res, err := idx.Search(t.Context(), Query{Text: q})
				require.NoError(t, err)

				best, _ := res.Best()
				assert.Equal(t, DecisionPlay, res.Decision, q)
				assert.Equal(t, want, best.Doc.ID, q)
			}
		}
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
