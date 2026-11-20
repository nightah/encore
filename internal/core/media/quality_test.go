package media_test

import (
	"context"
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

	testCases := []struct {
		name     string
		width    int
		height   int
		expected media.Quality
	}{
		{name: "ShouldBeUnknownWithoutASize"},
		{name: "ShouldRate3840x2160As2160p", width: 3840, height: 2160, expected: media.Quality2160p},
		{name: "ShouldRate3840x1600As2160p", width: 3840, height: 1600, expected: media.Quality2160p},
		{name: "ShouldRate1920x1080As1080p", width: 1920, height: 1080, expected: media.Quality1080p},
		{name: "ShouldRate1920x800As1080p", width: 1920, height: 800, expected: media.Quality1080p},
		{name: "ShouldRate1440x1080As1080p", width: 1440, height: 1080, expected: media.Quality1080p},
		{name: "ShouldRate1280x720As720p", width: 1280, height: 720, expected: media.Quality720p},
		{name: "ShouldRate1280x536As720p", width: 1280, height: 536, expected: media.Quality720p},
		{name: "ShouldRate720x576AsSD", width: 720, height: 576, expected: media.QualitySD},
		{name: "ShouldRateWidthOnlyAsSD", width: 640, expected: media.QualitySD},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, media.QualityOf(tc.width, tc.height))
		})
	}
}

func TestIsPreference(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     media.Quality
		expected bool
	}{
		{name: "ShouldAcceptNoPreference", expected: true},
		{name: "ShouldAcceptHighest", have: media.QualityHighest, expected: true},
		{name: "ShouldAcceptSD", have: media.QualitySD, expected: true},
		{name: "ShouldAccept720p", have: media.Quality720p, expected: true},
		{name: "ShouldAccept1080p", have: media.Quality1080p, expected: true},
		{name: "ShouldAccept2160p", have: media.Quality2160p, expected: true},
		{name: "ShouldReject4k", have: "4k"},
		{name: "ShouldRejectUppercaseHighest", have: "HIGHEST"},
		{name: "ShouldReject480p", have: "480p"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.IsPreference())
		})
	}
}

func TestBestCopy(t *testing.T) {
	t.Parallel()

	sd, hd, fhd, uhd, unknown := media.Copy{Quality: media.QualitySD}, media.Copy{Quality: media.Quality720p}, media.Copy{Quality: media.Quality1080p}, media.Copy{Quality: media.Quality2160p}, media.Copy{}

	testCases := []struct {
		name     string
		prefer   media.Quality
		copies   []media.Copy
		expected int
	}{
		{name: "ShouldFindNoneWithoutCopies", prefer: media.QualityHighest, expected: -1},
		{name: "ShouldTakeTheBestForHighest", prefer: media.QualityHighest, copies: []media.Copy{fhd, uhd, hd}, expected: 1},
		{name: "ShouldTakeTheBestWithoutAPreference", copies: []media.Copy{fhd, uhd}, expected: 1},
		{name: "ShouldTakeThePreferredQuality", prefer: media.Quality1080p, copies: []media.Copy{uhd, fhd, hd}, expected: 1},
		{name: "ShouldElseTakeTheBestBelowIt", prefer: media.Quality1080p, copies: []media.Copy{uhd, sd, hd}, expected: 2},
		{name: "ShouldElseTakeTheLeastAboveIt", prefer: media.Quality720p, copies: []media.Copy{uhd, fhd}, expected: 1},
		{name: "ShouldPutUnknownQualityLast", prefer: media.Quality2160p, copies: []media.Copy{unknown, sd}, expected: 1},
		{name: "ShouldGiveTiesToTheEarliest", prefer: media.Quality1080p, copies: []media.Copy{unknown, unknown}},
		{name: "ShouldPreferQualityOverACopyPartWatched", prefer: media.Quality1080p, copies: []media.Copy{{Quality: media.Quality2160p, Resume: time.Minute}, fhd}, expected: 1},
		{name: "ShouldThenPreferACopyPartWatched", prefer: media.Quality1080p, copies: []media.Copy{fhd, {Quality: media.Quality1080p, Resume: time.Minute}}, expected: 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.expected, media.BestCopy(tc.prefer, tc.copies))
		})
	}
}

func TestOneCopyEach(t *testing.T) {
	t.Parallel()

	ep := func(id, season, episode int, q media.Quality) media.Item {
		return media.Item{Kind: media.KindEpisode, ID: id, ShowID: 7, Season: season, Episode: episode, Quality: q}
	}

	t.Run("ShouldLeaveTheListUnchangedWithoutCopies", func(t *testing.T) {
		t.Parallel()

		eps := []media.Item{ep(1, 1, 1, media.Quality1080p), ep(2, 1, 2, media.Quality2160p)}
		assert.Equal(t, eps, media.OneCopyEach(eps, media.Quality1080p))
	})

	t.Run("ShouldKeepThePreferredCopyWhereTheFirstStood", func(t *testing.T) {
		t.Parallel()

		eps := []media.Item{ep(1, 1, 1, media.Quality1080p), ep(2, 1, 1, media.Quality2160p), ep(3, 1, 2, media.Quality2160p), ep(4, 1, 2, media.Quality1080p), ep(5, 1, 3, "")}

		got := media.OneCopyEach(eps, media.Quality1080p)

		assert.Equal(t, []int{1, 4, 5}, ids(got))
		assert.Equal(t, []int{2, 3, 5}, ids(media.OneCopyEach(eps, media.QualityHighest)))
	})

	t.Run("ShouldTreatAnEpisodeWatchedInAnyCopyAsWatched", func(t *testing.T) {
		t.Parallel()

		watched := ep(1, 1, 1, media.Quality1080p)
		watched.PlayCount = 2

		got := media.OneCopyEach([]media.Item{watched, ep(2, 1, 1, media.Quality2160p)}, media.Quality2160p)

		assert.Equal(t, []int{2}, ids(got))
		assert.Equal(t, 2, got[0].PlayCount)
	})

	t.Run("ShouldKeepThePreferredCopyFromWhereAnotherCopyStopped", func(t *testing.T) {
		t.Parallel()

		started := ep(1, 1, 1, media.Quality1080p)
		started.Resume = 10 * time.Minute

		got := media.OneCopyEach([]media.Item{started, ep(2, 1, 1, media.Quality2160p)}, media.Quality2160p)
		assert.Equal(t, []int{2}, ids(got))
		assert.Equal(t, 10*time.Minute, got[0].Resume)
	})

	t.Run("ShouldNotTreatUnnumberedEpisodesOrOtherShowsAsCopies", func(t *testing.T) {
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

	testCases := []struct {
		name     string
		have     []media.Copy
		expected time.Duration
	}{
		{name: "ShouldBeZeroWithoutCopies"},
		{name: "ShouldTakeTheFurthestCopy", have: []media.Copy{{Resume: time.Minute}, {Resume: 3 * time.Minute}, {}}, expected: 3 * time.Minute},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, media.FurthestResume(tc.have))
		})
	}
}

