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

	for _, tc := range []struct {
		kind Kind
		want Content
	}{
		{KindMovie, ContentMovies},
		{KindTVShow, ContentTV},
		{KindEpisode, ContentTV},
		{KindArtist, ContentMusic},
		{KindAlbum, ContentMusic},
		{KindSong, ContentMusic},
		{KindMusicVideo, ContentMusic},
		{KindPlaylist, ""},
		{"", ""},
		{"Movie", ""},
	} {
		assert.Equal(t, tc.want, ContentOf(tc.kind), "kind %q", tc.kind)
	}
}

func TestContentsAreEachReachableFromAKind(t *testing.T) {
	t.Parallel()

	reached := map[Content]bool{}

	for _, k := range []Kind{KindMovie, KindTVShow, KindEpisode, KindArtist, KindAlbum, KindSong, KindMusicVideo, KindPlaylist} {
		reached[ContentOf(k)] = true
	}

	for _, c := range Contents {
		assert.True(t, reached[c], "no kind belongs to %q", c)
	}

	assert.Equal(t, []Content{ContentMovies, ContentTV, ContentMusic}, Contents, "the offered order")
}

func TestArtworkCoverPrefersThePoster(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		art  Artwork
		want string
	}{
		{"poster and thumb", Artwork{Poster: "image://poster", Thumb: "image://thumb", Fanart: "image://fanart"}, "image://poster"},
		{"thumb only", Artwork{Thumb: "image://thumb", Fanart: "image://fanart"}, "image://thumb"},
		{"fanart is never the cover", Artwork{Fanart: "image://fanart"}, ""},
		{"none", Artwork{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.art.Cover())
		})
	}
}

func TestItemRef(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Ref{Kind: KindEpisode, ID: 42}, Item{Kind: KindEpisode, ID: 42, ShowID: 7, Title: "Pilot"}.Ref())
	assert.Equal(t, Ref{}, Item{}.Ref())
}

func TestItemInProgress(t *testing.T) {
	t.Parallel()

	assert.False(t, Item{}.InProgress(), "never started")
	assert.False(t, Item{Resume: 0, PlayCount: 3}.InProgress(), "watched to the end")
	assert.True(t, Item{Resume: time.Nanosecond}.InProgress(), "stopped just after starting")
	assert.True(t, Item{Resume: 20 * time.Minute, Runtime: time.Hour}.InProgress())
}

func TestNowPlayingRemaining(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		position, duration time.Duration
		want               time.Duration
	}{
		{"part way", 10 * time.Minute, time.Hour, 50 * time.Minute},
		{"at the start", 0, time.Hour, time.Hour},
		{"at the end", time.Hour, time.Hour, 0},
		{"past the end clamps to zero", time.Hour + time.Second, time.Hour, 0},
		{"unknown duration", 5 * time.Minute, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NowPlaying{Position: tc.position, Duration: tc.duration}.Remaining())
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
