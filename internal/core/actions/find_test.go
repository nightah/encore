package actions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

func TestGenreKey(t *testing.T) {
	for in, want := range map[string]string{
		"Science Fiction": "sciencefiction",
		"sci-fi":          "sciencefiction",
		"SF":              "sciencefiction",
		"comedies":        "comedy",
		"Westerns":        "western",
		"cartoons":        "animation",
		"kids":            "children",
		"docos":           "documentary",
		"R&B":             "randb",
		"Hip Hop":         "hiphop",
		"glass":           "glass",
		"bus":             "bus",
		"pies":            "pie",
		"ties":            "tie",
		"":                "",
	} {
		assert.Equal(t, want, genreKey("en", in), in)
	}
}

func TestFindGenre(t *testing.T) {
	items := []media.Item{
		{Genres: []string{"Action", "Science Fiction"}},
		{Genres: []string{"Documentary"}},
		{Genres: []string{"Science"}},
	}

	for _, tc := range []struct {
		spoken string
		want   string
		ok     bool
	}{
		{"sci fi", "Science Fiction", true},
		{"docos", "Documentary", true},
		{"science", "Science", true},
		{"scien", "Science Fiction", true},
		{"act", "", false},
		{"horror", "", false},
		{"", "", false},
		{"!!!", "", false},
	} {
		g, ok := findGenre("en", items, tc.spoken)
		assert.Equal(t, tc.ok, ok, tc.spoken)
		assert.Equal(t, tc.want, g, tc.spoken)
	}

	_, ok := findGenre("en", nil, "action")
	assert.False(t, ok, "an empty library has no genres")
}

func TestInGenre(t *testing.T) {
	items := []media.Item{{ID: 1, Genres: []string{"Rock"}}, {ID: 2, Genres: []string{"Pop", "rock"}}, {ID: 3, Genres: []string{"Jazz"}}}

	assert.Equal(t, items, inGenre(items, ""), "no genre keeps everything")
	assert.Equal(t, items[:2], inGenre(items, "ROCK"), "genres compare without case")
	assert.Empty(t, inGenre(items, "Rockabilly"), "a genre is a whole name, not a prefix")
}

func TestByArtist(t *testing.T) {
	duet := media.Item{Artist: "Queen, David Bowie"}

	assert.True(t, byArtist("en", duet, "queen"))
	assert.True(t, byArtist("en", duet, "david bowie"))
	assert.True(t, byArtist("en", media.Item{Artist: "AC/DC"}, "ac dc"), "the whole field is compared before it is split")
	assert.True(t, byArtist("en", media.Item{Artist: "Simon & Garfunkel; Paul Simon"}, "paul simon"))
	assert.True(t, byArtist("en", media.Item{Artist: "The Beatles"}, "beatles"), "articles are ignored")
	assert.False(t, byArtist("en", duet, "bowie"), "a part must match whole")
	assert.False(t, byArtist("en", media.Item{}, ""), "an empty artist matches nothing")
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
