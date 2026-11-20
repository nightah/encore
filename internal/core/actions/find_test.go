package actions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

func TestGenreKey(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldJoinWords", have: "Science Fiction", expected: "sciencefiction"},
		{name: "ShouldExpandSciFi", have: "sci-fi", expected: "sciencefiction"},
		{name: "ShouldExpandSF", have: "SF", expected: "sciencefiction"},
		{name: "ShouldSingulariseIes", have: "comedies", expected: "comedy"},
		{name: "ShouldSingulariseS", have: "Westerns", expected: "western"},
		{name: "ShouldMapCartoonsToAnimation", have: "cartoons", expected: "animation"},
		{name: "ShouldMapKidsToChildren", have: "kids", expected: "children"},
		{name: "ShouldMapDocosToDocumentary", have: "docos", expected: "documentary"},
		{name: "ShouldSpellOutAnAmpersand", have: "R&B", expected: "randb"},
		{name: "ShouldJoinHipHop", have: "Hip Hop", expected: "hiphop"},
		{name: "ShouldKeepADoubleS", have: "glass", expected: "glass"},
		{name: "ShouldKeepAShortWordEndingInS", have: "bus", expected: "bus"},
		{name: "ShouldSingularisePies", have: "pies", expected: "pie"},
		{name: "ShouldSingulariseTies", have: "ties", expected: "tie"},
		{name: "ShouldKeepEmpty"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, genreKey("en", tc.have))
		})
	}
}

func TestFindGenre(t *testing.T) {
	items := []media.Item{
		{Genres: []string{"Action", "Science Fiction"}},
		{Genres: []string{"Documentary"}},
		{Genres: []string{"Science"}},
	}

	testCases := []struct {
		name     string
		have     string
		expected string
		ok       bool
	}{
		{name: "ShouldFindAnAlias", have: "sci fi", expected: "Science Fiction", ok: true},
		{name: "ShouldFindASlangName", have: "docos", expected: "Documentary", ok: true},
		{name: "ShouldPreferAnExactName", have: "science", expected: "Science", ok: true},
		{name: "ShouldFindAPrefix", have: "scien", expected: "Science Fiction", ok: true},
		{name: "ShouldNotFindAShortPrefix", have: "act"},
		{name: "ShouldNotFindAMissingGenre", have: "horror"},
		{name: "ShouldNotFindEmpty"},
		{name: "ShouldNotFindPunctuation", have: "!!!"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := findGenre("en", items, tc.have)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.expected, actual)
		})
	}

	_, ok := findGenre("en", nil, "action")
	assert.False(t, ok, "an empty library has no genres")
}

func TestInGenre(t *testing.T) {
	items := []media.Item{{ID: 1, Genres: []string{"Rock"}}, {ID: 2, Genres: []string{"Pop", "rock"}}, {ID: 3, Genres: []string{"Jazz"}}}

	testCases := []struct {
		name     string
		have     string
		expected []media.Item
	}{
		{name: "ShouldKeepEverythingForNoGenre", expected: items},
		{name: "ShouldCompareGenresWithoutCase", have: "ROCK", expected: items[:2]},
		{name: "ShouldMatchAWholeNameNotAPrefix", have: "Rockabilly"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := inGenre(items, tc.have)

			if tc.expected == nil {
				assert.Empty(t, actual)

				return
			}

			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestByArtist(t *testing.T) {
	duet := media.Item{Artist: "Queen, David Bowie"}

	testCases := []struct {
		name     string
		have     media.Item
		spoken   string
		expected bool
	}{
		{name: "ShouldMatchTheFirstOfADuet", have: duet, spoken: "queen", expected: true},
		{name: "ShouldMatchTheSecondOfADuet", have: duet, spoken: "david bowie", expected: true},
		{name: "ShouldCompareTheWholeFieldBeforeSplittingIt", have: media.Item{Artist: "AC/DC"}, spoken: "ac dc", expected: true},
		{name: "ShouldSplitOnASemicolon", have: media.Item{Artist: "Simon & Garfunkel; Paul Simon"}, spoken: "paul simon", expected: true},
		{name: "ShouldIgnoreArticles", have: media.Item{Artist: "The Beatles"}, spoken: "beatles", expected: true},
		{name: "ShouldNotMatchPartOfAName", have: duet, spoken: "bowie"},
		{name: "ShouldNotMatchAnEmptyArtist"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, byArtist("en", tc.have, tc.spoken))
		})
	}
}

func TestFindIgnoresBlankNames(t *testing.T) {
	target, _, _ := newTarget()
	m := &brokenMatcher{}
	target.Matcher = m

	_, ok, err := newService().find(t.Context(), target, "  ", media.KindMovie)

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Zero(t, m.searches, "a blank name isn't searched for")
}

func TestItemsPreferTheCatalog(t *testing.T) {
	target, backend, _ := newTarget()
	cached := []media.Item{{Kind: media.KindSong, ID: 1, Title: "Cached"}}
	target.Library = &testkit.Library{Cached: cached}
	svc := newService()

	items, err := svc.items(t.Context(), target, media.KindSong)
	require.NoError(t, err)
	assert.Equal(t, cached, items)
	assert.Empty(t, backend.Calls(), "the catalog answers without asking Kodi")

	items, err = svc.items(t.Context(), target, media.KindAlbum)
	require.NoError(t, err)
	assert.Len(t, items, 3, "an empty catalog falls back to Kodi")
	assert.Equal(t, []string{"Library album"}, backend.Calls())
}