func TestPreferCopies(t *testing.T) {
	t.Parallel()

	ep := func(id, season, episode int, q media.Quality, played int) media.Item {
		return media.Item{Kind: media.KindEpisode, ID: id, ShowID: 7, Season: season, Episode: episode, Quality: q, PlayCount: played}
	}

	assert.Nil(t, media.PreferCopies(nil, media.Quality1080p, nil))

	t.Run("ShouldListOneCopyOfEachEpisode", func(t *testing.T) {
		t.Parallel()

		b := media.PreferCopies(testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 0), ep(2, 1, 1, media.Quality2160p, 0), ep(3, 1, 2, media.Quality1080p, 0)), media.Quality2160p, nil)

		eps, err := b.Episodes(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, []int{2, 3}, ids(eps))
	})

	t.Run("ShouldPickANextEpisodeUnwatchedInEveryCopy", func(t *testing.T) {
		t.Parallel()

		// The 2160p copy of 1x01 was watched, so the backend's own next episode, its 1080p copy, isn't next.
		b := media.PreferCopies(testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 0), ep(2, 1, 1, media.Quality2160p, 1), ep(3, 1, 2, media.Quality1080p, 0), ep(4, 1, 2, media.Quality2160p, 0)), media.Quality2160p, nil)

		next, err := b.NextEpisode(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, 4, next.ID)
	})

	t.Run("ShouldPutAnEpisodePartWatchedFirstInThePreferredCopyFromWhereTheOtherStopped", func(t *testing.T) {
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

	t.Run("ShouldStartTheFilmAtAPointTheMatchIndexFound", func(t *testing.T) {
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

	t.Run("ShouldPickSpecialsOnceEveryEpisodeIsWatched", func(t *testing.T) {
		t.Parallel()

		b := media.PreferCopies(testkit.NewBackend(ep(1, 0, 1, media.Quality1080p, 0), ep(2, 1, 1, media.Quality1080p, 1), ep(3, 1, 1, media.Quality2160p, 0)), media.Quality2160p, nil)

		next, err := b.NextEpisode(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, 1, next.ID)
	})

	t.Run("ShouldFindNothingWhenEverythingIsWatched", func(t *testing.T) {
		t.Parallel()

		b := media.PreferCopies(testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 1), ep(2, 1, 1, media.Quality2160p, 0)), media.Quality2160p, nil)

		_, err := b.NextEpisode(t.Context(), 7)
		require.ErrorIs(t, err, media.ErrNotFound)
	})

	t.Run("ShouldKeepTheBackendsAnswerForAShowWithoutCopies", func(t *testing.T) {
		t.Parallel()

		kit := testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 0))

		next, err := media.PreferCopies(kit, media.Quality2160p, nil).NextEpisode(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, 1, next.ID)
	})

	t.Run("ShouldKeepTheBackendsAnswerWhenEpisodesCannotBeListed", func(t *testing.T) {
		t.Parallel()

		kit := unlistedEpisodes{testkit.NewBackend(ep(1, 1, 1, media.Quality1080p, 0), ep(2, 1, 1, media.Quality2160p, 0))}

		next, err := media.PreferCopies(kit, media.Quality2160p, nil).NextEpisode(t.Context(), 7)
		require.NoError(t, err)
		assert.Equal(t, 1, next.ID)
	})

	t.Run("ShouldPassOnAFailure", func(t *testing.T) {
		t.Parallel()

		kit := testkit.NewBackend()
		kit.Err = errors.New("offline")

		_, err := media.PreferCopies(kit, media.Quality2160p, nil).NextEpisode(t.Context(), 7)
		require.EqualError(t, err, "offline")
	})

	t.Run("ShouldCarryThePreferenceIntoAShuffle", func(t *testing.T) {
		t.Parallel()

		kit := testkit.NewBackend(media.Item{Kind: media.KindTVShow, ID: 7, Title: "Bluey"})

		require.NoError(t, media.PreferCopies(kit, media.Quality1080p, nil).Play(t.Context(), media.Ref{Kind: media.KindTVShow, ID: 7}, media.PlayOptions{Shuffle: true}))
		assert.Equal(t, []string{"Play tvshow/7 shuffle 1080p"}, kit.Calls())
	})
}

// unlistedEpisodes answers NextEpisode but can't list a show's episodes.
type unlistedEpisodes struct{ *testkit.Backend }

func (unlistedEpisodes) Episodes(context.Context, int) ([]media.Item, error) {
	return nil, errors.New("offline")
}
