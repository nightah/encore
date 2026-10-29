package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

func TestReloadAliasesUpdatesTheIndex(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		ctx := t.Context()
		id := h.instance(t).id

		top := func() string {
			t.Helper()

			res, err := h.app.Search(ctx, id, match.Query{Text: "the cornetto one", Kinds: []media.Kind{media.KindMovie}})
			require.NoError(t, err)

			if len(res.Candidates) == 0 || res.Candidates[0].Breakdown.Alias == "" {
				return ""
			}

			return res.Candidates[0].Doc.Title
		}

		require.Empty(t, top(), "no alias yet")

		alias := store.TitleAlias{KodiInstanceID: id, Media: media.Ref{Kind: media.KindMovie, ID: 6}, Alias: "the cornetto one"}
		require.NoError(t, h.app.DB().Aliases().Add(ctx, &alias))
		assert.Empty(t, top(), "a saved alias waits for the reload")

		require.NoError(t, h.app.ReloadAliases(ctx))
		assert.Equal(t, "Hot Fuzz", top())

		require.NoError(t, h.app.DB().Aliases().Delete(ctx, alias.ID))
		require.NoError(t, h.app.ReloadAliases(ctx))
		assert.Empty(t, top(), "a removed alias leaves the index")
	})
}

func TestDocFor(t *testing.T) {
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		item    media.Item
		aliases []string
		ok      bool
		want    match.Doc
	}{
		{name: "untitled", item: media.Item{Kind: media.KindMovie, ID: 1}},
		{name: "episode", item: media.Item{Kind: media.KindEpisode, ID: 1, Title: "Rain"}},
		{
			name:    "aliases deduplicated",
			item:    media.Item{Kind: media.KindMovie, ID: 5, Title: "Amélie", SortTitle: "Amelie", OriginalTitle: "Amélie", Year: 2001},
			aliases: []string{"Amelie", "amelie the film"},
			ok:      true,
			want:    match.Doc{ID: "movie/5", Kind: media.KindMovie, Ref: media.Ref{Kind: media.KindMovie, ID: 5}, Title: "Amélie", Year: 2001, Aliases: []string{"Amelie", "amelie the film"}},
		},
		{
			name: "in progress",
			item: media.Item{Kind: media.KindMovie, ID: 3, Title: "Dune: Part Two", Resume: time.Hour, DateAdded: now.Add(-time.Hour)},
			ok:   true,
			want: match.Doc{ID: "movie/3", Kind: media.KindMovie, Ref: media.Ref{Kind: media.KindMovie, ID: 3}, Title: "Dune: Part Two", Boost: inProgressBoost},
		},
		{
			name: "recently added",
			item: media.Item{Kind: media.KindTVShow, ID: 7, Title: "Bluey", DateAdded: now.Add(-recentlyAddedFor + time.Minute)},
			ok:   true,
			want: match.Doc{ID: "tvshow/7", Kind: media.KindTVShow, Ref: media.Ref{Kind: media.KindTVShow, ID: 7}, Title: "Bluey", Boost: recentlyAddedBoost},
		},
		{
			name: "added a while ago",
			item: media.Item{Kind: media.KindMovie, ID: 8, Title: "Rocky II", DateAdded: now.Add(-recentlyAddedFor)},
			ok:   true,
			want: match.Doc{ID: "movie/8", Kind: media.KindMovie, Ref: media.Ref{Kind: media.KindMovie, ID: 8}, Title: "Rocky II"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, ok := docFor(&c.item, c.aliases, now)
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, doc)
		})
	}
}
