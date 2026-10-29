package app

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

func TestContentSettingsShapeTheLibrary(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		ctx := t.Context()
		id := h.instance(t).id
		movie := media.Ref{Kind: media.KindMovie, ID: 6}

		content := func(groups ...string) {
			h.app.SetSettings(func(context.Context) (*config.Config, error) {
				eff := *h.app.cfg
				eff.Alexa.Content = groups

				return &eff, nil
			})
		}

		content(config.ContentTV)

		assert.False(t, h.app.Findable(id, movie), "turning movies off takes them out of the library")
		assert.True(t, h.app.Findable(id, media.Ref{Kind: media.KindTVShow, ID: 1}))

		target, err := h.app.Target(ctx, testDevice)
		require.NoError(t, err)
		assert.Equal(t, []media.Content{media.ContentMovies, media.ContentMusic}, target.Disabled)

		content(config.ContentMovies, config.ContentTV, config.ContentMusic)

		settled(t, func() bool { return h.app.Findable(id, movie) }, "turning movies on fetches them from Kodi straight away")

		target, err = h.app.Target(ctx, testDevice)
		require.NoError(t, err)
		assert.Empty(t, target.Disabled)
	})
}

func TestUnreadableSettingsFallBackToTheStartupConfiguration(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)

		h.app.cfg.Alexa.QueueLimit = 12
		h.app.SetSettings(func(context.Context) (*config.Config, error) { return nil, errors.New("settings table locked") })

		target, err := h.app.Target(t.Context(), testDevice)
		require.NoError(t, err)
		assert.Equal(t, 12, target.QueueLimit)
		assert.True(t, h.app.Findable(h.instance(t).id, media.Ref{Kind: media.KindMovie, ID: 6}), "the library keeps the startup content")
	})
}

func TestTargetIsTheLinkedInstanceBeforeTheDefault(t *testing.T) {
	lounge, bedroom := newKodi(t, fakekodi.Options{}), newKodi(t, fakekodi.Options{})
	lc := kodiConfig("Lounge", lounge, "")
	lc.Default = true
	a, _ := runApp(t, t.TempDir(), fastTuning(), lc, kodiConfig("Bedroom", bedroom, ""))
	ctx := t.Context()

	target, err := a.Target(ctx, testDevice)
	require.NoError(t, err)
	assert.Equal(t, "Lounge", target.Name)

	dev, err := a.DB().EchoDevices().GetByAlexaID(ctx, testDevice)
	require.NoError(t, err)

	dev.KodiInstanceID = a.instanceNamed(t, "Bedroom").id
	require.NoError(t, a.DB().EchoDevices().Update(ctx, &dev))

	target, err = a.Target(ctx, testDevice)
	require.NoError(t, err)
	assert.Equal(t, "Bedroom", target.Name)

	// The bedroom room plays on the bedroom Kodi even for an Echo linked elsewhere.
	require.NoError(t, a.DB().Rooms().Create(ctx, &store.Room{Name: "Lounge", KodiInstanceID: a.instanceNamed(t, "Lounge").id}))

	target, err = a.TargetForRoom(ctx, testDevice, "lounge")
	require.NoError(t, err)
	assert.Equal(t, "Lounge", target.Name)

	// The default's runtime gone, an unlinked Echo has nowhere to play.
	require.NoError(t, a.RemoveInstance(ctx, a.instanceNamed(t, "Lounge").id))

	_, err = a.Target(ctx, "amzn1.ask.device.OTHER")
	require.ErrorIs(t, err, alexa.ErrUnlinked)

	target, err = a.TargetForRoom(ctx, testDevice, "lounge")
	require.NoError(t, err)
	assert.Equal(t, "Bedroom", target.Name, "a room whose Kodi has no runtime falls back to the Echo's own")
}

