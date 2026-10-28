package fakekodi

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func notifications(t *testing.T, s *Server) <-chan string {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	conn := dial(t, s)

	out := make(chan string, 16)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}

			var msg struct {
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Method != "" {
				out <- msg.Method
			}
		}
	}()

	t.Cleanup(func() {
		cancel()
		_ = conn.CloseNow()
		<-done
	})

	return out
}

func TestSharedLibrary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lib, n := DefaultLibrary(), NewNetwork(t)
		a, b := n.New(Options{Library: lib}), n.New(Options{Library: lib})
		fromA, fromB := notifications(t, a), notifications(t, b)

		played := time.Date(2026, 10, 26, 21, 30, 0, 0, time.Local)
		require.True(t, a.SetPlayState("movie", 3, PlayState{PlayCount: 1, LastPlayed: played}))
		assert.False(t, a.SetPlayState("movie", 999, PlayState{}))

		// The change is visible through both servers.
		for _, s := range []*Server{a, b} {
			_, resp := post(t, s, "", "", "VideoLibrary.GetMovieDetails", map[string]any{"movieid": 3})
			require.Nil(t, resp.Error)
			assert.Contains(t, string(resp.Result), `"playcount":1`)
			assert.Contains(t, string(resp.Result), `"lastplayed":"2026-10-26 21:30:00"`)
			assert.Contains(t, string(resp.Result), `"position":0`)
		}

		// Only the server that made the change says so.
		synctest.Wait()
		assert.Equal(t, "Player.OnStop", <-fromA)
		assert.Equal(t, "VideoLibrary.OnUpdate", <-fromA)
		assert.Empty(t, fromB, "the other server notifies nothing")

		// Updates through either server reach the shared library.
		b.Update(func(l *Library) { l.Episodes[0].PlayCount = 7 })

		_, resp := post(t, a, "", "", "VideoLibrary.GetEpisodeDetails", map[string]any{"episodeid": 101})
		assert.Contains(t, string(resp.Result), `"playcount":7`)
	})
}

func TestSetPlayStateAnnouncesPlayCountOnlyWhenChanged(t *testing.T) {
	s := New(t, Options{})
	conn := dial(t, s)

	read := func() string {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		_, data, err := conn.Read(ctx)
		require.NoError(t, err)

		return string(data)
	}

	// Episode 102 is unwatched; saving a resume point leaves the play count alone.
	require.True(t, s.SetPlayState("episode", 102, PlayState{Resume: 600}))
	assert.Contains(t, read(), `"Player.OnStop"`)
	assert.NotContains(t, read(), "playcount")

	require.True(t, s.SetPlayState("episode", 102, PlayState{PlayCount: 1}))
	assert.Contains(t, read(), `"Player.OnStop"`)
	assert.Contains(t, read(), `"playcount":1`)
}

func TestDatabaseName(t *testing.T) {
	s := New(t, Options{Version: [3]int{13, 1000, 0}, DatabaseName: "MyVideos149"})

	_, resp := post(t, s, "", "", "Database.GetDatabaseName", map[string]any{"type": "videos"})
	require.Nil(t, resp.Error)
	assert.JSONEq(t, `"MyVideos149"`, string(resp.Result))

	_, resp = post(t, New(t, Options{}), "", "", "Database.GetDatabaseName", map[string]any{"type": "videos"})
	require.NotNil(t, resp.Error, "Kodi 21 doesn't have the method")
	assert.Equal(t, CodeMethodNotFound, resp.Error.Code)
}
