package actions

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

func seeded() *Service {
	s := newService()
	s.SetSeed(1)

	return s
}

func TestRandomMovie(t *testing.T) {
	target, backend, _ := newTarget()

	o := seeded().RandomMovie(t.Context(), target, "", nil)
	require.NotNil(t, o.Played)
	assert.Zero(t, o.Played.PlayCount, "unwatched movies come first")
	assert.Contains(t, []int{3, 4}, o.Played.ID)

	o = seeded().RandomMovie(t.Context(), target, "comedies", nil)
	assert.Equal(t, "movie.playing_genre", o.Say.ID)
	assert.Equal(t, map[string]string{"title": "Hot Fuzz", "genre": "Comedy"}, o.Say.Params)
	assert.Contains(t, backend.Calls(), "Play movie/4", "a random movie starts from the beginning")

	assert.Equal(t, "library.no_genre", seeded().RandomMovie(t.Context(), target, "horror", nil).Say.ID)
}

func TestTrailer(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.Trailer(t.Context(), target, "hot fuzz", nil)
	assert.Equal(t, "movie.playing_trailer", o.Say.ID)
	assert.Contains(t, backend.Calls(), "PlayFile plugin://plugin.video.youtube/play/?video_id=ayTnvVpj9t4")

	pending := []Choice{{Ref: media.Ref{Kind: media.KindMovie, ID: 4}, Title: "Hot Fuzz"}}
	assert.Equal(t, "movie.playing_trailer", svc.Trailer(t.Context(), target, "", pending).Say.ID, "the recommended movie's trailer")

	o = svc.Trailer(t.Context(), target, "dune: part two", nil)
	assert.Equal(t, "movie.no_trailer", o.Say.ID)

	assert.Equal(t, "movie.which_trailer", svc.Trailer(t.Context(), target, "", nil).Say.ID)
	assert.Equal(t, "match.not_found", svc.Trailer(t.Context(), target, "alien", nil).Say.ID)
}

func TestNextInCollection(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.NextInCollection(t.Context(), target, "dune", nil)
	assert.Equal(t, "playback.playing_movie", o.Say.ID)
	assert.Equal(t, "Dune: Part Two", o.Say.Params["title"], "the first unwatched movie of the set, in release order")
	assert.Contains(t, backend.Calls(), "Play movie/3 resume")

	assert.Equal(t, "movie.no_collection", svc.NextInCollection(t.Context(), target, "hot fuzz", nil).Say.ID)
}

func TestEpisode(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.Episode(t.Context(), target, "severance", 2, 3, nil)
	assert.Equal(t, "playback.playing_episode", o.Say.ID)
	assert.Equal(t, "Who Is Alive?", o.Say.Params["title"])
	assert.Contains(t, backend.Calls(), "Play episode/101 resume")

	o = svc.Episode(t.Context(), target, "severance", 9, 1, nil)
	assert.Equal(t, "tv.no_episode", o.Say.ID)
	assert.Equal(t, map[string]string{"show": "Severance", "season": "9", "episode": "1"}, o.Say.Params)

	assert.Equal(t, "match.not_found", svc.Episode(t.Context(), target, "lost", 1, 1, nil).Say.ID)
}

func TestLatestEpisode(t *testing.T) {
	target, _, _ := newTarget()

	o := newService().LatestEpisode(t.Context(), target, "severance", nil)
	assert.Equal(t, "Trojan's Horse", o.Say.Params["title"])
}

func TestFirstEpisode(t *testing.T) {
	target, _, _ := newTarget()

	o := newService().FirstEpisode(t.Context(), target, "severance", nil)
	assert.Equal(t, "Who Is Alive?", o.Say.Params["title"])
}

func TestContinueWatching(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().ContinueWatching(t.Context(), target, nil)
	assert.Equal(t, "playback.playing_episode", o.Say.ID)
	assert.Equal(t, "Severance", o.Say.Params["show"], "Bluey was watched last but is finished")
	assert.Contains(t, backend.Calls(), "Play episode/102 resume")
}

func TestRandomEpisodeAndShow(t *testing.T) {
	target, _, _ := newTarget()

	o := seeded().RandomEpisode(t.Context(), target, "bluey", nil)
	require.NotNil(t, o.Played)
	assert.Equal(t, 201, o.Played.ID)

	o = seeded().RandomShow(t.Context(), target, "", nil)
	require.NotNil(t, o.Played)
	assert.Equal(t, "Severance", o.Played.ShowTitle, "shows with unwatched episodes come first")

	o = seeded().RandomShow(t.Context(), target, "kids", nil)
	require.NotNil(t, o.Played)
	assert.Equal(t, "Bluey", o.Played.ShowTitle, "a finished show plays a random episode")
}

