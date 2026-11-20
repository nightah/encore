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

	testCases := []struct {
		name     string
		query    string
		quality  media.Quality
		expected string
		from     time.Duration
	}{
		{name: "ShouldPlayHighestQualityWithoutPreference", query: "dune part two", expected: "movie/2"},
		{name: "ShouldPlayHighestQualityWhenPreferred", query: "dune part two", quality: media.QualityHighest, expected: "movie/2"},
		{name: "ShouldPlayPreferredQuality", query: "dune part two", quality: media.Quality1080p, expected: "movie/1"},
		{name: "ShouldPlayNextQualityBelowPreferredWhenMissing", query: "dune part two", quality: media.Quality720p, expected: "movie/1"},
		{name: "ShouldStartWhereOtherCopyStopped", query: "hot fuzz", quality: media.Quality2160p, expected: "movie/3", from: 10 * time.Minute},
		{name: "ShouldPlayPreferredCopyThatStopped", query: "hot fuzz", quality: media.Quality1080p, expected: "movie/4"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ix.Search(t.Context(), Query{Text: tc.query, Quality: tc.quality})
			require.NoError(t, err)
			require.Equal(t, DecisionPlay, res.Decision)

			best, ok := res.Best()
			require.True(t, ok)
			assert.Equal(t, tc.expected, best.Doc.ID)
			assert.Equal(t, tc.from, best.From, "starts where the other copy stopped")
			assert.Len(t, res.Candidates, 1, "copies are one choice")
		})
	}
}
