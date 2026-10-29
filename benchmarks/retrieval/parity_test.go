package retrieval

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
)

// newIndexes builds the same library with the inverted index (the default) and with bleve.
func newIndexes(tb testing.TB, lang string, cfg match.Config, docs []match.Doc) (inverted, blv *match.Index) {
	tb.Helper()

	inverted, err := match.New(lang, cfg)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = inverted.Close() })

	blv, err = match.NewWithRetriever(lang, cfg, NewBleve(lang))
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = blv.Close() })

	require.NoError(tb, inverted.Upsert(docs...))
	require.NoError(tb, blv.Upsert(docs...))

	return inverted, blv
}

// sameResults checks that both indexes retrieve the same candidates for q, in any order, and that Search then returns the same result.
func sameResults(t *testing.T, inverted, blv *match.Index, q match.Query) {
	t.Helper()

	ctx := t.Context()

	a, err := inverted.Candidates(ctx, q)
	require.NoError(t, err)

	b, err := blv.Candidates(ctx, q)
	require.NoError(t, err)

	slices.Sort(a)
	slices.Sort(b)
	assert.Equal(t, b, a, "candidates for %q %v", q.Text, q.Kinds)

	sameSearch(t, inverted, blv, q)
}

// sameSearch checks that Search returns the same result from both indexes.
func sameSearch(t *testing.T, inverted, blv *match.Index, q match.Query) {
	t.Helper()

	ctx := t.Context()

	ra, err := inverted.Search(ctx, q)
	require.NoError(t, err)

	rb, err := blv.Search(ctx, q)
	require.NoError(t, err)

	assert.Equal(t, rb, ra, "result for %q %v", q.Text, q.Kinds)
}

// TestParityCorpus runs every ASR corpus case, and every title of the suite's library as a query, through both retrievers.
func TestParityCorpus(t *testing.T) {
	for _, s := range loadCorpus(t).Suites {
		t.Run(s.Name, func(t *testing.T) {
			docs := s.docs()
			inverted, blv := newIndexes(t, s.Lang, match.Config{}, docs)

			queries := s.queries()
			for _, d := range docs {
				queries = append(queries, match.Query{Text: d.Title})
			}

			for _, q := range queries {
				sameResults(t, inverted, blv, q)
			}
		})
	}
}

// TestParityScale runs the scale harness's synthetic queries and the English ASR corpus cases over a 10,000-title scale library. At the default pool of 800 candidates, retrieval cuts off inside runs of equal scores, where the two retrievers may keep different Docs, so the candidates and Search results are compared with a pool as large as the library, and decisions at the default pool.
func TestParityScale(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 10,000-title library four times")
	}

	suite := englishSuite(t)
	docs := scaleDocs(10_000, suite.docs(), 1)
	allInverted, allBleve := newIndexes(t, "en", match.Config{CandidatePool: len(docs)}, docs)
	inverted, blv := newIndexes(t, "en", match.Config{}, docs)

	for _, q := range append(scaleQueries(docs, 700, 2), suite.queries()...) {
		sameResults(t, allInverted, allBleve, q)
		sameDecision(t, inverted, blv, q)
	}
}

// sameDecision checks that Search decides the same with both indexes, on the same top candidate with the same score and margin. Lower candidates may differ where the pool was cut inside a run of equal retrieval scores.
func sameDecision(t *testing.T, inverted, blv *match.Index, q match.Query) {
	t.Helper()

	ctx := t.Context()

	ra, err := inverted.Search(ctx, q)
	require.NoError(t, err)

	rb, err := blv.Search(ctx, q)
	require.NoError(t, err)

	assert.Equal(t, rb.Decision, ra.Decision, "decision for %q", q.Text)
	assert.InDelta(t, rb.Margin, ra.Margin, 0, "margin for %q", q.Text)

	if assert.Len(t, ra.Candidates, len(rb.Candidates), "candidates for %q", q.Text) && len(ra.Candidates) > 0 {
		assert.Equal(t, rb.Candidates[0], ra.Candidates[0], "top candidate for %q", q.Text)
	}
}