func TestDatabaseFailuresAreNotMistakenForMissingLinks(t *testing.T) {
	h := newHarness(t, true)
	a := h.app
	ctx := t.Context()

	require.NoError(t, a.DB().Rooms().Create(ctx, &store.Room{Name: "Lounge"}))
	require.NoError(t, a.DB().Close())

	_, err := a.Target(ctx, testDevice)
	require.Error(t, err)
	require.NotErrorIs(t, err, alexa.ErrUnlinked)

	_, err = a.TargetForRoom(ctx, testDevice, "lounge")
	require.Error(t, err)
	require.NotErrorIs(t, err, alexa.ErrUnknownRoom)

	_, err = a.DeviceName(ctx, testDevice)
	require.Error(t, err)

	dry := DryRunTargets{App: a, Run: &DryRun{}}

	_, err = dry.Target(ctx, testDevice)
	require.Error(t, err)
	require.NotErrorIs(t, err, alexa.ErrUnlinked)

	_, err = dry.TargetForRoom(ctx, testDevice, "lounge")
	require.Error(t, err)
	require.NotErrorIs(t, err, alexa.ErrUnknownRoom)

	_, err = dry.DeviceName(ctx, testDevice)
	require.Error(t, err)

	_, err = a.SlotValues(ctx)
	require.Error(t, err)

	require.Error(t, a.ReloadAliases(ctx))
	a.cfg.Rooms = []config.Room{{Name: "Lounge"}}
	require.Error(t, a.ApplyConfiguredRoomsAndDevices(ctx))
}

func TestTargetCarriesVoiceSettings(t *testing.T) {
	h := newHarness(t, true)

	target, err := h.app.Target(t.Context(), testDevice)
	require.NoError(t, err)
	assert.Equal(t, "Lounge", target.Name)
	assert.Equal(t, config.DefaultQueueLimit, target.QueueLimit)
	assert.Equal(t, "en", target.Language)
	assert.False(t, target.ShutdownQuits)
	assert.Nil(t, target.Wake, "without a MAC address there's no way to wake Kodi")

	var sent atomic.Int32

	waker := h.instance(t).Waker
	waker.Send = func(context.Context, string, []byte) error {
		sent.Add(1)

		return nil
	}
	waker.SetMAC(net.HardwareAddr{0xdc, 0xa6, 0x32, 0x4b, 0x19, 0x0e})

	target, err = h.app.Target(t.Context(), testDevice)
	require.NoError(t, err)
	require.NotNil(t, target.Wake, "a known MAC address lets a request wake Kodi")
	require.NoError(t, target.Wake(t.Context()), "a Kodi that already answers is awake")
	assert.Zero(t, sent.Load(), "so no packet is sent")

	h.app.cfg.Alexa.ShutdownQuits = true
	h.app.cfg.Alexa.QueueLimit = 25

	target, err = h.app.Target(t.Context(), testDevice)
	require.NoError(t, err)
	assert.True(t, target.ShutdownQuits)
	assert.Equal(t, 25, target.QueueLimit)

	recs, err := h.app.db.KodiInstances().List(t.Context())
	require.NoError(t, err)

	rec := recs[0]
	rec.ShutdownQuits, rec.Language = new(false), "de"
	require.NoError(t, h.app.db.KodiInstances().Update(t.Context(), &rec))
	require.NoError(t, h.app.ReloadInstance(t.Context(), rec.ID))

	target, err = h.app.Target(t.Context(), testDevice)
	require.NoError(t, err)
	assert.False(t, target.ShutdownQuits, "the instance's setting wins")
	assert.Equal(t, "de", target.Language)

	// Settings saved in the web interface take effect without a restart.
	assert.True(t, target.DeepSearch)

	h.app.SetSettings(func(context.Context) (*config.Config, error) {
		eff := *h.app.cfg
		eff.Alexa.DeepSearch = false

		return &eff, nil
	})

	target, err = h.app.Target(t.Context(), testDevice)
	require.NoError(t, err)
	assert.False(t, target.DeepSearch)
}

