package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSameTitleAndYearIsOneChoice checks that a library holding a title twice, such as two copies of a film, plays it rather than asking which of two identical choices was meant, while titles that differ by year are still asked about.
func TestSameTitleAndYearIsOneChoice(t *testing.T) {
	ix, err := New("en", Config{})
	require.NoError(t, err)

	t.Cleanup(func() { _ = ix.Close() })

	require.NoError(t, ix.Upsert(
		Doc{ID: "movie/1", Kind: "movie", Title: "Bridget Jones: Mad About the Boy", Year: 2025},
		Doc{ID: "movie/2", Kind: "movie", Title: "Bridget Jones: Mad About the Boy", Year: 2025},
		Doc{ID: "movie/3", Kind: "movie", Title: "Hunter", Year: 1999},
		Doc{ID: "movie/4", Kind: "movie", Title: "Hunter", Year: 2011},
	))

	res, err := ix.Search(t.Context(), Query{Text: "bridget jones mad about the boy"})
	require.NoError(t, err)
	assert.Equal(t, DecisionPlay, res.Decision)

	best, ok := res.Best()
	require.True(t, ok)
	assert.Equal(t, "movie/1", best.Doc.ID, "the first copy")

	for _, c := range res.Candidates {
		assert.NotEqual(t, "movie/2", c.Doc.ID, "the second copy isn't a candidate of its own")
	}

	res, err = ix.Search(t.Context(), Query{Text: "hunter"})
	require.NoError(t, err)
	assert.Equal(t, DecisionAsk, res.Decision, "a title made twice, in different years, is still a real choice")
}
