package actions

import (
	"fmt"
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

func TestAlbumsByEdges(t *testing.T) {
	var albums []media.Item

	for i := range 12 {
		albums = append(albums, media.Item{Kind: media.KindAlbum, ID: 100 + i, Title: fmt.Sprintf("Album %02d", 12-i), Artist: "Prince", Year: 1978 + i})
	}

	target, _, _ := newTarget(append(albums,
		media.Item{Kind: media.KindArtist, ID: 1, Title: "Prince"},
		media.Item{Kind: media.KindArtist, ID: 2, Title: "Kate Bush"},
	)...)
	svc := newService()

	o := svc.AlbumsBy(t.Context(), target, "prince")
	assert.Equal(t, "music.albums_by_more", o.Say.ID, "more than ten albums says there are more")
	require.Len(t, o.Say.Lists["titles"], 10)
	assert.Equal(t, "Album 12", o.Say.Lists["titles"][0], "oldest first")
	assert.Equal(t, "Album 03", o.Say.Lists["titles"][9])

	o = svc.AlbumsBy(t.Context(), target, "kate bush")
	assert.Equal(t, "music.no_albums_by", o.Say.ID)
	assert.Equal(t, "Kate Bush", o.Say.Params["artist"])

	o = svc.AlbumsBy(t.Context(), target, "abba")
	assert.Equal(t, "match.not_found", o.Say.ID)
	assert.Equal(t, "abba", o.Say.Params["query"])
}

func TestRecommendEachKind(t *testing.T) {
	for _, tc := range []struct {
		kind   media.Kind
		id     string
		params map[string]string
	}{
		{media.KindTVShow, "recommend.show", map[string]string{"title": "Severance"}},
		{media.KindArtist, "recommend.artist", nil},
		{media.KindSong, "recommend.song", nil},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			target, backend, _ := newTarget()

			o := seeded().Recommend(t.Context(), target, RecommendRequest{Kinds: []media.Kind{tc.kind}}, nil)

			assert.Equal(t, tc.id, o.Say.ID)
			require.Len(t, o.Pending, 1)
			assert.Equal(t, tc.kind, o.Pending[0].Ref.Kind)
			assert.Equal(t, o.Say.Params["title"], o.Pending[0].Title, "the question names the offered title")

			if tc.params != nil {
				assert.Equal(t, tc.params, o.Say.Params, "unwatched shows come first")
			}

			if tc.kind == media.KindSong {
				assert.NotEmpty(t, o.Say.Params["artist"])
			}

			assert.Equal(t, []string{"Library " + string(tc.kind)}, backend.Calls(), "the library is read, nothing plays")
		})
	}
}

func TestRecommendSkipsKindsWithoutTheGenre(t *testing.T) {
	target, _, _ := newTarget()

	for seed := range uint64(8) {
		svc := newService()
		svc.SetSeed(seed)

		o := svc.Recommend(t.Context(), target, RecommendRequest{Kinds: []media.Kind{media.KindMovie, media.KindTVShow, media.KindSong}, Genre: "pop"}, nil)

		assert.Equal(t, "recommend.song", o.Say.ID, "seed %d", seed)
		assert.Equal(t, "Come Together", o.Say.Params["title"], "seed %d", seed)
		require.NotNil(t, o.Followup)
		assert.Equal(t, "pop", o.Followup.Recommend.Genre, "the request is kept for another suggestion")
	}
}

func TestRecommendSkipsKindsWithNothingLeft(t *testing.T) {
	target, _, _ := newTarget()
	shown := []Choice{{Ref: media.Ref{Kind: media.KindTVShow, ID: 10}}, {Ref: media.Ref{Kind: media.KindTVShow, ID: 20}}}

	o := seeded().Recommend(t.Context(), target, RecommendRequest{Kinds: []media.Kind{media.KindTVShow, media.KindMovie}}, shown)

	assert.Equal(t, "recommend.movie", o.Say.ID, "every show was offered, so a movie is")
}

func numbered(kind media.Kind, n int, added time.Time) []media.Item {
	items := make([]media.Item, n)
	for i := range items {
		items[i] = media.Item{Kind: kind, ID: 500 + i, Title: fmt.Sprintf("%s %d", kind, i), DateAdded: added.Add(time.Duration(i) * time.Hour)}
	}

	return items
}

