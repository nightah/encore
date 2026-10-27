package actions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

func TestRecommend(t *testing.T) {
	target, backend, _ := newTarget()
	svc := seeded()

	o := svc.Recommend(t.Context(), target, RecommendRequest{Kinds: []media.Kind{media.KindMovie}}, nil)
	assert.False(t, o.EndSession, "a recommendation is a question")
	require.Len(t, o.Pending, 1)
	require.NotNil(t, o.Followup)
	require.NotNil(t, o.Followup.Recommend)
	assert.Equal(t, "recommend.movie", o.Say.ID)
	first := o.Pending[0]
	assert.Contains(t, []int{3, 4}, first.Ref.ID, "unwatched first")

	// "No" asks again, excluding what was offered.
	o = svc.Recommend(t.Context(), target, *o.Followup.Recommend, o.Pending)
	require.Len(t, o.Pending, 1)
	assert.NotEqual(t, first.Ref, o.Pending[0].Ref)

	// "Yes" plays it.
	played := svc.ChooseFirst(t.Context(), target, o.Pending, nil)
	assert.Equal(t, "playback.playing_movie", played.Say.ID)
	assert.Empty(t, backend.Calls()[:0])
}

func TestRecommendEpisodeAndMusic(t *testing.T) {
	target, _, _ := newTarget()
	svc := seeded()

	o := svc.Recommend(t.Context(), target, RecommendRequest{Kinds: []media.Kind{media.KindEpisode}, Genre: "mystery"}, nil)
	assert.Equal(t, "recommend.episode", o.Say.ID)
	assert.Equal(t, map[string]string{"show": "Severance", "season": "2", "episode": "4"}, o.Say.Params)

	played := svc.ChooseFirst(t.Context(), target, o.Pending, nil)
	assert.Equal(t, "playback.playing_episode", played.Say.ID)
	assert.Equal(t, "Woe's Hollow", played.Say.Params["title"], "the episode's details are fetched to say what's playing")

	o = svc.Recommend(t.Context(), target, RecommendRequest{Kinds: []media.Kind{media.KindAlbum}}, nil)
	assert.Equal(t, "recommend.album", o.Say.ID)
	assert.NotEmpty(t, o.Say.Params["artist"])

	assert.Equal(t, "library.no_genre", svc.Recommend(t.Context(), target, RecommendRequest{Kinds: []media.Kind{media.KindMovie}, Genre: "western"}, nil).Say.ID)

	everything := []Choice{}
	for _, it := range testkit.SampleLibrary() {
		if it.Kind == media.KindArtist {
			everything = append(everything, Choice{Ref: it.Ref()})
		}
	}

	assert.Equal(t, "recommend.nothing", svc.Recommend(t.Context(), target, RecommendRequest{Kinds: []media.Kind{media.KindArtist}}, everything).Say.ID)
}

func TestWhatsNew(t *testing.T) {
	target, _, _ := newTarget()
	svc := newService()
	svc.now = func() time.Time { return time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC) }

	o := svc.WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindMovie})
	assert.Equal(t, "whatsnew.movies", o.Say.ID)
	assert.Equal(t, []string{"Dune: Part Two", "Hot Fuzz"}, o.Say.Lists["titles"], "unwatched, newest first")

	o = svc.WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindMovie, Genre: "comedy"})
	assert.Equal(t, []string{"Hot Fuzz"}, o.Say.Lists["titles"])

	o = svc.WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindTVShow})
	assert.Equal(t, "tv.new_episodes", o.Say.ID)
	assert.Equal(t, map[string]string{"show": "Severance", "count": "2"}, o.Say.Params)

	o = svc.WhatsNew(t.Context(), target, WhatsNewRequest{Show: "severance"})
	assert.Equal(t, "tv.unwatched", o.Say.ID)
	assert.Equal(t, "2", o.Say.Params["count"])

	assert.Equal(t, "tv.no_unwatched", svc.WhatsNew(t.Context(), target, WhatsNewRequest{Show: "bluey"}).Say.ID)

	o = svc.WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindAlbum})
	assert.Equal(t, "whatsnew.albums", o.Say.ID)
	assert.Equal(t, "Innuendo", o.Say.Lists["titles"][0])

	svc.now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	assert.Equal(t, "whatsnew.no_episodes", svc.WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindEpisode}).Say.ID)
}

func TestAlbumsBy(t *testing.T) {
	target, _, _ := newTarget()

	o := newService().AlbumsBy(t.Context(), target, "queen")
	assert.Equal(t, "music.albums_by", o.Say.ID)
	assert.Equal(t, []string{"A Night at the Opera", "Innuendo"}, o.Say.Lists["titles"])
	assert.Equal(t, "Queen", o.Say.Params["artist"])
}
