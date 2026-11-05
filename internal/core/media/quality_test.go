package media_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

func TestQualityOf(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		width, height int
		want          media.Quality
	}{
		{0, 0, ""},
		{3840, 2160, media.Quality2160p},
		{3840, 1600, media.Quality2160p},
		{1920, 1080, media.Quality1080p},
		{1920, 800, media.Quality1080p},
		{1440, 1080, media.Quality1080p},
		{1280, 720, media.Quality720p},
		{1280, 536, media.Quality720p},
		{720, 576, media.QualitySD},
		{640, 0, media.QualitySD},
	} {
		assert.Equal(t, tc.want, media.QualityOf(tc.width, tc.height), "%dx%d", tc.width, tc.height)
	}
}

func TestIsPreference(t *testing.T) {
	t.Parallel()

	for _, q := range []media.Quality{"", media.QualityHighest, media.QualitySD, media.Quality720p, media.Quality1080p, media.Quality2160p} {
		assert.True(t, q.IsPreference(), q)
	}

	for _, q := range []media.Quality{"4k", "HIGHEST", "480p"} {
		assert.False(t, q.IsPreference(), q)
	}
}

func TestBestCopy(t *testing.T) {
	t.Parallel()

	sd, hd, fhd, uhd, unknown := media.Copy{Quality: media.QualitySD}, media.Copy{Quality: media.Quality720p}, media.Copy{Quality: media.Quality1080p}, media.Copy{Quality: media.Quality2160p}, media.Copy{}

	for name, tc := range map[string]struct {
		prefer media.Quality
		copies []media.Copy
		want   int
	}{
		"none":                                  {media.QualityHighest, nil, -1},
		"highest takes the best":                {media.QualityHighest, []media.Copy{fhd, uhd, hd}, 1},
		"no preference takes the best":          {"", []media.Copy{fhd, uhd}, 1},
		"the preferred quality":                 {media.Quality1080p, []media.Copy{uhd, fhd, hd}, 1},
		"else the best below it":                {media.Quality1080p, []media.Copy{uhd, sd, hd}, 2},
		"else the least above it":               {media.Quality720p, []media.Copy{uhd, fhd}, 1},
		"unknown quality comes last":            {media.Quality2160p, []media.Copy{unknown, sd}, 1},
		"ties go to the earliest":               {media.Quality1080p, []media.Copy{unknown, unknown}, 0},
		"quality wins over a copy part watched": {media.Quality1080p, []media.Copy{{Quality: media.Quality2160p, Resume: time.Minute}, fhd}, 1},
		"then a copy part watched":              {media.Quality1080p, []media.Copy{fhd, {Quality: media.Quality1080p, Resume: time.Minute}}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, media.BestCopy(tc.prefer, tc.copies))
		})
	}
}

func TestOneCopyEach(t *testing.T) {
	t.Parallel()

	ep := func(id, season, episode int, q media.Quality) media.Item {
		return media.Item{Kind: media.KindEpisode, ID: id, ShowID: 7, Season: season, Episode: episode, Quality: q}
	}

	t.Run("without copies the list is unchanged", func(t *testing.T) {
		t.Parallel()

		eps := []media.Item{ep(1, 1, 1, media.Quality1080p), ep(2, 1, 2, media.Quality2160p)}
		assert.Equal(t, eps, media.OneCopyEach(eps, media.Quality1080p))
	})

	t.Run("keeps the preferred copy where the first stood", func(t *testing.T) {
		t.Parallel()

		eps := []media.Item{ep(1, 1, 1, media.Quality1080p), ep(2, 1, 1, media.Quality2160p), ep(3, 1, 2, media.Quality2160p), ep(4, 1, 2, media.Quality1080p), ep(5, 1, 3, "")}

		got := media.OneCopyEach(eps, media.Quality1080p)

		assert.Equal(t, []int{1, 4, 5}, ids(got))
		assert.Equal(t, []int{2, 3, 5}, ids(media.OneCopyEach(eps, media.QualityHighest)))
	})

	t.Run("an episode watched in any copy is watched", func(t *testing.T) {
		t.Parallel()

		watched := ep(1, 1, 1, media.Quality1080p)
		watched.PlayCount = 2

		got := media.OneCopyEach([]media.Item{watched, ep(2, 1, 1, media.Quality2160p)}, media.Quality2160p)

		assert.Equal(t, []int{2}, ids(got))
		assert.Equal(t, 2, got[0].PlayCount)
	})

	t.Run("the preferred copy is kept, from where another copy stopped", func(t *testing.T) {
		t.Parallel()

		started := ep(1, 1, 1, media.Quality1080p)
		started.Resume = 10 * time.Minute

		got := media.OneCopyEach([]media.Item{started, ep(2, 1, 1, media.Quality2160p)}, media.Quality2160p)
		assert.Equal(t, []int{2}, ids(got))
		assert.Equal(t, 10*time.Minute, got[0].Resume)
	})

	t.Run("unnumbered episodes and other shows are not copies", func(t *testing.T) {
		t.Parallel()

		other := ep(3, 1, 1, media.Quality1080p)
		other.ShowID = 8

		eps := []media.Item{ep(1, 0, 0, ""), ep(2, 0, 0, ""), other, ep(4, 1, 1, media.Quality2160p)}

		assert.Equal(t, []int{1, 2, 3, 4}, ids(media.OneCopyEach(eps, media.QualityHighest)))
	})
}