func TestWhatsNewLists(t *testing.T) {
	added := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	t.Run("more than five movies", func(t *testing.T) {
		target, _, _ := newTarget(numbered(media.KindMovie, 7, added)...)

		o := newService().WhatsNew(t.Context(), target, WhatsNewRequest{})

		assert.Equal(t, "whatsnew.movies_more", o.Say.ID)
		assert.Equal(t, []string{"movie 6", "movie 5", "movie 4", "movie 3", "movie 2"}, o.Say.Lists["titles"])
	})

	t.Run("exactly five movies", func(t *testing.T) {
		target, _, _ := newTarget(numbered(media.KindMovie, 5, added)...)

		o := newService().WhatsNew(t.Context(), target, WhatsNewRequest{})

		assert.Equal(t, "whatsnew.movies", o.Say.ID)
		assert.Len(t, o.Say.Lists["titles"], 5)
	})

	t.Run("everything watched", func(t *testing.T) {
		movies := numbered(media.KindMovie, 2, added)
		for i := range movies {
			movies[i].PlayCount = 1
		}

		target, _, _ := newTarget(movies...)

		o := newService().WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindMovie})

		assert.Equal(t, "whatsnew.nothing", o.Say.ID)
		assert.Empty(t, o.Say.Lists)
	})

	t.Run("music videos", func(t *testing.T) {
		target, _, _ := newTarget()

		o := newService().WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindMusicVideo})

		assert.Equal(t, "whatsnew.music_videos", o.Say.ID)
		assert.Equal(t, []string{"Bohemian Rhapsody"}, o.Say.Lists["titles"])
	})

	t.Run("unknown genre", func(t *testing.T) {
		target, _, _ := newTarget()

		o := newService().WhatsNew(t.Context(), target, WhatsNewRequest{Genre: "western"})

		assert.Equal(t, "library.no_genre", o.Say.ID)
	})

	t.Run("no albums", func(t *testing.T) {
		target, _, _ := newTarget(numbered(media.KindMovie, 1, added)...)

		assert.Equal(t, "whatsnew.no_albums", newService().WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindSong}).Say.ID)
	})

	t.Run("more than five albums", func(t *testing.T) {
		target, _, _ := newTarget(numbered(media.KindAlbum, 6, added)...)

		o := newService().WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindArtist})

		assert.Equal(t, "whatsnew.albums_more", o.Say.ID)
		assert.Equal(t, []string{"album 5", "album 4", "album 3", "album 2", "album 1"}, o.Say.Lists["titles"])
	})
}

func TestWhatsNewEpisodes(t *testing.T) {
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	recent := now.Add(-24 * time.Hour)

	episode := func(id, show int, title string, playCount int, added time.Time) media.Item {
		return media.Item{Kind: media.KindEpisode, ID: id, ShowID: show, ShowTitle: title, PlayCount: playCount, DateAdded: added}
	}

	for _, tc := range []struct {
		name   string
		eps    []media.Item
		id     string
		params map[string]string
		list   []string
	}{
		{"one new episode", []media.Item{episode(1, 1, "Slow Horses", 0, recent), episode(2, 1, "Slow Horses", 1, recent)}, "tv.one_new_episode", map[string]string{"show": "Slow Horses"}, nil},
		{"several shows", []media.Item{episode(1, 1, "Slow Horses", 0, recent), episode(2, 2, "Bluey", 0, recent.Add(time.Hour)), episode(3, 1, "Slow Horses", 0, recent)}, "whatsnew.shows", nil, []string{"Bluey", "Slow Horses"}},
		{"many shows", []media.Item{
			episode(1, 1, "A", 0, recent), episode(2, 2, "B", 0, recent), episode(3, 3, "C", 0, recent),
			episode(4, 4, "D", 0, recent), episode(5, 5, "E", 0, recent), episode(6, 6, "F", 0, recent),
		}, "whatsnew.shows_more", nil, nil},
		{"only old episodes", []media.Item{episode(1, 1, "Slow Horses", 0, now.Add(-31*24*time.Hour))}, "whatsnew.no_episodes", nil, nil},
		{"only watched episodes", []media.Item{episode(1, 1, "Slow Horses", 3, recent)}, "whatsnew.no_episodes", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, _, _ := newTarget(tc.eps...)
			svc := newService()
			svc.now = func() time.Time { return now }

			o := svc.WhatsNew(t.Context(), target, WhatsNewRequest{Kind: media.KindEpisode})

			assert.Equal(t, tc.id, o.Say.ID)

			if tc.params != nil {
				assert.Equal(t, tc.params, o.Say.Params)
			}

			if tc.list != nil {
				assert.Equal(t, tc.list, o.Say.Lists["titles"], "newest first, each show once")
			}

			if tc.id == "whatsnew.shows_more" {
				assert.Len(t, o.Say.Lists["titles"], whatsNewMax)
			}
		})
	}
}

func TestWhatsNewForOneShow(t *testing.T) {
	items := append(testkit.SampleLibrary(), media.Item{Kind: media.KindTVShow, ID: 30, Title: "Andor"},
		media.Item{Kind: media.KindEpisode, ID: 301, ShowID: 30, ShowTitle: "Andor", Season: 1, Episode: 1, PlayCount: 1},
		media.Item{Kind: media.KindEpisode, ID: 302, ShowID: 30, ShowTitle: "Andor", Season: 1, Episode: 2})
	target, _, _ := newTarget(items...)
	svc := newService()

	o := svc.WhatsNew(t.Context(), target, WhatsNewRequest{Show: "andor"})
	assert.Equal(t, "tv.one_unwatched", o.Say.ID)
	assert.Equal(t, map[string]string{"show": "Andor"}, o.Say.Params)

	o = svc.WhatsNew(t.Context(), target, WhatsNewRequest{Show: "the bear"})
	assert.Equal(t, "match.not_found", o.Say.ID)
}
