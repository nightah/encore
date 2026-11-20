package media

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentOf(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     Kind
		expected Content
	}{
		{name: "ShouldMapMovieToMovies", have: KindMovie, expected: ContentMovies},
		{name: "ShouldMapTVShowToTV", have: KindTVShow, expected: ContentTV},
		{name: "ShouldMapEpisodeToTV", have: KindEpisode, expected: ContentTV},
		{name: "ShouldMapArtistToMusic", have: KindArtist, expected: ContentMusic},
		{name: "ShouldMapAlbumToMusic", have: KindAlbum, expected: ContentMusic},
		{name: "ShouldMapSongToMusic", have: KindSong, expected: ContentMusic},
		{name: "ShouldMapMusicVideoToMusic", have: KindMusicVideo, expected: ContentMusic},
		{name: "ShouldMapPlaylistToNothing", have: KindPlaylist},
		{name: "ShouldMapEmptyKindToNothing"},
		{name: "ShouldMapWrongCaseKindToNothing", have: "Movie"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ContentOf(tc.have))
		})
	}
}

func TestContentsAreEachReachableFromAKind(t *testing.T) {
	t.Parallel()

	reached := map[Content]bool{}

	for _, k := range []Kind{KindMovie, KindTVShow, KindEpisode, KindArtist, KindAlbum, KindSong, KindMusicVideo, KindPlaylist, KindChannel} {
		reached[ContentOf(k)] = true
	}

	for _, c := range Contents {
		assert.True(t, reached[c], "no kind belongs to %q", c)
	}

	assert.Equal(t, []Content{ContentMovies, ContentTV, ContentMusic, ContentLiveTV}, Contents, "the offered order")
	assert.Equal(t, []Content{ContentMovies, ContentTV, ContentMusic}, LibraryContents, "live TV isn't the library")
}

func TestArtworkCoverPrefersThePoster(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     Artwork
		expected string
	}{
		{name: "ShouldPreferThePosterOverTheThumb", have: Artwork{Poster: "image://poster", Thumb: "image://thumb", Fanart: "image://fanart"}, expected: "image://poster"},
		{name: "ShouldUseTheThumbWithoutAPoster", have: Artwork{Thumb: "image://thumb", Fanart: "image://fanart"}, expected: "image://thumb"},
		{name: "ShouldNeverUseTheFanart", have: Artwork{Fanart: "image://fanart"}},
		{name: "ShouldHaveNoCoverWithoutArtwork", have: Artwork{}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.Cover())
		})
	}
}

func TestItemRef(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     Item
		expected Ref
	}{
		{name: "ShouldKeepOnlyTheKindAndID", have: Item{Kind: KindEpisode, ID: 42, ShowID: 7, Title: "Pilot"}, expected: Ref{Kind: KindEpisode, ID: 42}},
		{name: "ShouldBeEmptyForAnEmptyItem", have: Item{}, expected: Ref{}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.Ref())
		})
	}
}

func TestItemInProgress(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     Item
		expected bool
	}{
		{name: "ShouldNotBeInProgressWhenNeverStarted", have: Item{}},
		{name: "ShouldNotBeInProgressWhenWatchedToTheEnd", have: Item{Resume: 0, PlayCount: 3}},
		{name: "ShouldBeInProgressWhenStoppedJustAfterStarting", have: Item{Resume: time.Nanosecond}, expected: true},
		{name: "ShouldBeInProgressWhenPartWatched", have: Item{Resume: 20 * time.Minute, Runtime: time.Hour}, expected: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.InProgress())
		})
	}
}

func TestNowPlayingRemaining(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		position time.Duration
		duration time.Duration
		expected time.Duration
	}{
		{name: "ShouldLeaveTheRestPartWay", position: 10 * time.Minute, duration: time.Hour, expected: 50 * time.Minute},
		{name: "ShouldLeaveAllAtTheStart", duration: time.Hour, expected: time.Hour},
		{name: "ShouldLeaveNothingAtTheEnd", position: time.Hour, duration: time.Hour},
		{name: "ShouldClampToZeroPastTheEnd", position: time.Hour + time.Second, duration: time.Hour},
		{name: "ShouldLeaveNothingWithUnknownDuration", position: 5 * time.Minute},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, NowPlaying{Position: tc.position, Duration: tc.duration}.Remaining())
		})
	}
}

func TestReportCallWithoutAnObserverIsANoop(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() { ReportCall(t.Context(), Call{Method: "Player.Open"}) })
}

func TestReportCallReachesTheObserver(t *testing.T) {
	t.Parallel()

	var got []Call

	ctx := ObserveCalls(t.Context(), func(c Call) { got = append(got, c) })

	errBoom := errors.New("boom")
	ReportCall(ctx, Call{Method: "Player.Open", Duration: time.Millisecond})
	ReportCall(ctx, Call{Method: "Player.Stop", Err: errBoom})

	require.Len(t, got, 2)
	assert.Equal(t, Call{Method: "Player.Open", Duration: time.Millisecond}, got[0])
	assert.Equal(t, "Player.Stop", got[1].Method)
	assert.ErrorIs(t, got[1].Err, errBoom)
}

func TestReportCallReachesTheObserverThroughDerivedContexts(t *testing.T) {
	t.Parallel()

	var got []string

	ctx := ObserveCalls(t.Context(), func(c Call) { got = append(got, c.Method) })
	derived, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()

	ReportCall(derived, Call{Method: "JSONRPC.Ping"})

	assert.Equal(t, []string{"JSONRPC.Ping"}, got)
}

func TestObserveCallsInnerObserverReplacesTheOuter(t *testing.T) {
	t.Parallel()

	var outer, inner []string

	ctx := ObserveCalls(t.Context(), func(c Call) { outer = append(outer, c.Method) })
	ReportCall(ObserveCalls(ctx, func(c Call) { inner = append(inner, c.Method) }), Call{Method: "Input.Up"})
	ReportCall(ctx, Call{Method: "Input.Down"})

	assert.Equal(t, []string{"Input.Up"}, inner)
	assert.Equal(t, []string{"Input.Down"}, outer)
}

func TestReportCallFromSeveralGoroutines(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		count int
	)

	ctx := ObserveCalls(t.Context(), func(Call) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { ReportCall(ctx, Call{Method: "Player.GetItem"}) })
	}

	wg.Wait()
	assert.Equal(t, 16, count)
}

func TestSearchModeIsValid(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     SearchMode
		expected bool
	}{
		{name: "ShouldAcceptEmpty", expected: true},
		{name: "ShouldAcceptGlobal", have: SearchModeGlobal, expected: true},
		{name: "ShouldAcceptSkin", have: SearchModeSkin, expected: true},
		{name: "ShouldRejectNative", have: "native"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.IsValid())
		})
	}
}