func TestShuffleShow(t *testing.T) {
	target, backend, _ := newTarget()
	target.QueueLimit = 25

	o := newService().ShuffleShow(t.Context(), target, "severance", nil)
	assert.Equal(t, "playback.shuffling", o.Say.ID)
	assert.Equal(t, []string{"Play tvshow/10 shuffle limit 25"}, backend.Calls())
}

func TestBinge(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().Binge(t.Context(), target, "severance", nil)
	assert.Equal(t, "tv.binge", o.Say.ID)
	assert.Equal(t, map[string]string{"show": "Severance", "count": "2", "season": "2", "episode": "4"}, o.Say.Params)
	assert.Contains(t, backend.Calls(), "Queue episode/102,episode/103")

	target.QueueLimit = 1
	newService().Binge(t.Context(), target, "severance", nil)
	assert.Contains(t, backend.Calls(), "Queue episode/102", "capped at the queue limit")

	assert.Equal(t, "playback.all_watched", newService().Binge(t.Context(), target, "bluey", nil).Say.ID)
}

func TestNextInCollectionOrder(t *testing.T) {
	alien := func(id, year, plays int, resume time.Duration) media.Item {
		return media.Item{Kind: media.KindMovie, ID: id, Title: fmt.Sprintf("Alien %d", id), Year: year, SetID: 7, SetTitle: "Alien Collection", PlayCount: plays, Resume: resume}
	}

	t.Run("a film in progress comes before an unwatched one", func(t *testing.T) {
		target, backend, _ := newTarget(alien(1, 1979, 1, 0), alien(2, 1986, 0, 0), alien(3, 1992, 1, 20*time.Minute))

		o := newService().NextInCollection(t.Context(), target, "alien 1", nil)

		assert.Equal(t, "Alien 3", o.Say.Params["title"])
		assert.Equal(t, "Play movie/3 resume", lastCall(backend.Calls()))
	})

	t.Run("release order, then ID", func(t *testing.T) {
		target, _, _ := newTarget(alien(9, 1997, 0, 0), alien(1, 1979, 1, 0), alien(8, 1997, 0, 0))

		o := newService().NextInCollection(t.Context(), target, "alien 1", nil)

		assert.Equal(t, "Alien 8", o.Say.Params["title"])
	})

	t.Run("all watched", func(t *testing.T) {
		target, backend, _ := newTarget(alien(1, 1979, 1, 0), alien(2, 1986, 2, 0))

		o := newService().NextInCollection(t.Context(), target, "alien 2", nil)

		assert.Equal(t, "movie.collection_watched", o.Say.ID)
		assert.Equal(t, map[string]string{"collection": "Alien Collection"}, o.Say.Params)
		assert.NotContains(t, strings.Join(backend.Calls(), "\n"), "Play", "nothing plays")
	})

	t.Run("unknown film", func(t *testing.T) {
		target, _, _ := newTarget()

		o := newService().NextInCollection(t.Context(), target, "alien", nil)

		assert.Equal(t, "match.not_found", o.Say.ID)
	})
}

func TestTrailerNeedsAMovie(t *testing.T) {
	target, backend, _ := newTarget()
	pending := []Choice{{Ref: media.Ref{Kind: media.KindTVShow, ID: 10}, Title: "Severance"}}

	assert.Equal(t, "movie.which_trailer", newService().Trailer(t.Context(), target, "", pending).Say.ID, "a recommended show has no trailer")
	assert.Empty(t, backend.Calls())
}

func TestShowsNotFound(t *testing.T) {
	target, backend, _ := newTarget()
	svc := seeded()

	for name, o := range map[string]Outcome{
		"first episode":  svc.FirstEpisode(t.Context(), target, "the bear", nil),
		"latest episode": svc.LatestEpisode(t.Context(), target, "the bear", nil),
		"random episode": svc.RandomEpisode(t.Context(), target, "the bear", nil),
		"shuffle show":   svc.ShuffleShow(t.Context(), target, "the bear", nil),
		"binge":          svc.Binge(t.Context(), target, "the bear", nil),
	} {
		assert.Equal(t, "match.not_found", o.Say.ID, name)
		assert.Equal(t, "the bear", o.Say.Params["query"], name)
	}

	assert.Empty(t, backend.Calls())
}

func TestShowsWithoutEpisodes(t *testing.T) {
	andor := media.Item{Kind: media.KindTVShow, ID: 30, Title: "Andor"}
	target, backend, _ := newTarget(andor)
	svc := seeded()

	o := svc.LatestEpisode(t.Context(), target, "andor", nil)
	assert.Equal(t, "tv.no_episodes", o.Say.ID)
	assert.Equal(t, map[string]string{"show": "Andor"}, o.Say.Params)

	o = svc.FirstEpisode(t.Context(), target, "andor", nil)
	assert.Equal(t, "tv.no_episodes", o.Say.ID)

	o = svc.RandomEpisode(t.Context(), target, "andor", nil)
	assert.Equal(t, "tv.no_episodes", o.Say.ID)

	assert.Equal(t, []string{"Episodes 30", "Episodes 30", "Episodes 30"}, backend.Calls())
}

