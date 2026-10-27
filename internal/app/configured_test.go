package app

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/config"
)

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
