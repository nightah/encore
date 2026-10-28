package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

func TestOnPlayerChangeHearsPlaybackNotifications(t *testing.T) {
	h := newHarness(t, true)
	id := h.instance(t).id

	heard := make(chan int64, 16)
	h.app.OnPlayerChange(func(instanceID int64) { heard <- instanceID })

	connected(t, h.kodi)

	for _, method := range []string{"Player.OnPlay", "Player.OnPause", "Player.OnResume", "Player.OnStop", "Player.OnSeek", "Player.OnSpeedChanged", "Application.OnVolumeChanged"} {
		// A library notification first, which isn't about playback; notifications arrive in order.
		h.kodi.Notify("VideoLibrary.OnScanStarted", nil)
		h.kodi.Notify(method, map[string]any{"item": map[string]any{"type": "movie", "id": 6}, "player": map[string]any{"playerid": 1}})

		select {
		case got := <-heard:
			assert.Equal(t, id, got, method)
		case <-time.After(eventually):
			t.Fatalf("%s wasn't heard", method)
		}

		assert.Empty(t, heard, "only playback changes are reported")
	}
}

func TestPlayerChangeWithoutAListener(t *testing.T) {
	a := newBareApp(t)

	assert.NotPanics(t, func() { a.playerChanged(1) })

	var got []int64

	a.OnPlayerChange(func(id int64) { got = append(got, id) })
	a.playerChanged(7)
	a.OnPlayerChange(nil)
	a.playerChanged(8)

	assert.Equal(t, []int64{7}, got)
}

func TestObserveOnlyWatchesSharedDetectedGroups(t *testing.T) {
	notification := func(method kodirpc.NotificationName, data string) kodi.Notification {
		return kodi.Notification{Method: method, Params: []byte(`{"sender":"xbmc","data":` + data + `}`)}
	}

	a := &App{}
	lounge, bedroom := &Instance{id: 1, name: "Lounge"}, &Instance{id: 2, name: "Bedroom"}
	g := &group{members: []*Instance{lounge, bedroom}}
	lounge.group, bedroom.group = g, g

	solo := &Instance{id: 3, name: "Study"}
	solo.group = &group{members: []*Instance{solo}}

	manual := &Instance{id: 4, name: "Kitchen"}
	manual.group = &group{members: []*Instance{manual, lounge}}
	manual.group.rec.Manual = true

	for _, c := range []struct {
		name string
		inst *Instance
		n    kodi.Notification
	}{
		{"other method", lounge, notification(kodirpc.NotificationPlayerOnPlay, `{"item":{"type":"movie","id":6}}`)},
		{"bad params", lounge, kodi.Notification{Method: kodirpc.NotificationVideoLibraryOnUpdate, Params: []byte(`not json`)}},
		{"song", lounge, notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"song","id":6}`)},
		{"no id", lounge, notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"movie"}`)},
		{"solo", solo, notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"movie","id":6}`)},
		{"manual", manual, notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"movie","id":6}`)},
		{"no group", &Instance{id: 5}, notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"movie","id":6}`)},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, ok := a.observe(c.inst, c.n)
			assert.False(t, ok)
		})
	}
}

func TestVetoed(t *testing.T) {
	lounge, bedroom, study := &Instance{id: 1}, &Instance{id: 2}, &Instance{id: 3}

	g := &group{members: []*Instance{lounge}}
	h := &group{members: []*Instance{bedroom, study}}
	assert.False(t, vetoed(lounge, g, h))

	h.ev.Vetoed = []int64{lounge.id}
	assert.True(t, vetoed(lounge, g, h), "h was split from lounge before")

	h.ev.Vetoed = nil
	g.ev.Vetoed = []int64{study.id}
	assert.True(t, vetoed(lounge, g, h), "lounge was split from one of h's members before")
}

func TestObservationMerge(t *testing.T) {
	one, two := 1, 2

	first := observation{playCount: &one, havePrior: true}
	first.prior.Title = "before"

	later := observation{playCount: &two, havePrior: true}
	later.prior.Title = "between"

	merged := first.merge(&later)
	assert.Equal(t, &two, merged.playCount, "the newest play count")
	assert.Equal(t, "before", merged.prior.Title, "the oldest prior state")

	merged = (&observation{}).merge(&later)
	assert.True(t, merged.havePrior)
	assert.Equal(t, "between", merged.prior.Title)

	merged = first.merge(&observation{})
	assert.Equal(t, &one, merged.playCount, "a later change without a play count keeps the one seen")
}