func TestLatestEpisodeIgnoresSpecials(t *testing.T) {
	special := media.Item{Kind: media.KindEpisode, ID: 300, ShowID: 30, ShowTitle: "Andor", Season: 0, Episode: 1, Title: "Behind the Scenes"}
	first := media.Item{Kind: media.KindEpisode, ID: 301, ShowID: 30, ShowTitle: "Andor", Season: 1, Episode: 1, Title: "Kassa"}
	show := media.Item{Kind: media.KindTVShow, ID: 30, Title: "Andor"}

	target, backend, _ := newTarget(show, special, first)
	newService().LatestEpisode(t.Context(), target, "andor", nil)
	assert.Equal(t, "Play episode/301 resume", lastCall(backend.Calls()), "the special sorts first but isn't the latest")

	target, backend, _ = newTarget(show, special)
	o := newService().LatestEpisode(t.Context(), target, "andor", nil)
	assert.Equal(t, "Behind the Scenes", o.Say.Params["title"], "a show of only specials plays its last special")
	assert.Equal(t, "Play episode/300 resume", lastCall(backend.Calls()))
}

func TestFirstEpisodeIgnoresSpecials(t *testing.T) {
	special := media.Item{Kind: media.KindEpisode, ID: 300, ShowID: 30, ShowTitle: "Andor", Season: 0, Episode: 1, Title: "Behind the Scenes"}
	first := media.Item{Kind: media.KindEpisode, ID: 301, ShowID: 30, ShowTitle: "Andor", Season: 1, Episode: 1, Title: "Kassa"}
	show := media.Item{Kind: media.KindTVShow, ID: 30, Title: "Andor"}

	target, backend, _ := newTarget(show, special, first)
	newService().FirstEpisode(t.Context(), target, "andor", nil)
	assert.Equal(t, "Play episode/301 resume", lastCall(backend.Calls()), "the special sorts first but isn't the first episode")

	target, backend, _ = newTarget(show, special)
	o := newService().FirstEpisode(t.Context(), target, "andor", nil)
	assert.Equal(t, "Behind the Scenes", o.Say.Params["title"], "a show of only specials plays its first special")
	assert.Equal(t, "Play episode/300 resume", lastCall(backend.Calls()))
}

func TestContinueWatchingEdges(t *testing.T) {
	t.Run("nothing watched yet", func(t *testing.T) {
		target, backend, _ := newTarget(media.Item{Kind: media.KindTVShow, ID: 30, Title: "Andor"})

		o := newService().ContinueWatching(t.Context(), target, nil)

		assert.Equal(t, "tv.nothing_to_continue", o.Say.ID)
		assert.Equal(t, []string{"Library tvshow"}, backend.Calls())
	})

	t.Run("everything finished", func(t *testing.T) {
		var items []media.Item

		day := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)

		for i := range lastShows + 2 {
			items = append(items,
				media.Item{Kind: media.KindTVShow, ID: i + 1, Title: fmt.Sprintf("Show %d", i), LastPlayed: day.AddDate(0, 0, i)},
				media.Item{Kind: media.KindEpisode, ID: 100 + i, ShowID: i + 1, PlayCount: 1})
		}

		target, backend, _ := newTarget(items...)

		o := newService().ContinueWatching(t.Context(), target, nil)

		assert.Equal(t, "playback.all_watched", o.Say.ID)
		assert.Equal(t, map[string]string{"show": fmt.Sprintf("Show %d", lastShows+1)}, o.Say.Params, "names the most recently watched show")

		var episodes int

		for _, c := range backend.Calls() {
			if strings.HasPrefix(c, "Episodes") {
				episodes++
			}
		}

		assert.Equal(t, lastShows, episodes, "only the most recent shows are tried")
	})
}

func TestRandomShowEdges(t *testing.T) {
	target, _, _ := newTarget()

	assert.Equal(t, "library.no_genre", seeded().RandomShow(t.Context(), target, "western", nil).Say.ID)

	movies, _, _ := newTarget(media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune"})
	assert.Equal(t, "library.empty", seeded().RandomShow(t.Context(), movies, "", nil).Say.ID)
	shows, _, _ := newTarget(media.Item{Kind: media.KindTVShow, ID: 1, Title: "Andor"})
	assert.Equal(t, "library.empty", seeded().RandomMovie(t.Context(), shows, "", nil).Say.ID)
}

func TestRandomPicksAreSafeConcurrently(t *testing.T) {
	target, _, _ := newTarget()
	s := newService()

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			o := s.RandomMovie(t.Context(), target, "", nil)
			assert.NotNil(t, o.Played)
		})
	}

	wg.Wait()
}
