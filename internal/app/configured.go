package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/nightah/encore/internal/store"
)

// ApplyConfiguredRoomsAndDevices makes the rooms and Echo links declared in the config file match the database, so a stateless deployment can be set up entirely as code. Rooms are matched by name and devices by Alexa device ID; a declared room's spoken aliases are made to match exactly. Rooms and devices that aren't in the config file are left alone.
func (a *App) ApplyConfiguredRoomsAndDevices(ctx context.Context) error {
	if len(a.cfg.Rooms) == 0 && len(a.cfg.Devices) == 0 {
		return nil
	}

	instances, err := a.db.KodiInstances().List(ctx)
	if err != nil {
		return fmt.Errorf("listing kodi instances: %w", err)
	}

	instanceID := map[string]int64{}
	for _, k := range instances {
		instanceID[k.Name] = k.ID
	}

	roomID, err := a.applyConfiguredRooms(ctx, instanceID)
	if err != nil {
		return err
	}

	for _, d := range a.cfg.Devices {
		dev, err := a.db.EchoDevices().Declare(ctx, d.ID)
		if err != nil {
			return fmt.Errorf("device %q: %w", d.ID, err)
		}

		dev.DisplayName, dev.KodiInstanceID, dev.RoomID = d.Name, instanceID[d.Kodi], roomID[d.Room]

		if err := a.db.EchoDevices().Update(ctx, &dev); err != nil {
			return fmt.Errorf("device %q: %w", d.ID, err)
		}
	}

	return nil
}

func (a *App) applyConfiguredRooms(ctx context.Context, instanceID map[string]int64) (map[string]int64, error) {
	existing, err := a.db.Rooms().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing rooms: %w", err)
	}

	byName := map[string]store.Room{}
	for _, r := range existing {
		byName[r.Name] = r
	}

	ids := map[string]int64{}

	for _, c := range a.cfg.Rooms {
		room := byName[c.Name]
		room.Name, room.KodiInstanceID = c.Name, instanceID[c.Kodi]

		if room.ID == 0 {
			err = a.db.Rooms().Create(ctx, &room)
		} else {
			err = a.db.Rooms().Update(ctx, &room)
		}

		if err != nil {
			return nil, fmt.Errorf("room %q: %w", c.Name, err)
		}

		ids[c.Name] = room.ID

		want := make([]store.RoomAlias, 0, len(c.Aliases)+len(c.Devices))
		for _, alias := range c.Aliases {
			want = append(want, store.RoomAlias{Alias: alias, Kind: store.SpokenRoom})
		}

		for _, device := range c.Devices {
			want = append(want, store.RoomAlias{Alias: device, Kind: store.SpokenDevice})
		}

		if err := a.syncRoomAliases(ctx, room.ID, want); err != nil {
			return nil, fmt.Errorf("room %q: %w", c.Name, err)
		}
	}

	return ids, nil
}

func (a *App) syncRoomAliases(ctx context.Context, roomID int64, want []store.RoomAlias) error {
	have, err := a.db.Rooms().Aliases(ctx, roomID)
	if err != nil {
		return err
	}

	present := map[string]bool{}

	for _, alias := range have {
		if slices.ContainsFunc(want, func(w store.RoomAlias) bool { return w.Alias == alias.Alias && w.Kind == alias.Kind }) {
			present[alias.Alias] = true

			continue
		}

		if err := a.db.Rooms().RemoveAlias(ctx, alias.ID); err != nil {
			return err
		}
	}

	for _, w := range want {
		if present[w.Alias] {
			continue
		}

		if _, err := a.db.Rooms().AddAlias(ctx, roomID, w.Alias, w.Kind); err != nil {
			return fmt.Errorf("alias %q: %w", w.Alias, err)
		}
	}

	return nil
}
