package app

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
	"github.com/nightah/encore/internal/testkit"
)

// TestDryBackendSendsNoWrites guards against a write method added to media.Backend reaching Kodi during a replay.
func TestDryBackendSendsNoWrites(t *testing.T) {
	kodi := testkit.NewBackend(testkit.SampleLibrary()...)
	run := &DryRun{}
	b := dryBackend{Backend: kodi, run: run}
	ctx := t.Context()
	ref := media.Ref{Kind: media.KindMovie, ID: 1}
	level := 30

	writes := []func() error{
		func() error { return b.Play(ctx, ref, media.PlayOptions{}) },
		func() error { return b.Queue(ctx, []media.Ref{ref}) },
		func() error { return b.PlayFile(ctx, "/media/a.mkv") },
		func() error { return b.PartyMode(ctx) },
		func() error { return b.Transport(ctx, media.Transport("pause")) },
		func() error { return b.Seek(ctx, media.Seek{Step: "smallforward"}) },
		func() error { _, err := b.Volume(ctx, media.VolumeChange{Set: &level}); return err },
		func() error { return b.SetShuffle(ctx, true) },
		func() error { _, err := b.SetRepeat(ctx, media.Repeat("all")); return err },
		func() error { return b.Picture(ctx, media.PictureCommand("zoomin")) },
		func() error { return b.SetSubtitle(ctx, media.StreamChange{}) },
		func() error { return b.SetAudioStream(ctx, media.StreamChange{}) },
		func() error { return b.Input(ctx, media.InputAction("select")) },
		func() error { return b.SendText(ctx, "dune") },
		func() error { return b.ShowView(ctx, media.View{}) },
		func() error { return b.OpenFavourite(ctx, media.Favourite{Title: "Radio"}) },
		func() error { return b.ExecuteAddon(ctx, "plugin.video.youtube", nil) },
		func() error { return b.System(ctx, media.SystemCommand("shutdown")) },
		func() error { return b.ScanLibrary(ctx, media.Database("video")) },
		func() error { return b.CleanLibrary(ctx, media.Database("video")) },
		func() error { return b.PlayChannel(ctx, 24) },
	}

	for _, w := range writes {
		require.NoError(t, w())
	}

	assert.Empty(t, kodi.Calls(), "a dry run must not send commands")
	assert.Len(t, run.Calls(), len(writes))
}

func TestDryBackendRecordsSkinSearches(t *testing.T) {
	kodi := testkit.NewBackend()
	run := &DryRun{}
	b := dryBackend{Backend: kodi, run: run}

	kodi.SetSkin(media.Skin{ID: "skin.copacetic", Search: media.SkinSearchNoProfile}, nil)
	skin, err := b.SkinSearch(t.Context(), "dune", "")
	require.NoError(t, err)
	assert.Equal(t, media.SkinSearchNoProfile, skin.Search)
	assert.Empty(t, run.Calls(), "a skin without a profile would search with Global Search instead")

	kodi.SetSkin(media.Skin{ID: "skin.af2.fork", Profile: "skin.arctic.fuse.2", Search: media.SkinSearchSupported}, errBroken)
	skin, err = b.SkinSearch(t.Context(), "dune", "skin.arctic.fuse.2")
	require.NoError(t, err, "nothing runs, so nothing fails")
	assert.Equal(t, media.SkinSearchSupported, skin.Search)
	assert.Equal(t, []Call{{Method: "SkinSearch", Params: map[string]any{"skin": "skin.af2.fork", "profile": "skin.arctic.fuse.2", "query": "dune"}}}, run.Calls())
	assert.Equal(t, []string{"Skin ", "Skin skin.arctic.fuse.2"}, kodi.Calls(), "only the skin is read")

	kodi.Err = errBroken
	_, err = b.SkinSearch(t.Context(), "dune", "")
	require.ErrorIs(t, err, errBroken)
}

