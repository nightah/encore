package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Rooms returns the room repository.
func (db *DB) Rooms() Rooms { return Rooms{db} }

func scanRoom(s scanner) (Room, error) {
	var (
		r                Room
		kodi             sql.NullInt64
		created, updated int64
	)

	if err := s.Scan(&r.ID, &r.Name, &kodi, &created, &updated); err != nil {
		return Room{}, mapErr(err)
	}

	r.KodiInstanceID = kodi.Int64
	r.CreatedAt, r.UpdatedAt = fromMillis(created), fromMillis(updated)

	return r, nil
}

func scanRoomAlias(s scanner) (RoomAlias, error) {
	var (
		a       RoomAlias
		created int64
	)

	if err := s.Scan(&a.ID, &a.RoomID, &a.Alias, &a.Normalised, &created, &a.Kind); err != nil {
		return RoomAlias{}, mapErr(err)
	}

	a.CreatedAt = fromMillis(created)

	return a, nil
}

func (t trailing) Scan(dest ...any) error { return t.scanner.Scan(append(dest, t.last)...) }

// Create inserts room and sets its ID and timestamps.
func (r Rooms) Create(ctx context.Context, room *Room) error {
	now := r.db.stamp()
	norm := Normalise(room.Name)

	return r.db.tx(ctx, func(tx *sql.Tx) error {
		if err := roomNameFree(ctx, tx, norm, 0); err != nil {
			return err
		}

		err := tx.QueryRowContext(ctx, `INSERT INTO rooms (name, normalised, kodi_instance_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?) RETURNING id`,
			room.Name, norm, nullID(room.KodiInstanceID), toMillis(now), toMillis(now)).Scan(&room.ID)
		if err != nil {
			return fmt.Errorf("creating room “%s”: %w", room.Name, mapErr(err))
		}

		room.CreatedAt, room.UpdatedAt = now, now

		return nil
	})
}

// Update saves room's name and Kodi instance.
func (r Rooms) Update(ctx context.Context, room *Room) error {
	now := r.db.stamp()
	norm := Normalise(room.Name)

	return r.db.tx(ctx, func(tx *sql.Tx) error {
		if err := roomNameFree(ctx, tx, norm, room.ID); err != nil {
			return err
		}

		err := affected(tx.ExecContext(ctx, `UPDATE rooms SET name = ?, normalised = ?, kodi_instance_id = ?, updated_at = ? WHERE id = ?`,
			room.Name, norm, nullID(room.KodiInstanceID), toMillis(now), room.ID))
		if err != nil {
			return fmt.Errorf("updating room %d: %w", room.ID, err)
		}

		room.UpdatedAt = now

		return nil
	})
}

// roomNameFree reports ErrConflict if norm is already a room name or alias, other than room self's own.
func roomNameFree(ctx context.Context, tx *sql.Tx, norm string, self int64) error {
	if norm == "" {
		return fmt.Errorf("%w: room name must contain a letter or digit", ErrInvalid)
	}

	var owner int64

	err := tx.QueryRowContext(ctx, `SELECT id FROM rooms WHERE normalised = ?1 AND id <> ?2
		UNION ALL SELECT room_id FROM room_aliases WHERE normalised = ?1 AND room_id <> ?2 LIMIT 1`, norm, self).Scan(&owner)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("checking room name: %w", err)
	default:
		return fmt.Errorf("%w: “%s” is already used by room %d", ErrConflict, norm, owner)
	}
}

// Get returns the room with the given ID.
func (r Rooms) Get(ctx context.Context, id int64) (Room, error) {
	room, err := scanRoom(r.db.read.QueryRowContext(ctx, `SELECT `+roomColumns+` FROM rooms WHERE id = ?`, id))
	if err != nil {
		return Room{}, fmt.Errorf("getting room %d: %w", id, err)
	}

	return room, nil
}

// List returns all rooms ordered by name.
func (r Rooms) List(ctx context.Context) ([]Room, error) {
	return queryAll(ctx, r.db.read, scanRoom, `SELECT `+roomColumns+` FROM rooms ORDER BY normalised`)
}

// Delete removes a room and its aliases. Devices in it become unassigned.
func (r Rooms) Delete(ctx context.Context, id int64) error {
	if err := affected(r.db.write.ExecContext(ctx, `DELETE FROM rooms WHERE id = ?`, id)); err != nil {
		return fmt.Errorf("deleting room %d: %w", id, err)
	}

	return nil
}

// Resolve returns the room whose name or alias matches spoken under Normalise, and the kind of name it matched; a room's own name is SpokenRoom.
func (r Rooms) Resolve(ctx context.Context, spoken string) (Room, SpokenKind, error) {
	var kind SpokenKind

	room, err := scanRoom(trailing{r.db.read.QueryRowContext(ctx, `SELECT `+roomColumns+`, ?2 FROM rooms WHERE normalised = ?1
		UNION ALL SELECT r.id, r.name, r.kodi_instance_id, r.created_at, r.updated_at, a.kind
		FROM room_aliases a JOIN rooms r ON r.id = a.room_id WHERE a.normalised = ?1 LIMIT 1`, Normalise(spoken), SpokenRoom), &kind})
	if err != nil {
		return Room{}, "", fmt.Errorf("resolving room “%s”: %w", spoken, err)
	}

	return room, kind, nil
}

// AddAlias adds a spoken alias of the given kind to a room. It returns ErrConflict if the alias already names any room.
func (r Rooms) AddAlias(ctx context.Context, roomID int64, alias string, kind SpokenKind) (RoomAlias, error) {
	a := RoomAlias{RoomID: roomID, Alias: alias, Normalised: Normalise(alias), CreatedAt: r.db.stamp(), Kind: kind}

	err := r.db.tx(ctx, func(tx *sql.Tx) error {
		if err := roomNameFree(ctx, tx, a.Normalised, 0); err != nil {
			return err
		}

		err := tx.QueryRowContext(ctx, `INSERT INTO room_aliases (room_id, alias, normalised, created_at, kind) VALUES (?, ?, ?, ?, ?) RETURNING id`,
			a.RoomID, a.Alias, a.Normalised, toMillis(a.CreatedAt), a.Kind).Scan(&a.ID)
		if err != nil {
			return mapErr(err)
		}

		return nil
	})
	if err != nil {
		return RoomAlias{}, fmt.Errorf("adding alias “%s” to room %d: %w", alias, roomID, err)
	}

	return a, nil
}

// RemoveAlias deletes a room alias by ID.
func (r Rooms) RemoveAlias(ctx context.Context, aliasID int64) error {
	if err := affected(r.db.write.ExecContext(ctx, `DELETE FROM room_aliases WHERE id = ?`, aliasID)); err != nil {
		return fmt.Errorf("removing room alias %d: %w", aliasID, err)
	}

	return nil
}

// Aliases returns a room's aliases.
func (r Rooms) Aliases(ctx context.Context, roomID int64) ([]RoomAlias, error) {
	return queryAll(ctx, r.db.read, scanRoomAlias,
		`SELECT id, room_id, alias, normalised, created_at, kind FROM room_aliases WHERE room_id = ? ORDER BY normalised`, roomID)
}
