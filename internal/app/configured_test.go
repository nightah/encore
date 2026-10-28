package app

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
)

func TestApplyConfiguredRoomsAndDevicesWithNothingDeclared(t *testing.T) {
	a := newBareApp(t)

	require.NoError(t, a.DB().Rooms().Create(t.Context(), &store.Room{Name: "Kitchen"}))
	require.NoError(t, a.ApplyConfiguredRoomsAndDevices(t.Context()))

	rooms, err := a.DB().Rooms().List(t.Context())
	require.NoError(t, err)
	assert.Len(t, rooms, 1, "rooms made in the web interface are left alone")
}

func TestApplyConfiguredRoomsAndDevicesWithUnknownNames(t *testing.T) {
	a := newBareApp(t)
	ctx := t.Context()

	require.NoError(t, a.DB().Rooms().Create(ctx, &store.Room{Name: "Kitchen"}))

	a.cfg.Rooms = []config.Room{{Name: "Attic", Kodi: "Nowhere"}}
	a.cfg.Devices = []config.Device{{ID: "amzn1.ask.device.C", Name: "Attic Dot", Kodi: "Nowhere", Room: "Cellar"}}

	require.NoError(t, a.ApplyConfiguredRoomsAndDevices(ctx))

	room, err := a.DB().Rooms().Resolve(ctx, "attic")
	require.NoError(t, err)
	assert.Zero(t, room.KodiInstanceID, "a room naming no known Kodi instance plays nowhere of its own")

	_, err = a.DB().Rooms().Resolve(ctx, "kitchen")
	require.NoError(t, err, "rooms that aren't declared are left alone")

	dev, err := a.DB().EchoDevices().GetByAlexaID(ctx, "amzn1.ask.device.C")
	require.NoError(t, err)
	assert.Equal(t, "Attic Dot", dev.DisplayName)
	assert.Zero(t, dev.KodiInstanceID)
	assert.Zero(t, dev.RoomID)
}

func TestApplyConfiguredRoomsAndDevices(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Kodi = []config.KodiInstance{{Name: "Lounge", URL: "http://127.0.0.1:1/jsonrpc"}, {Name: "Bedroom", URL: "http://127.0.0.1:2/jsonrpc"}}
	cfg.Rooms = []config.Room{{Name: "Lounge", Kodi: "Lounge", Aliases: []string{"living room", "big tv"}}}
	cfg.Devices = []config.Device{{ID: "amzn1.ask.device.A", Name: "Lounge Echo Show", Kodi: "Lounge", Room: "Lounge"}, {ID: "amzn1.ask.device.B", Kodi: "Bedroom"}}

	a, err := New(t.Context(), &cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	require.NoError(t, a.ApplyConfiguredRoomsAndDevices(t.Context()))

	dev, err := a.DB().EchoDevices().GetByAlexaID(t.Context(), "amzn1.ask.device.A")
	require.NoError(t, err)
	assert.Equal(t, "Lounge Echo Show", dev.DisplayName)
	assert.NotZero(t, dev.KodiInstanceID)
	assert.NotZero(t, dev.RoomID)
	assert.Zero(t, dev.RequestCount, "declaring a device isn't a request")

	room, err := a.DB().Rooms().Resolve(t.Context(), "living room")
	require.NoError(t, err)
	assert.Equal(t, "Lounge", room.Name)

	// Applying again with fewer aliases removes the dropped one, and stays idempotent.
	a.cfg.Rooms[0].Aliases = []string{"big tv"}
	require.NoError(t, a.ApplyConfiguredRoomsAndDevices(t.Context()))

	_, err = a.DB().Rooms().Resolve(t.Context(), "living room")
	require.Error(t, err)

	target, err := a.Target(t.Context(), "amzn1.ask.device.B")
	require.NoError(t, err)
	assert.Equal(t, "Bedroom", target.Name, "a declared device is linked before it ever speaks")
}