func ids(items []media.Item) []int {
	out := make([]int, len(items))
	for i, it := range items {
		out[i] = it.ID
	}

	return out
}

func TestFurthestResume(t *testing.T) {
	t.Parallel()

	assert.Zero(t, media.FurthestResume(nil))
	assert.Equal(t, 3*time.Minute, media.FurthestResume([]media.Copy{{Resume: time.Minute}, {Resume: 3 * time.Minute}, {}}))
}

func TestPreferCopies(t *testing.T) {
	t.Parallel()

	ep := func(id, season, episode int, q media.Quality, played int) media.Item {
		return media.Item{Kind: media.KindEpisode, ID: id, ShowID: 7, Season: season, Episode: episode, Quality: q, PlayCount: played}
	}

	assert.Nil(t, media.PreferCopies(nil, media.Quality1080p, nil))

	t.Run("episodes", func(t *testing.T) {
		t.Parallel()

		b := media.PreferCopies(testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 0), ep(2, 1, 1, media.Quality2160p, 0), ep(3, 1, 2, media.Quality1080p, 0)), media.Quality2160p, nil)

		eps, err := b.Episodes(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, []int{2, 3}, ids(eps))
	})

	t.Run("the next episode is unwatched in every copy", func(t *testing.T) {
		t.Parallel()

		// The 2160p copy of 1x01 was watched, so the backend's own next episode, its 1080p copy, isn't next.
		b := media.PreferCopies(testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 0), ep(2, 1, 1, media.Quality2160p, 1), ep(3, 1, 2, media.Quality1080p, 0), ep(4, 1, 2, media.Quality2160p, 0)), media.Quality2160p, nil)

		next, err := b.NextEpisode(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, 4, next.ID)
	})

	t.Run("an episode part watched comes first, in the preferred copy from where the other stopped", func(t *testing.T) {
		t.Parallel()

		started := ep(3, 1, 2, media.Quality1080p, 0)
		started.Resume = time.Minute

		kit := testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 0), ep(2, 1, 1, media.Quality2160p, 0), started, ep(4, 1, 2, media.Quality2160p, 0))
		b := media.PreferCopies(kit, media.Quality2160p, nil)

		next, err := b.NextEpisode(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, 4, next.ID)
		assert.Equal(t, time.Minute, next.Resume)

		require.NoError(t, b.Play(t.Context(), next.Ref(), media.PlayOptions{Resume: true}))
		assert.Contains(t, kit.Calls(), "Play episode/4 resume from 1m0s 2160p", "the 2160p copy starts where the 1080p one stopped")
	})

	t.Run("a point the match index found starts the film there", func(t *testing.T) {
		t.Parallel()

		kit := testkit.NewBackend(media.Item{Kind: media.KindMovie, ID: 3, Title: "Dune"})
		points := &media.ResumePoints{}
		points.Set(media.Ref{Kind: media.KindMovie, ID: 3}, 5*time.Minute)
		points.Set(media.Ref{Kind: media.KindMovie, ID: 3}, 2*time.Minute)

		b := media.PreferCopies(kit, media.Quality1080p, points)
		require.NoError(t, b.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 3}, media.PlayOptions{Resume: true}))
		require.NoError(t, b.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 3}, media.PlayOptions{}))
		assert.Equal(t, []string{"Play movie/3 resume from 5m0s 1080p", "Play movie/3 1080p"}, kit.Calls(), "the furthest point, and only when resuming")
	})

	t.Run("specials once every episode is watched", func(t *testing.T) {
		t.Parallel()

		b := media.PreferCopies(testkit.NewBackend(ep(1, 0, 1, media.Quality1080p, 0), ep(2, 1, 1, media.Quality1080p, 1), ep(3, 1, 1, media.Quality2160p, 0)), media.Quality2160p, nil)

		next, err := b.NextEpisode(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, 1, next.ID)
	})

	t.Run("nothing left", func(t *testing.T) {
		t.Parallel()

		b := media.PreferCopies(testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 1), ep(2, 1, 1, media.Quality2160p, 0)), media.Quality2160p, nil)

		_, err := b.NextEpisode(t.Context(), 7)
		require.ErrorIs(t, err, media.ErrNotFound)
	})

	t.Run("a show without copies keeps the backend's answer", func(t *testing.T) {
		t.Parallel()

		kit := testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 0))

		next, err := media.PreferCopies(kit, media.Quality2160p, nil).NextEpisode(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, 1, next.ID)
	})

	t.Run("a failure is passed on", func(t *testing.T) {
		t.Parallel()

		kit := testkit.NewBackend()
		kit.Err = errors.New("offline")

		_, err := media.PreferCopies(kit, media.Quality2160p, nil).NextEpisode(t.Context(), 7)
		require.EqualError(t, err, "offline")
	})

	t.Run("a shuffle carries the preference", func(t *testing.T) {
		t.Parallel()

		kit := testkit.NewBackend(media.Item{Kind: media.KindTVShow, ID: 7, Title: "Bluey"})

		require.NoError(t, media.PreferCopies(kit, media.Quality1080p, nil).Play(t.Context(), media.Ref{Kind: media.KindTVShow, ID: 7}, media.PlayOptions{Shuffle: true}))
		assert.Equal(t, []string{"Play tvshow/7 shuffle 1080p"}, kit.Calls())
	})
}
