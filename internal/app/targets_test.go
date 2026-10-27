package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
)

func TestTargetCarriesVoiceSettings(t *testing.T) {
	h := newHarness(t, true)

	target, err := h.app.Target(t.Context(), testDevice)
	require.NoError(t, err)
	assert.Equal(t, "Lounge", target.Name)
	assert.Equal(t, config.DefaultQueueLimit, target.QueueLimit)
	assert.Equal(t, "en", target.Language)
	assert.False(t, target.ShutdownQuits)

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

	_, err = h.app.TargetForRoom(ctx, testDevice, "attic")
	require.ErrorIs(t, err, alexa.ErrUnknownRoom)

	// The kitchen has no Kodi, so the device's own target applies; this device is unlinked and there's no default.
	_, err = h.app.TargetForRoom(ctx, testDevice, "kitchen")
	require.ErrorIs(t, err, alexa.ErrUnlinked)

	dev, err := h.app.DB().EchoDevices().GetByAlexaID(ctx, testDevice)
	require.NoError(t, err)
	assert.Positive(t, dev.RequestCount, "room requests still record the device as seen")
}

func TestEndToEndRoomTargeting(t *testing.T) {
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
