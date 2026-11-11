package kodi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
)

func melbourne(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Australia/Melbourne")
	require.NoError(t, err)

	return loc
}

func TestLiveTVAvailable(t *testing.T) {
	b, _ := newBackend(t, fakekodi.Options{PVR: fakekodi.AustralianTV(time.Now())}, BackendOptions{})
	ok, err := b.LiveTVAvailable(t.Context())
	require.NoError(t, err)
	assert.True(t, ok)

	b, _ = newBackend(t, fakekodi.Options{}, BackendOptions{})
	ok, err = b.LiveTVAvailable(t.Context())
	require.NoError(t, err)
	assert.False(t, ok, "no PVR client")
}

func TestChannels(t *testing.T) {
	loc := melbourne(t)
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, loc)
	b, s := newBackend(t, fakekodi.Options{PVR: fakekodi.AustralianTV(now)}, BackendOptions{Location: loc})

	channels, err := b.Channels(t.Context(), false)
	require.NoError(t, err)

	names := make([]string, len(channels))
	for i, c := range channels {
		names[i] = c.Number() + " " + c.Name
		assert.Nil(t, c.Now, "no guide asked for")
	}

	assert.Equal(t, []string{
		"1 Seven", "2 7two", "3 7mate", "5 Channel 9", "7 9Go!", "10 ABC TV", "11 10", "12 10 Drama", "30.1 SBS HD", "64 Seinfeld", "73 BBC News",
		"135 SBS", "151 SBS Radio 1", "158 ABC NEWS", "169 BBC News", "173 DW English", "173 Fox Sports News", "176 Fox Sports 503", "990 Late Night Movies",
	}, names, "in number order, hidden channels left out")
	assert.NotContains(t, lastParams(t, s, "PVR.GetChannels"), "broadcastnow")

	locked := channels[len(channels)-1]
	assert.True(t, locked.Locked)
	assert.Equal(t, 6, channels[16].Client, "Fox Sports News comes from the second source")
}

func TestChannelsGuide(t *testing.T) {
	loc := melbourne(t)
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, loc)
	b, s := newBackend(t, fakekodi.Options{PVR: fakekodi.AustralianTV(now)}, BackendOptions{Location: loc})

	channels, err := b.Channels(t.Context(), true)
	require.NoError(t, err)
	assert.Contains(t, lastParams(t, s, "PVR.GetChannels"), `"broadcastnow","broadcastnext"`)

	seven := channels[0]
	require.NotNil(t, seven.Now)
	assert.Equal(t, media.Broadcast{
		Title: "House Of Wellness", Episode: "Episode 23 of Season 11", Genre: "Special Interest",
		Start: now.Add(-10 * time.Minute), End: now.Add(20 * time.Minute),
	}, *seven.Now)
	assert.Equal(t, "21:16", seven.Now.Start.Format("15:04"), "Kodi's UTC times are read in the instance's zone")
	assert.Same(t, loc, seven.Now.Start.Location())
	require.NotNil(t, seven.Next)
	assert.Equal(t, "The Amazing Race", seven.Next.Title)
	assert.True(t, seven.Now.OnAt(now))
	assert.False(t, seven.Next.OnAt(now))

	assert.Nil(t, channels[9].Now, "Seinfeld's channel has no guide")
}

func TestBroadcastGenre(t *testing.T) {
	for name, tc := range map[string]struct{ json, want string }{
		"list, as Kodi sends it":   {`{"title":"NCIS","genre":["Drama","Crime"],"starttime":"2026-11-11 10:00:00","endtime":"2026-11-11 11:00:00"}`, "Drama / Crime"},
		"string, as schema has it": {`{"title":"NCIS","genre":"Drama","starttime":"2026-11-11 10:00:00","endtime":"2026-11-11 11:00:00"}`, "Drama"},
		"absent":                   {`{"title":"NCIS","starttime":"2026-11-11 10:00:00","endtime":"2026-11-11 11:00:00"}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var p pvrBroadcast
			require.NoError(t, json.Unmarshal([]byte(tc.json), &p))

			got := p.broadcast(time.UTC)
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.Genre)
		})
	}

	var p pvrBroadcast
	require.NoError(t, json.Unmarshal([]byte(`{"title":"NCIS","starttime":"","endtime":""}`), &p))
	assert.Nil(t, p.broadcast(time.UTC), "a broadcast without times")
}

func TestChannelsNoLiveTV(t *testing.T) {
	b, _ := newBackend(t, fakekodi.Options{}, BackendOptions{})

	_, err := b.Channels(t.Context(), true)
	require.ErrorIs(t, err, media.ErrNoLiveTV)
	assert.NotErrorIs(t, err, media.ErrNotFound)
}

func TestPlayChannel(t *testing.T) {
	loc := melbourne(t)
	b, s := newBackend(t, fakekodi.Options{PVR: fakekodi.AustralianTV(time.Now())}, BackendOptions{Location: loc})

	require.NoError(t, b.PlayChannel(t.Context(), 3096))
	assert.JSONEq(t, `{"item":{"channelid":3096}}`, lastParams(t, s, "Player.Open"))

	np, err := b.NowPlaying(t.Context())
	require.NoError(t, err)
	require.NotNil(t, np)
	assert.Equal(t, media.KindChannel, np.Item.Kind)
	assert.Equal(t, 3096, np.Item.ID)
	assert.Equal(t, "The World", np.Item.Title)

	require.ErrorIs(t, b.PlayChannel(t.Context(), 1), media.ErrNotFound)
}

func TestShowViewLiveTV(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})

	require.NoError(t, b.ShowView(t.Context(), media.View{Section: media.SectionTVGuide}))
	assert.JSONEq(t, `{"window":"tvguide"}`, lastParams(t, s, "GUI.ActivateWindow"))

	require.NoError(t, b.ShowView(t.Context(), media.View{Section: media.SectionTVChannels}))
	assert.JSONEq(t, `{"window":"tvchannels"}`, lastParams(t, s, "GUI.ActivateWindow"))
}

func TestLiveTVAvailableKodiFails(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{PVR: fakekodi.AustralianTV(time.Now())}, BackendOptions{})
	s.Handle("PVR.GetProperties", func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: fakekodi.CodeInvalidParams, Message: "Invalid params."}
	})

	_, err := b.LiveTVAvailable(t.Context())
	require.Error(t, err)
}

func TestChannelsHiddenByKodi(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{PVR: fakekodi.AustralianTV(time.Now())}, BackendOptions{})
	s.Handle("PVR.GetChannels", func(json.RawMessage) (any, *fakekodi.Error) {
		return map[string]any{"channels": []map[string]any{
			{"channelid": 1, "channel": "Seven", "channelnumber": 71},
			{"channelid": 2, "channel": "Test Card", "channelnumber": 1, "hidden": true},
		}}, nil
	})

	channels, err := b.Channels(t.Context(), false)
	require.NoError(t, err)
	require.Len(t, channels, 1, "a channel Kodi sends though it's hidden is left out")
	assert.Equal(t, "Seven", channels[0].Name)
}

func TestBroadcastGenreMalformed(t *testing.T) {
	for name, genre := range map[string]string{"a list of numbers": `[1, 2]`, "a number": `5`} {
		t.Run(name, func(t *testing.T) {
			var p pvrBroadcast
			require.Error(t, json.Unmarshal([]byte(`{"title":"NCIS","genre":`+genre+`}`), &p))
		})
	}
}