func TestDryBackendRecordsWhatWouldBeSent(t *testing.T) {
	to, set, mute := 90*time.Second, 40, true

	testCases := []struct {
		name   string
		call   func(b dryBackend) error
		method string
		params map[string]any
	}{
		{name: "ShouldRecordASeekTo", call: func(b dryBackend) error { return b.Seek(t.Context(), media.Seek{To: &to}) }, method: "Player.Seek", params: map[string]any{"to_s": 90.0}},
		{name: "ShouldRecordASeekStep", call: func(b dryBackend) error { return b.Seek(t.Context(), media.Seek{Step: "bigforward"}) }, method: "Player.Seek", params: map[string]any{"step": media.SeekStep("bigforward")}},
		{name: "ShouldRecordASeekBy", call: func(b dryBackend) error { return b.Seek(t.Context(), media.Seek{By: -30 * time.Second}) }, method: "Player.Seek", params: map[string]any{"by_s": -30.0}},
		{name: "ShouldRecordAVolumeSet", call: func(b dryBackend) error { _, err := b.Volume(t.Context(), media.VolumeChange{Set: &set}); return err }, method: "Application.SetVolume", params: map[string]any{"set": 40}},
		{name: "ShouldRecordAVolumeMute", call: func(b dryBackend) error { _, err := b.Volume(t.Context(), media.VolumeChange{Mute: &mute}); return err }, method: "Application.SetVolume", params: map[string]any{"mute": true}},
		{name: "ShouldRecordAVolumeToggle", call: func(b dryBackend) error {
			_, err := b.Volume(t.Context(), media.VolumeChange{ToggleMute: true})
			return err
		}, method: "Application.SetVolume", params: map[string]any{"toggle_mute": true}},
		{name: "ShouldRecordAVolumeStep", call: func(b dryBackend) error { _, err := b.Volume(t.Context(), media.VolumeChange{Step: -10}); return err }, method: "Application.SetVolume", params: map[string]any{"step": -10}},
		{name: "ShouldRecordAPlay", call: func(b dryBackend) error {
			return b.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 6}, media.PlayOptions{Resume: true})
		}, method: "Player.Open", params: map[string]any{"kind": media.KindMovie, "id": 6, "resume": true, "shuffle": false}},
		{name: "ShouldRecordAPlayFile", call: func(b dryBackend) error { return b.PlayFile(t.Context(), "/media/a.mkv") }, method: "Player.Open", params: map[string]any{"file": "/media/a.mkv"}},
		{name: "ShouldRecordPartyMode", call: func(b dryBackend) error { return b.PartyMode(t.Context()) }, method: "Player.Open", params: map[string]any{"partymode": true}},
		{name: "ShouldRecordAChannel", call: func(b dryBackend) error { return b.PlayChannel(t.Context(), 24) }, method: "Player.Open", params: map[string]any{"channelid": 24}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run := &DryRun{}
			require.NoError(t, tc.call(dryBackend{Backend: testkit.NewBackend(), run: run}))
			assert.Equal(t, []Call{{Method: tc.method, Params: tc.params}}, run.Calls())
		})
	}
}

func TestDryVolumeReportsTheLevelItWouldSet(t *testing.T) {
	b := dryBackend{Backend: testkit.NewBackend(), run: &DryRun{}}
	set := 15

	st, err := b.Volume(t.Context(), media.VolumeChange{Set: &set})
	require.NoError(t, err)
	assert.Equal(t, 15, st.Level)

	st, err = b.Volume(t.Context(), media.VolumeChange{Step: 10})
	require.NoError(t, err)
	assert.Equal(t, 50, st.Level, "without Kodi's level, a relative change reports the midpoint")

	mode, err := b.SetRepeat(t.Context(), media.Repeat("one"))
	require.NoError(t, err)
	assert.Equal(t, media.Repeat("one"), mode)
}

func TestDryRunCallsAreACopy(t *testing.T) {
	run := &DryRun{}
	run.record("Player.Open", nil)

	calls := run.Calls()
	calls[0].Method = "changed"

	assert.Equal(t, "Player.Open", run.Calls()[0].Method)
	assert.Empty(t, (&DryRun{}).Calls())
}

func TestDryRunTargets(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		ctx := t.Context()
		inst := h.instance(t)
		inst.Waker.SetMAC(net.HardwareAddr{0xdc, 0xa6, 0x32, 0x4b, 0x19, 0x0e})

		run := &DryRun{}
		targets := DryRunTargets{App: h.app, Run: run}

		target, err := targets.Target(ctx, testDevice)
		require.NoError(t, err)
		assert.Equal(t, "Lounge", target.Name, "an unknown device gets the default instance")
		assert.Nil(t, target.Wake, "a dry run never wakes Kodi")
		assert.NotNil(t, target.Matcher)

		items, err := target.Backend.Library(ctx, media.KindMovie)
		require.NoError(t, err)
		assert.Len(t, items, 7, "reads still reach Kodi")

		require.NoError(t, target.Backend.Play(ctx, media.Ref{Kind: media.KindMovie, ID: 6}, media.PlayOptions{}))
		assert.Empty(t, h.kodi.CallsTo("Player.Open"), "writes don't")
		require.Len(t, run.Calls(), 1)
		assert.Equal(t, "Player.Open", run.Calls()[0].Method)

		_, err = h.app.DB().EchoDevices().GetByAlexaID(ctx, testDevice)
		require.ErrorIs(t, err, store.ErrNotFound, "a replay doesn't record the device as seen")

		name, err := targets.DeviceName(ctx, testDevice)
		require.NoError(t, err)
		assert.Empty(t, name)

		_, err = h.app.DB().EchoDevices().GetByAlexaID(ctx, testDevice)
		require.ErrorIs(t, err, store.ErrNotFound, "nor does asking its name")
	})
}

