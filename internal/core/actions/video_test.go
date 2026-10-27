package actions

import (
	"sync"
	"testing"

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
