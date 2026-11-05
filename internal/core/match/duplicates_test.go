package match

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
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

// TestCopiesFollowThePreferredQuality checks that the copy played is the one the request's quality preference picks, starting where another copy stopped when that's further along.
func TestCopiesFollowThePreferredQuality(t *testing.T) {
	ix, err := New("en", Config{})
	require.NoError(t, err)

	t.Cleanup(func() { _ = ix.Close() })

	require.NoError(t, ix.Upsert(
		Doc{ID: "movie/1", Kind: "movie", Title: "Dune: Part Two", Year: 2024, Copy: media.Copy{Quality: media.Quality1080p}},
		Doc{ID: "movie/2", Kind: "movie", Title: "Dune: Part Two", Year: 2024, Copy: media.Copy{Quality: media.Quality2160p}},
		Doc{ID: "movie/3", Kind: "movie", Title: "Hot Fuzz", Year: 2007, Copy: media.Copy{Quality: media.Quality2160p}},
		Doc{ID: "movie/4", Kind: "movie", Title: "Hot Fuzz", Year: 2007, Copy: media.Copy{Quality: media.Quality1080p, Resume: 10 * time.Minute}},
	))

	for _, tc := range []struct {
		query   string
		quality media.Quality
		want    string
		from    time.Duration
	}{
		{"dune part two", "", "movie/2", 0},
		{"dune part two", media.QualityHighest, "movie/2", 0},
		{"dune part two", media.Quality1080p, "movie/1", 0},
		{"dune part two", media.Quality720p, "movie/1", 0},
		{"hot fuzz", media.Quality2160p, "movie/3", 10 * time.Minute},
		{"hot fuzz", media.Quality1080p, "movie/4", 0},
	} {
		res, err := ix.Search(t.Context(), Query{Text: tc.query, Quality: tc.quality})
		require.NoError(t, err)
		require.Equal(t, DecisionPlay, res.Decision, tc.query)

		best, ok := res.Best()
		require.True(t, ok)
		assert.Equal(t, tc.want, best.Doc.ID, "%s preferring %q", tc.query, tc.quality)
		assert.Equal(t, tc.from, best.From, "%s preferring %q starts where the other copy stopped", tc.query, tc.quality)
		assert.Len(t, res.Candidates, 1, "copies are one choice")
	}
}