func TestDryRunTargetsFollowLinksAndRooms(t *testing.T) {
	h := newHarness(t, false)
	ctx := t.Context()
	db := h.app.DB()
	inst := h.instance(t)
	targets := DryRunTargets{App: h.app, Run: &DryRun{}}

	_, err := targets.Target(ctx, testDevice)
	require.ErrorIs(t, err, alexa.ErrUnlinked, "no link and no default")

	dev, err := db.EchoDevices().Declare(ctx, "amzn1.ask.device.LINKED")
	require.NoError(t, err)

	dev.DisplayName, dev.KodiInstanceID = "Lounge Echo", inst.id
	require.NoError(t, db.EchoDevices().Update(ctx, &dev))

	target, err := targets.Target(ctx, "amzn1.ask.device.LINKED")
	require.NoError(t, err)
	assert.Equal(t, "Lounge", target.Name)

	name, err := targets.DeviceName(ctx, "amzn1.ask.device.LINKED")
	require.NoError(t, err)
	assert.Equal(t, "Lounge Echo", name)

	require.NoError(t, db.Rooms().Create(ctx, &store.Room{Name: "Bedroom", KodiInstanceID: inst.id}))
	require.NoError(t, db.Rooms().Create(ctx, &store.Room{Name: "Kitchen"}))

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldFollowTheRoomName", have: "bedroom"},
		{name: "ShouldFollowTheRoomNameAfterAnArticle", have: "the bedroom"},
		{name: "ShouldFollowTheRoomNameInAnyCase", have: "The Bedroom"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, err := targets.TargetForRoom(t.Context(), testDevice, tc.have)
			require.NoError(t, err)
			assert.Equal(t, "Lounge", target.Name)
		})
	}

	_, err = targets.TargetForRoom(ctx, testDevice, "the attic")
	require.ErrorIs(t, err, alexa.ErrUnknownRoom)
	assert.Contains(t, err.Error(), "the attic", "the room is named as heard")

	_, err = targets.TargetForRoom(ctx, testDevice, "kitchen")
	require.ErrorIs(t, err, alexa.ErrUnlinked, "a room without Kodi falls back to the device's own target")

	target, err = targets.TargetForRoom(ctx, "amzn1.ask.device.LINKED", "kitchen")
	require.NoError(t, err)
	assert.Equal(t, "Lounge", target.Name)

	_, err = db.EchoDevices().GetByAlexaID(ctx, testDevice)
	require.ErrorIs(t, err, store.ErrNotFound, "a replay doesn't record the device as seen")
}

func TestTargetForRoomSaysHowTheRoomWasNamed(t *testing.T) {
	h := newHarness(t, true)
	ctx := t.Context()
	db := h.app.DB()
	inst := h.instance(t)

	lounge := store.Room{Name: "Lounge", KodiInstanceID: inst.id}
	require.NoError(t, db.Rooms().Create(ctx, &lounge))

	_, err := db.Rooms().AddAlias(ctx, lounge.ID, "TV", store.SpokenDevice)
	require.NoError(t, err)

	_, err = db.Rooms().AddAlias(ctx, lounge.ID, "lounge room", store.SpokenRoom)
	require.NoError(t, err)

	testCases := []struct {
		name    string
		targets alexa.Targets
	}{
		{name: "ShouldSayHowTheRoomWasNamedToTheApp", targets: h.app},
		{name: "ShouldSayHowTheRoomWasNamedToADryRun", targets: DryRunTargets{App: h.app, Run: &DryRun{}}},
	}

	spokenCases := []struct {
		name     string
		have     string
		expected actions.Via
	}{
		{name: "ShouldNameADeviceAlias", have: "TV", expected: actions.Via{Spoken: "TV", Kind: "device", Room: "Lounge"}},
		{name: "ShouldNameADeviceAliasAfterAnArticle", have: "the tv", expected: actions.Via{Spoken: "tv", Kind: "device", Room: "Lounge"}},
		{name: "ShouldNameARoomAlias", have: "lounge room", expected: actions.Via{Spoken: "lounge room", Kind: "room", Room: "Lounge"}},
		{name: "ShouldNameTheRoomAfterAnArticle", have: "the lounge", expected: actions.Via{Spoken: "Lounge", Kind: "room", Room: "Lounge"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, sc := range spokenCases {
				t.Run(sc.name, func(t *testing.T) {
					target, err := tc.targets.TargetForRoom(t.Context(), testDevice, sc.have)
					require.NoError(t, err)
					assert.Equal(t, sc.expected, target.Via)
				})
			}
		})
	}

	target, err := h.app.Target(ctx, testDevice)
	require.NoError(t, err)
	assert.Zero(t, target.Via, "an Echo's own target names no room")
}

func TestDryRunTargetsSkipAMissingRuntime(t *testing.T) {
	h := newHarness(t, true)
	ctx := t.Context()
	targets := DryRunTargets{App: h.app, Run: &DryRun{}}

	require.NoError(t, h.app.RemoveInstance(ctx, h.instance(t).id))

	_, err := targets.Target(ctx, testDevice)
	require.ErrorIs(t, err, alexa.ErrUnlinked, "the default's runtime is gone")
}
