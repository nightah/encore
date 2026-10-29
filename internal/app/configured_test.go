package app

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
	"github.com/nightah/encore/internal/testkit"
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
	room, err := a.DB().Rooms().Resolve(t.Context(), "living room")
	require.NoError(t, err)
	assert.Equal(t, "Lounge", room.Name)

	ids := map[string]int64{}

	kodis, err := a.DB().KodiInstances().List(t.Context())
	require.NoError(t, err)

	for _, k := range kodis {
		ids[k.Name] = k.ID
	}

	assert.Equal(t, ids["Lounge"], room.KodiInstanceID, "the room plays on the Kodi instance it names")
	assert.Equal(t, "Lounge Echo Show", dev.DisplayName)
	assert.Equal(t, ids["Lounge"], dev.KodiInstanceID)
	assert.Equal(t, room.ID, dev.RoomID)
	assert.Zero(t, dev.RequestCount, "declaring a device isn't a request")

	aliases, err := a.DB().Rooms().Aliases(t.Context(), room.ID)
	require.NoError(t, err)
	assert.Len(t, aliases, 2)

	// Applying again with fewer aliases removes the dropped one, and stays idempotent.
	a.cfg.Rooms[0].Aliases = []string{"big tv"}
	require.NoError(t, a.ApplyConfiguredRoomsAndDevices(t.Context()))

	_, err = a.DB().Rooms().Resolve(t.Context(), "living room")
	require.Error(t, err)

	aliases, err = a.DB().Rooms().Aliases(t.Context(), room.ID)
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, "big tv", aliases[0].Alias, "an alias still wanted is kept, not added twice")

	target, err := a.Target(t.Context(), "amzn1.ask.device.B")
	require.NoError(t, err)
	assert.Equal(t, "Bedroom", target.Name, "a declared device is linked before it ever speaks")
}

func TestApplyConfiguredRoomsAndDevicesReportsDatabaseFailures(t *testing.T) {
	const device = "amzn1.ask.device.A"

	testCases := []struct {
		name  string
		setup func(t *testing.T, db *store.DB)
		fault testkit.Fault
		want  string
		// after checks what the failure left behind.
		after func(t *testing.T, db *store.DB)
	}{
		{
			name:  "listing the Kodi instances",
			fault: testkit.Fault{Op: testkit.Read, Table: "kodi_instances"},
			want:  "listing kodi instances: querying",
		},
		{
			name:  "listing the rooms",
			fault: testkit.Fault{Op: testkit.Read, Table: "rooms"},
			want:  "listing rooms: querying",
		},
		{
			name:  "creating a room",
			fault: testkit.Fault{Op: testkit.Insert, Table: "rooms"},
			want:  `room "Lounge": creating room`,
			after: func(t *testing.T, db *store.DB) {
				rooms, err := db.Rooms().List(t.Context())
				require.NoError(t, err)
				assert.Empty(t, rooms)
			},
		},
		{
			name: "updating a room",
			setup: func(t *testing.T, db *store.DB) {
				require.NoError(t, db.Rooms().Create(t.Context(), &store.Room{Name: "Lounge"}))
			},
			fault: testkit.Fault{Op: testkit.Update, Table: "rooms"},
			want:  `room "Lounge": updating room`,
		},
		{
			name: "reading a room's aliases",
			// Creating the room checks its name against every alias first.
			fault: testkit.Fault{Op: testkit.Read, Table: "room_aliases", Nth: 2},
			want:  `room "Lounge": querying`,
		},
		{
			name: "removing an alias no longer declared",
			setup: func(t *testing.T, db *store.DB) {
				room := store.Room{Name: "Lounge"}
				require.NoError(t, db.Rooms().Create(t.Context(), &room))

				_, err := db.Rooms().AddAlias(t.Context(), room.ID, "front room")
				require.NoError(t, err)
			},
			fault: testkit.Fault{Op: testkit.Delete, Table: "room_aliases"},
			want:  `room "Lounge": removing room alias`,
		},
		{
			name:  "adding the second alias",
			fault: testkit.Fault{Op: testkit.Insert, Table: "room_aliases", Nth: 2},
			want:  `room "Lounge": alias "big tv": adding alias`,
			after: func(t *testing.T, db *store.DB) {
				room, err := db.Rooms().Resolve(t.Context(), "living room")
				require.NoError(t, err, "the room and the aliases before the failure are kept")
				assert.Equal(t, "Lounge", room.Name)

				_, err = db.EchoDevices().GetByAlexaID(t.Context(), device)
				require.ErrorIs(t, err, store.ErrNotFound, "devices are applied after rooms")
			},
		},
		{
			name:  "declaring a device",
			fault: testkit.Fault{Op: testkit.Insert, Table: "echo_devices"},
			want:  `device "` + device + `": declaring echo device`,
		},
		{
			name:  "linking a device",
			fault: testkit.Fault{Op: testkit.Update, Table: "echo_devices"},
			want:  `device "` + device + `": updating echo device`,
			after: func(t *testing.T, db *store.DB) {
				dev, err := db.EchoDevices().GetByAlexaID(t.Context(), device)
				require.NoError(t, err, "the device is declared")
				assert.Zero(t, dev.RoomID, "but not linked")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, faults := faultyConfig(t, config.KodiInstance{Name: "Lounge", URL: "http://127.0.0.1:1/jsonrpc"})
			cfg.Rooms = []config.Room{{Name: "Lounge", Kodi: "Lounge", Aliases: []string{"living room", "big tv"}}}
			cfg.Devices = []config.Device{{ID: device, Name: "Lounge Echo", Kodi: "Lounge", Room: "Lounge"}}

			a, err := New(t.Context(), cfg, slog.New(slog.DiscardHandler))
			require.NoError(t, err)

			t.Cleanup(func() { assert.NoError(t, a.Close()) })

			if tc.setup != nil {
				tc.setup(t, a.DB())
			}

			faults.Inject(tc.fault)

			err = a.ApplyConfiguredRoomsAndDevices(t.Context())
			require.ErrorContains(t, err, tc.want)

			if tc.fault.Op == testkit.Read {
				require.ErrorContains(t, err, testkit.ReadFaultMessage)
			} else {
				require.ErrorContains(t, err, testkit.FaultMessage)
			}

			faults.Clear()

			if tc.after != nil {
				tc.after(t, a.DB())
			}

			// Applying again once the database works finishes the job.
			require.NoError(t, a.ApplyConfiguredRoomsAndDevices(t.Context()))

			room, err := a.DB().Rooms().Resolve(t.Context(), "lounge")
			require.NoError(t, err)
			assert.NotZero(t, room.KodiInstanceID)

			aliases, err := a.DB().Rooms().Aliases(t.Context(), room.ID)
			require.NoError(t, err)

			names := []string{}
			for _, alias := range aliases {
				names = append(names, alias.Alias)
			}

			assert.ElementsMatch(t, []string{"living room", "big tv"}, names, "the declared aliases, exactly")

			dev, err := a.DB().EchoDevices().GetByAlexaID(t.Context(), device)
			require.NoError(t, err)
			assert.Equal(t, room.ID, dev.RoomID)
			assert.Equal(t, room.KodiInstanceID, dev.KodiInstanceID)
		})
	}
}