func TestTargetForRoom(t *testing.T) {
	h := newHarness(t, false)
	ctx := t.Context()

	instances, err := h.app.DB().KodiInstances().List(ctx)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	bedroom := store.Room{Name: "Bedroom", KodiInstanceID: instances[0].ID}
	require.NoError(t, h.app.DB().Rooms().Create(ctx, &bedroom))
	_, err = h.app.DB().Rooms().AddAlias(ctx, bedroom.ID, "main bedroom")
	require.NoError(t, err)
	require.NoError(t, h.app.DB().Rooms().Create(ctx, &store.Room{Name: "Kitchen"}))

	for _, spoken := range []string{"bedroom", "the Bedroom", "main bedroom"} {
		target, err := h.app.TargetForRoom(ctx, testDevice, spoken)
		require.NoError(t, err, spoken)
		assert.Equal(t, "Lounge", target.Name, "%q is the bedroom, which plays on Lounge", spoken)
		assert.Equal(t, config.DefaultQueueLimit, target.QueueLimit)
	}

	dev, err := h.app.DB().EchoDevices().GetByAlexaID(ctx, testDevice)
	require.NoError(t, err)
	assert.EqualValues(t, 3, dev.RequestCount, "room requests still record the device as seen")

	_, err = h.app.TargetForRoom(ctx, testDevice, "attic")
	require.ErrorIs(t, err, alexa.ErrUnknownRoom)

	// The kitchen has no Kodi, so the device's own target applies; this device is unlinked and there's no default.
	_, err = h.app.TargetForRoom(ctx, testDevice, "kitchen")
	require.ErrorIs(t, err, alexa.ErrUnlinked)
}

func TestCutArticle(t *testing.T) {
	for spoken, want := range map[string]struct {
		rest string
		ok   bool
	}{
		"the bedroom":       {"bedroom", true},
		"The  Main Bedroom": {"Main Bedroom", true},
		"the":               {"", false},
		"bedroom":           {"", false},
		"theatre room":      {"", false},
		"":                  {"", false},
	} {
		rest, ok := cutArticle(spoken)
		assert.Equal(t, want.rest, rest, spoken)
		assert.Equal(t, want.ok, ok, spoken)
	}
}

func TestEndToEndRoomTargeting(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, false)
		h.waitIndexed(t)

		instances, err := h.app.DB().KodiInstances().List(t.Context())
		require.NoError(t, err)
		require.NoError(t, h.app.DB().Rooms().Create(t.Context(), &store.Room{Name: "Lounge Room", KodiInstanceID: instances[0].ID}))

		// The device is unlinked, but naming the room reaches its Kodi.
		resp := h.say(t, "PartyMode", "Room", "lounge room")
		assert.Equal(t, "Starting party mode.", speech(resp))
		assert.Len(t, h.kodi.CallsTo("Player.Open"), 1)

		resp = h.say(t, "PartyMode", "Room", "attic")
		assert.Equal(t, "I don't know a room called attic.", speech(resp))
	})
}

func TestDeviceNameRecordsTheEcho(t *testing.T) {
	h := newHarness(t, false)
	ctx := t.Context()

	name, err := h.app.DeviceName(ctx, "amzn1.ask.device.NEWECHO")
	require.NoError(t, err)
	assert.Empty(t, name, "a new Echo has no name yet")

	dev, err := h.app.db.EchoDevices().GetByAlexaID(ctx, "amzn1.ask.device.NEWECHO")
	require.NoError(t, err, "asking records the Echo, so it shows up to be named")

	dev.DisplayName = "Kitchen Dot"
	require.NoError(t, h.app.db.EchoDevices().Update(ctx, &dev))

	name, err = h.app.DeviceName(ctx, "amzn1.ask.device.NEWECHO")
	require.NoError(t, err)
	assert.Equal(t, "Kitchen Dot", name)
}
