package app

import (
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

func TestOnPlayerChangeHearsPlaybackNotifications(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		id := h.instance(t).id

		heard := make(chan int64, 16)
		h.app.OnPlayerChange(func(instanceID int64) { heard <- instanceID })

		connected(t, h.kodi)

		for _, method := range []string{"Player.OnPlay", "Player.OnPause", "Player.OnResume", "Player.OnStop", "Player.OnSeek", "Player.OnSpeedChanged", "Application.OnVolumeChanged"} {
			h.kodi.Notify("VideoLibrary.OnScanStarted", nil)
			h.kodi.Notify(method, map[string]any{"item": map[string]any{"type": "movie", "id": 6}, "player": map[string]any{"playerid": 1}})
			synctest.Wait()

			require.Len(t, heard, 1, "%s is heard, and the library notification isn't", method)
			assert.Equal(t, id, <-heard, method)
		}
	})
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

	testCases := []struct {
		name string
		inst *Instance
		have kodi.Notification
	}{
		{name: "ShouldIgnoreAnotherMethod", inst: lounge, have: notification(kodirpc.NotificationPlayerOnPlay, `{"item":{"type":"movie","id":6}}`)},
		{name: "ShouldIgnoreBadParams", inst: lounge, have: kodi.Notification{Method: kodirpc.NotificationVideoLibraryOnUpdate, Params: []byte(`not json`)}},
		{name: "ShouldIgnoreASong", inst: lounge, have: notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"song","id":6}`)},
		{name: "ShouldIgnoreAnItemWithoutAnID", inst: lounge, have: notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"movie"}`)},
		{name: "ShouldIgnoreASoloGroup", inst: solo, have: notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"movie","id":6}`)},
		{name: "ShouldIgnoreAManualGroup", inst: manual, have: notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"movie","id":6}`)},
		{name: "ShouldIgnoreAnInstanceWithoutAGroup", inst: &Instance{id: 5}, have: notification(kodirpc.NotificationVideoLibraryOnUpdate, `{"type":"movie","id":6}`)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := a.observe(tc.inst, tc.have)
			assert.False(t, ok)
		})
	}
}

func TestVetoed(t *testing.T) {
	lounge, bedroom, study := &Instance{id: 1}, &Instance{id: 2}, &Instance{id: 3}

	testCases := []struct {
		name     string
		gVetoed  []int64
		hVetoed  []int64
		expected bool
	}{
		{name: "ShouldNotVetoWithoutEarlierSplits"},
		{name: "ShouldVetoWhenHWasSplitFromLoungeBefore", hVetoed: []int64{lounge.id}, expected: true},
		{name: "ShouldVetoWhenLoungeWasSplitFromOneOfHsMembersBefore", gVetoed: []int64{study.id}, expected: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := &group{members: []*Instance{lounge}}
			h := &group{members: []*Instance{bedroom, study}}
			g.ev.Vetoed, h.ev.Vetoed = tc.gVetoed, tc.hVetoed

			assert.Equal(t, tc.expected, vetoed(lounge, g, h))
		})
	}
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

func TestASampleTooSmallToTellKeepsAMemberInItsGroup(t *testing.T) {
	small := func(id int) *kodi.LibraryFingerprint {
		return &kodi.LibraryFingerprint{Movies: []kodi.FingerprintItem{{ID: id, File: fmt.Sprintf("nfs://nas/movies/%d.mkv", id), DateAdded: "2026-01-01 10:00:00"}}}
	}

	testCases := []struct {
		name   string
		lounge *kodi.LibraryFingerprint
		mine   *kodi.LibraryFingerprint
	}{
		{name: "ShouldKeepTheMemberForAnEmptySampleAsWhileKodiRebuildsItsLibrary", mine: &kodi.LibraryFingerprint{}},
		{name: "ShouldKeepTheMemberWhenBothLibrariesAreTooSmallToCompare", lounge: small(1), mine: small(2)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inBubble(t, func(t *testing.T) {
				lib := testLibrary(30, 0, "2024-01-01 10:00:00")
				lounge, bedroom := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})
				a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

				settled(t, func() bool {
					g, _ := a.groupOf("Lounge")
					return len(g.Members) == 2 && a.fingerprintedAll("Lounge", "Bedroom")
				})

				loungeID, inst := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom")

				a.planMu.Lock()

				if tc.lounge != nil {
					a.mu.Lock()
					a.prints[loungeID] = tc.lounge
					a.mu.Unlock()
				}

				a.fingerprinted(t.Context(), inst, tc.mine)
				a.planMu.Unlock()

				g, _ := a.groupOf("Bedroom")
				assert.ElementsMatch(t, []string{"Lounge", "Bedroom"}, memberNames(g))
				assert.Empty(t, g.Reason)
			})
		})
	}
}
