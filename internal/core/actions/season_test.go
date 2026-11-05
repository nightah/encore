package actions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

func seasonLibrary(eps ...media.Item) []media.Item {
	return append([]media.Item{{Kind: media.KindTVShow, ID: 20, Title: "Bluey"}}, eps...)
}

func blueyEpisode(id, season, episode, plays int, resume time.Duration) media.Item {
	return media.Item{Kind: media.KindEpisode, ID: id, ShowID: 20, ShowTitle: "Bluey", Season: season, Episode: episode, Title: "Episode", PlayCount: plays, Resume: resume}
}

func TestSeason(t *testing.T) {
	t.Run("from the first unwatched episode to the end of the season", func(t *testing.T) {
		target, backend, _ := newTarget(seasonLibrary(
			blueyEpisode(101, 1, 1, 1, 0),
			blueyEpisode(201, 2, 1, 1, 0),
			blueyEpisode(202, 2, 2, 0, 0),
			blueyEpisode(203, 2, 3, 0, 0),
			blueyEpisode(301, 3, 1, 0, 0),
		)...)

		o := newService().Season(t.Context(), target, "bluey", 2, nil)

		assert.Equal(t, "tv.playing_season", o.Say.ID)
		assert.Equal(t, map[string]string{"show": "Bluey", "season": "2", "episode": "2", "title": "Episode"}, o.Say.Params)
		assert.Equal(t, "Queue episode/202,episode/203", lastCall(backend.Calls()), "the next season isn't queued")
		require.NotNil(t, o.Played)
		assert.Equal(t, 202, o.Played.ID)
	})

	t.Run("an episode in progress comes before an unwatched one", func(t *testing.T) {
		target, backend, _ := newTarget(seasonLibrary(
			blueyEpisode(201, 2, 1, 0, 0),
			blueyEpisode(202, 2, 2, 0, 3*time.Minute),
		)...)

		o := newService().Season(t.Context(), target, "bluey", 2, nil)

		assert.Equal(t, "2", o.Say.Params["episode"])
		assert.Equal(t, "Queue episode/202", lastCall(backend.Calls()))
	})

	t.Run("a watched season plays again from its first episode", func(t *testing.T) {
		target, backend, _ := newTarget(seasonLibrary(
			blueyEpisode(202, 2, 2, 1, 0),
			blueyEpisode(201, 2, 1, 3, 0),
			blueyEpisode(301, 3, 1, 0, 0),
		)...)

		o := newService().Season(t.Context(), target, "bluey", 2, nil)

		assert.Equal(t, "1", o.Say.Params["episode"])
		assert.Equal(t, "Queue episode/201,episode/202", lastCall(backend.Calls()))
	})

	t.Run("capped at the queue limit", func(t *testing.T) {
		target, backend, _ := newTarget(seasonLibrary(
			blueyEpisode(201, 2, 1, 0, 0),
			blueyEpisode(202, 2, 2, 0, 0),
			blueyEpisode(203, 2, 3, 0, 0),
		)...)
		target.QueueLimit = 2

		newService().Season(t.Context(), target, "bluey", 2, nil)

		assert.Equal(t, "Queue episode/201,episode/202", lastCall(backend.Calls()))
	})

	t.Run("a season the library doesn't have", func(t *testing.T) {
		target, backend, _ := newTarget(seasonLibrary(blueyEpisode(101, 1, 1, 0, 0))...)

		o := newService().Season(t.Context(), target, "bluey", 4, nil)

		assert.Equal(t, "tv.no_season", o.Say.ID)
		assert.Equal(t, map[string]string{"show": "Bluey", "season": "4"}, o.Say.Params)
		assert.NotContains(t, lastCall(backend.Calls()), "Queue")
		assert.False(t, o.Missed(), "the show was found, so no other reading is tried")
	})

	t.Run("a show the library doesn't have", func(t *testing.T) {
		target, backend, _ := newTarget()

		o := newService().Season(t.Context(), target, "the bear", 2, nil)

		assert.Equal(t, "match.not_found", o.Say.ID)
		assert.True(t, o.Missed())
		assert.Empty(t, backend.Calls())
	})

	t.Run("a failing backend", func(t *testing.T) {
		for _, method := range []string{"Episodes", "Queue"} {
			target, backend, _ := newTarget(seasonLibrary(blueyEpisode(201, 2, 1, 0, 0))...)
			target, _ = failing(target, backend, errBoom, method)

			assert.ErrorIs(t, newService().Season(t.Context(), target, "bluey", 2, nil).Err, errBoom, method)
		}
	})
}

func TestSeasonOfThePlayingShow(t *testing.T) {
	t.Run("the show playing now", func(t *testing.T) {
		target, backend, _ := newTarget(seasonLibrary(blueyEpisode(101, 1, 1, 0, 0), blueyEpisode(301, 3, 1, 0, 0))...)
		backend.SetPlaying(&media.NowPlaying{Item: blueyEpisode(101, 1, 1, 0, 0)})

		o := newService().Season(t.Context(), target, "", 3, nil)

		assert.Equal(t, "tv.playing_season", o.Say.ID)
		assert.Equal(t, "Bluey", o.Say.Params["show"])
		assert.Equal(t, "Queue episode/301", lastCall(backend.Calls()))
	})

	t.Run("nothing playing", func(t *testing.T) {
		target, backend, _ := newTarget()

		o := newService().Season(t.Context(), target, "", 3, nil)

		assert.Equal(t, "tv.season_needs_show", o.Say.ID)
		assert.Equal(t, map[string]string{"season": "3"}, o.Say.Params)
		assert.Equal(t, []string{"NowPlaying"}, backend.Calls())
	})

	t.Run("a film playing", func(t *testing.T) {
		target, backend, _ := newTarget()
		backend.SetPlaying(&media.NowPlaying{Item: media.Item{Kind: media.KindMovie, ID: 4, Title: "Hot Fuzz"}})

		assert.Equal(t, "tv.season_needs_show", newService().Season(t.Context(), target, "", 3, nil).Say.ID)
	})

	t.Run("a failing backend", func(t *testing.T) {
		target, backend, _ := newTarget()
		target, _ = failing(target, backend, errBoom, "NowPlaying")

		assert.ErrorIs(t, newService().Season(t.Context(), target, "", 3, nil).Err, errBoom)
	})
}
